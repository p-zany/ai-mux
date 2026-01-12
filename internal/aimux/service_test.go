package aimux

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAuthEnforcedWhenUsersConfigured(t *testing.T) {
	stateDir := writeTempCreds(t, "upstream-token", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	tokenServer := newAnthropicTokenServer(t, "upstream-token", "refresh-token")
	defer tokenServer.Close()

	var upstreamCalls int32
	var upstreamAuth string
	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		upstreamAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Users = []User{{Name: "alice", Token: "secret"}}
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	server := newHTTPTestServer(t, service)
	defer server.Close()

	// Test 1: Request without auth header should be allowed (anonymous access)
	resp, err := http.Get(server.URL + "/claude/v1/test")
	if err != nil {
		t.Fatalf("request without auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for anonymous request, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&upstreamCalls) != 1 {
		t.Fatalf("upstream should be called for anonymous requests, got %d calls", upstreamCalls)
	}

	// Test 2: Request with valid token should be allowed
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/claude/v1/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authorized request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for valid token, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&upstreamCalls) != 2 {
		t.Fatalf("expected upstream to be called twice, got %d", upstreamCalls)
	}
	if upstreamAuth != "Bearer upstream-token" {
		t.Fatalf("upstream should receive refreshed access token, got %q", upstreamAuth)
	}

	// Test 3: Request with invalid token should be rejected
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/claude/v1/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request with invalid token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&upstreamCalls) != 2 {
		t.Fatalf("upstream should not be called for invalid token, got %d calls", upstreamCalls)
	}
}

func TestNoAuthRequiredWhenNoUsersConfigured(t *testing.T) {
	stateDir := writeTempCreds(t, "upstream-token", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	tokenServer := newAnthropicTokenServer(t, "upstream-token", "refresh-token")
	defer tokenServer.Close()

	var upstreamCalls int32
	var upstreamAuth string
	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		upstreamAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Users = []User{} // No users configured
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	server := newHTTPTestServer(t, service)
	defer server.Close()

	// Test 1: Request without auth header should work
	resp, err := http.Get(server.URL + "/claude/v1/test")
	if err != nil {
		t.Fatalf("request without auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for anonymous request (no users), got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&upstreamCalls) != 1 {
		t.Fatalf("upstream should be called, got %d calls", upstreamCalls)
	}

	// Test 2: Request with any auth header should also work (ignored)
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/claude/v1/test", nil)
	req.Header.Set("Authorization", "Bearer some-random-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request with auth header: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when no users configured (even with auth header), got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&upstreamCalls) != 2 {
		t.Fatalf("upstream should be called, got %d calls", upstreamCalls)
	}
	if upstreamAuth != "Bearer upstream-token" {
		t.Fatalf("upstream should receive provider's token, got %q", upstreamAuth)
	}
}

func TestRoutingDispatchesProviders(t *testing.T) {
	stateDir := writeTempCreds(t, "token-a", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	anthTokenServer := newAnthropicTokenServer(t, "token-a", "refresh-token")
	defer anthTokenServer.Close()

	var anthCalls, chatgptCalls int32
	var anthAuth, chatgptAuth string
	var anthPath, chatgptPath string
	var chatgptBeta, chatgptOrg, accountID string

	anthropic := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&anthCalls, 1)
		anthAuth = r.Header.Get("Authorization")
		anthPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer anthropic.Close()

	chatgpt := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chatgptCalls, 1)
		chatgptAuth = r.Header.Get("Authorization")
		chatgptPath = r.URL.Path
		chatgptBeta = r.Header.Get("anthropic-beta")
		chatgptOrg = r.Header.Get("OpenAI-Organization")
		accountID = r.Header.Get("ChatGPT-Account-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer chatgpt.Close()

	tokenServer := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"openai-access","refresh_token":"openai-refresh-new","account_id":"acct-123","expires_in":120}`)
	}))
	defer tokenServer.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Users = []User{{Name: "alice", Token: "secret"}}
	cfg.Providers = []string{"claude", "chatgpt"}
	cfg.TestClaudeBaseURL = anthropic.URL
	cfg.TestClaudeTokenEndpoint = anthTokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}
	cfg.TestChatGPTBaseURL = chatgpt.URL
	cfg.TestChatGPTTokenEndpoint = tokenServer.URL
	cfg.TestChatGPTRefreshToken = "openai-refresh"

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/claude/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("claude request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if anthPath != "/v1/models" || anthAuth != "Bearer token-a" {
		t.Fatalf("anthropic upstream mismatch: path=%q auth=%q", anthPath, anthAuth)
	}
	if atomic.LoadInt32(&chatgptCalls) != 0 {
		t.Fatalf("chatgpt should not be called for claude prefix")
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/chatgpt/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chatgpt request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// ChatGPT backend API doesn't use /v1 prefix
	if chatgptPath != "/chat/completions" {
		t.Fatalf("chatgpt upstream path mismatch: %q", chatgptPath)
	}
	if chatgptAuth != "Bearer openai-access" {
		t.Fatalf("chatgpt upstream auth mismatch: %q", chatgptAuth)
	}
	if chatgptBeta != "" {
		t.Fatalf("anthropic-beta should not be forwarded to chatgpt, got %q", chatgptBeta)
	}
	// Organization support removed - chatgptOrg should be empty
	if chatgptOrg != "" {
		t.Fatalf("organization header should not be set (feature removed), got %q", chatgptOrg)
	}
	if accountID != "acct-123" {
		t.Fatalf("chatgpt account header mismatch: %q", accountID)
	}

	resp, err = http.Get(server.URL + "/unknown/v1/test")
	if err != nil {
		t.Fatalf("unknown prefix: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown prefix, got %d", resp.StatusCode)
	}
}

func TestRequestHopByHopHeadersAreStripped(t *testing.T) {
	stateDir := writeTempCreds(t, "token-a", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	tokenServer := newAnthropicTokenServer(t, "token-a", "refresh-token")
	defer tokenServer.Close()

	var receivedHeaders http.Header

	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/claude/v1/models", nil)
	// Set hop-by-hop headers that should be stripped
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Proxy-Authorization", "Basic xyz")
	req.Header.Set("Proxy-Authenticate", "Basic")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Te", "trailers")
	req.Header.Set("Upgrade", "websocket")
	// Set regular headers that should pass through
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify hop-by-hop headers are NOT forwarded
	hopByHopHeaders := []string{"Connection", "Keep-Alive", "Proxy-Authorization", "Proxy-Authenticate", "Transfer-Encoding", "Te", "Upgrade"}
	for _, h := range hopByHopHeaders {
		if v := receivedHeaders.Get(h); v != "" {
			t.Errorf("hop-by-hop header %q should be stripped, got %q", h, v)
		}
	}

	// Verify regular headers ARE forwarded
	if v := receivedHeaders.Get("User-Agent"); v != "test-agent" {
		t.Errorf("User-Agent should be forwarded, got %q", v)
	}
	if v := receivedHeaders.Get("X-Custom-Header"); v != "custom-value" {
		t.Errorf("X-Custom-Header should be forwarded, got %q", v)
	}
	if v := receivedHeaders.Get("Accept"); v != "application/json" {
		t.Errorf("Accept should be forwarded, got %q", v)
	}
}

func TestUpstreamResponseHeadersAreForwardedToClient(t *testing.T) {
	stateDir := writeTempCreds(t, "token-a", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	tokenServer := newAnthropicTokenServer(t, "token-a", "refresh-token")
	defer tokenServer.Close()

	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set custom response headers that should be forwarded
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("X-Request-Id", "req-12345")
		w.Header().Set("X-RateLimit-Remaining", "99")
		w.Header().Add("X-Multi-Value", "value1")
		w.Header().Add("X-Multi-Value", "value2")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		// Set hop-by-hop headers that should NOT be forwarded
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("Proxy-Authenticate", "Basic")
		w.Header().Set("Upgrade", "websocket")

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"status":"ok"}`)
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	resp, err := http.Get(server.URL + "/claude/v1/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify custom headers are forwarded to client
	if v := resp.Header.Get("X-Custom-Header"); v != "custom-value" {
		t.Errorf("X-Custom-Header not forwarded, got %q", v)
	}
	if v := resp.Header.Get("X-Request-Id"); v != "req-12345" {
		t.Errorf("X-Request-Id not forwarded, got %q", v)
	}
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "99" {
		t.Errorf("X-RateLimit-Remaining not forwarded, got %q", v)
	}
	if v := resp.Header.Get("Content-Type"); v != "application/json" {
		t.Errorf("Content-Type not forwarded, got %q", v)
	}
	if v := resp.Header.Get("Cache-Control"); v != "no-cache" {
		t.Errorf("Cache-Control not forwarded, got %q", v)
	}

	// Verify multi-value headers are forwarded correctly
	multiValues := resp.Header.Values("X-Multi-Value")
	if len(multiValues) != 2 || multiValues[0] != "value1" || multiValues[1] != "value2" {
		t.Errorf("X-Multi-Value not forwarded correctly, got %v", multiValues)
	}

	// Verify hop-by-hop headers are NOT forwarded to client
	if v := resp.Header.Get("Connection"); v != "" {
		t.Errorf("Connection header should not be forwarded, got %q", v)
	}
	if v := resp.Header.Get("Keep-Alive"); v != "" {
		t.Errorf("Keep-Alive header should not be forwarded, got %q", v)
	}
	if v := resp.Header.Get("Transfer-Encoding"); v != "" {
		t.Errorf("Transfer-Encoding header should not be forwarded, got %q", v)
	}
	if v := resp.Header.Get("Proxy-Authenticate"); v != "" {
		t.Errorf("Proxy-Authenticate header should not be forwarded, got %q", v)
	}
	if v := resp.Header.Get("Upgrade"); v != "" {
		t.Errorf("Upgrade header should not be forwarded, got %q", v)
	}
}

func TestSSEPassthroughStreams(t *testing.T) {
	stateDir := writeTempCreds(t, "token-c", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	tokenServer := newAnthropicTokenServer(t, "token-c", "refresh-token")
	defer tokenServer.Close()

	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "data: one\n\n")
		flusher.Flush()
		time.Sleep(50 * time.Millisecond)
		io.WriteString(w, "data: two\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(server.URL + "/claude/v1/stream")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	first := readNextDataLine(t, reader, 200*time.Millisecond)
	if !strings.Contains(first, "data: one") {
		t.Fatalf("expected first event, got %q", first)
	}

	done := make(chan string, 1)
	go func() {
		done <- readNextDataLine(t, reader, time.Second)
	}()

	select {
	case second := <-done:
		if !strings.Contains(second, "data: two") {
			t.Fatalf("expected second event, got %q", second)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("second chunk did not stream in time")
	}
}

func TestSSENotCutOffByRequestTimeout(t *testing.T) {
	stateDir := writeTempCreds(t, "token-sse", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	tokenServer := newAnthropicTokenServer(t, "token-sse", "refresh-token")
	defer tokenServer.Close()

	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "data: start\n\n")
		flusher.Flush()
		time.Sleep(150 * time.Millisecond)
		io.WriteString(w, "data: after-timeout\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 50 * time.Millisecond}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(server.URL + "/claude/v1/stream")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	first := readNextDataLine(t, reader, 200*time.Millisecond)
	if !strings.Contains(first, "data: start") {
		t.Fatalf("expected first event, got %q", first)
	}

	second := readNextDataLine(t, reader, 500*time.Millisecond)
	if !strings.Contains(second, "data: after-timeout") {
		t.Fatalf("expected second event after timeout window, got %q", second)
	}
}

func TestChatGPTSSENotCutOffByRequestTimeout(t *testing.T) {
	stateDir := writeTempCreds(t, "token-sse", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	anthTokenServer := newAnthropicTokenServer(t, "token-sse", "refresh-token")
	defer anthTokenServer.Close()

	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "data: start\n\n")
		flusher.Flush()
		time.Sleep(150 * time.Millisecond)
		io.WriteString(w, "data: after-timeout\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	tokenServer := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"openai-access","refresh_token":"openai-refresh-new","expires_in":120}`)
	}))
	defer tokenServer.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.TestClaudeTokenEndpoint = anthTokenServer.URL
	cfg.Users = []User{{Name: "alice", Token: "secret"}}
	cfg.RequestTimeout = Duration{Duration: 50 * time.Millisecond}
	cfg.Providers = []string{"chatgpt"}
	cfg.TestChatGPTBaseURL = upstream.URL
	cfg.TestChatGPTTokenEndpoint = tokenServer.URL
	cfg.TestChatGPTRefreshToken = "openai-refresh"

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/chatgpt/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	first := readNextDataLine(t, reader, 200*time.Millisecond)
	if !strings.Contains(first, "data: start") {
		t.Fatalf("expected first event, got %q", first)
	}

	second := readNextDataLine(t, reader, 500*time.Millisecond)
	if !strings.Contains(second, "data: after-timeout") {
		t.Fatalf("expected second event after timeout window, got %q", second)
	}
}

func TestChatGPTSSEPassthroughStreams(t *testing.T) {
	stateDir := writeTempCreds(t, "token-c", "refresh-token", time.Now().Add(5*time.Minute).UnixMilli())

	anthTokenServer := newAnthropicTokenServer(t, "token-c", "refresh-token")
	defer anthTokenServer.Close()

	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "data: one\n\n")
		flusher.Flush()
		time.Sleep(50 * time.Millisecond)
		io.WriteString(w, "data: two\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	tokenServer := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"openai-access","refresh_token":"openai-refresh-new","account_id":"acct-123","expires_in":120}`)
	}))
	defer tokenServer.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.TestClaudeTokenEndpoint = anthTokenServer.URL
	cfg.Users = []User{{Name: "alice", Token: "secret"}}
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}
	cfg.Providers = []string{"chatgpt"}
	cfg.TestChatGPTTokenEndpoint = tokenServer.URL
	cfg.TestChatGPTBaseURL = upstream.URL
	cfg.TestChatGPTRefreshToken = "openai-refresh"

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/chatgpt/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	first := readNextDataLine(t, reader, 200*time.Millisecond)
	if !strings.Contains(first, "data: one") {
		t.Fatalf("expected first event, got %q", first)
	}

	done := make(chan string, 1)
	go func() {
		done <- readNextDataLine(t, reader, time.Second)
	}()

	select {
	case second := <-done:
		if !strings.Contains(second, "data: two") {
			t.Fatalf("expected second event, got %q", second)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("second chunk did not stream in time")
	}
}

func TestRefreshBeforeExpiry(t *testing.T) {
	stateDir := t.TempDir()
	credsPath := filepath.Join(stateDir, "claude", ".credentials.json")

	store := NewClaudeStore(credsPath)
	if err := store.Save(context.Background(), &TokenCredentials{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(10 * time.Second),
		Metadata:     &ClaudeMetadata{},
	}); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	refreshCalled := int32(0)
	tokenServer := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalled, 1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":120}`)
	}))
	defer tokenServer.Close()

	var upstreamAuth string
	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.StateDir = stateDir
	cfg.Providers = []string{"claude"}
	cfg.TestClaudeBaseURL = upstream.URL
	cfg.TestClaudeTokenEndpoint = tokenServer.URL
	cfg.RequestTimeout = Duration{Duration: 2 * time.Second}

	service, err := NewService(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := newHTTPTestServer(t, service)
	defer server.Close()

	resp, err := http.Get(server.URL + "/claude/v1/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if atomic.LoadInt32(&refreshCalled) == 0 {
		t.Fatalf("expected refresh to be called")
	}
	if upstreamAuth != "Bearer new-token" {
		t.Fatalf("expected refreshed token upstream, got %q", upstreamAuth)
	}

	store2 := NewClaudeStore(credsPath)
	stored, err := store2.Load(context.Background())
	if err != nil {
		t.Fatalf("read stored creds: %v", err)
	}
	if stored.AccessToken != "new-token" || stored.RefreshToken != "new-refresh" {
		t.Fatalf("stored credentials not updated: %+v", stored)
	}
}

func TestSanitizeHeadersMasksAuth(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer secret-token-123456789")
	h.Set("OpenAI-Organization", "org")
	masked := sanitizeHeaders(h)
	if val := masked.Get("Authorization"); val == "" || strings.Contains(val, "secret-token") {
		t.Fatalf("authorization not masked: %q", val)
	}
	if val := masked.Get("OpenAI-Organization"); val == "" || val == "org" {
		t.Fatalf("organization should be masked, got %q", val)
	}
}

func readNextDataLine(t *testing.T, reader *bufio.Reader, timeout time.Duration) string {
	t.Helper()
	for {
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- line
		}()
		select {
		case <-time.After(timeout):
			t.Fatalf("timed out waiting for SSE data line")
		case err := <-errCh:
			t.Fatalf("read SSE line: %v", err)
		case line := <-lineCh:
			if strings.TrimSpace(line) == "" {
				continue
			}
			return line
		}
	}
}

func writeTempCreds(t *testing.T, accessToken, refreshToken string, expiresAt int64) string {
	t.Helper()
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "claude", ".credentials.json")

	store := NewClaudeStore(path)
	if err := store.Save(context.Background(), &TokenCredentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.UnixMilli(expiresAt),
		Metadata:     &ClaudeMetadata{},
	}); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return stateDir
}
