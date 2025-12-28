package aimux

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// Test helpers for reading/writing credential files
func writeClaudeTestFile(t *testing.T, path string, creds *TokenCredentials) {
	t.Helper()
	store := NewClaudeStore(path)
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("write claude credentials: %v", err)
	}
}

func writeChatGPTTestFile(t *testing.T, path string, creds *TokenCredentials) {
	t.Helper()
	store := NewChatGPTStore(path)
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("write chatgpt credentials: %v", err)
	}
}

func TestReadCredentialsCamelCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude", ".credentials.json")
	data := `{
		"claudeAiOauth": {
			"accessToken": "sk-ant-camel",
			"refreshToken": "sk-ant-refresh",
			"expiresAt": 123456789,
			"scopes": ["user:inference"],
			"subscriptionType": "max",
			"isMax": true,
			"rateLimitTier": "default_claude_max_20x"
		}
	}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Use Store to load and verify
	store := NewClaudeStore(path)
	creds, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}

	if creds.AccessToken != "sk-ant-camel" || creds.RefreshToken != "sk-ant-refresh" {
		t.Fatalf("unexpected tokens: %+v", creds)
	}

	meta, ok := creds.Metadata.(*ClaudeMetadata)
	if !ok {
		t.Fatalf("expected ClaudeMetadata, got %T", creds.Metadata)
	}

	if len(meta.Scopes) != 1 || meta.Scopes[0] != "user:inference" {
		t.Fatalf("unexpected scopes: %+v", meta.Scopes)
	}
	if meta.SubscriptionType != "max" || !meta.IsMax || meta.RateLimitTier != "default_claude_max_20x" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestClaudeExtraHeadersEmpty(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "claude", ".credentials.json")

	// Write test credentials
	writeClaudeTestFile(t, credsPath, &TokenCredentials{
		AccessToken:  "sk-ant-token",
		RefreshToken: "sk-ant-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
		Metadata:     &ClaudeMetadata{},
	})

	// Create credential manager
	source, err := NewClaudeCredentials(
		credsPath,
		"http://dummy",
		time.Hour,
		nil,
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new claude credentials: %v", err)
	}

	// Verify ExtraHeaders returns nil (Claude doesn't use extra headers)
	headers, err := source.ExtraHeaders(context.Background())
	if err != nil {
		t.Fatalf("extra headers: %v", err)
	}
	if headers != nil {
		t.Fatalf("expected no extra headers, got %#v", headers)
	}
}

func TestClaudeCredentialSourceRefreshAndPersist(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "claude", ".credentials.json")

	// Write credentials that will need refresh (expires soon)
	writeClaudeTestFile(t, credsPath, &TokenCredentials{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(200 * time.Millisecond), // expires within refreshInterval (300ms)
		Metadata:     &ClaudeMetadata{},
	})

	var callCount atomic.Int32

	// Mock token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":120}`)
	}))
	defer tokenServer.Close()

	// Create credential manager
	source, err := NewClaudeCredentials(
		credsPath,
		tokenServer.URL,
		300*time.Millisecond,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new claude credentials: %v", err)
	}

	// Before Start, token is still valid, so IsAvailable should be true
	if !source.IsAvailable() {
		t.Fatal("expected IsAvailable=true before Start when token is still valid")
	}

	// Start should trigger refresh (token expires within refreshInterval)
	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer source.Shutdown(context.Background())

	// Verify new token is being used
	header, err := source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("authorization header: %v", err)
	}
	if header != "Bearer new-token" {
		t.Fatalf("expected new token, got: %q", header)
	}

	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected exactly one refresh call (at Start), got %d", got)
	}

	// Verify credentials were persisted to file
	store := NewClaudeStore(credsPath)
	persistedCreds, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load persisted credentials: %v", err)
	}
	if persistedCreds.AccessToken != "new-token" || persistedCreds.RefreshToken != "new-refresh" {
		t.Fatalf("credentials not persisted correctly: %+v", persistedCreds)
	}
}

func TestWriteCredentialsCamelCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude", ".credentials.json")

	// Write using Store
	store := NewClaudeStore(path)
	if err := store.Save(context.Background(), &TokenCredentials{
		AccessToken:  "sk-ant-token",
		RefreshToken: "sk-ant-refresh",
		ExpiresAt:    time.UnixMilli(987654321),
		Metadata: &ClaudeMetadata{
			Scopes:           []string{"user:inference"},
			SubscriptionType: "max",
			IsMax:            true,
			RateLimitTier:    "tier1",
		},
	}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	// Read raw JSON to verify format
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var wrapper map[string]any
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	claudeData, ok := wrapper["claudeAiOauth"].(map[string]any)
	if !ok {
		t.Fatalf("expected claudeAiOauth field, got %+v", wrapper)
	}

	if claudeData["accessToken"] != "sk-ant-token" {
		t.Fatalf("unexpected accessToken: %v", claudeData["accessToken"])
	}
	if claudeData["refreshToken"] != "sk-ant-refresh" {
		t.Fatalf("unexpected refreshToken: %v", claudeData["refreshToken"])
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude", ".credentials.json")

	original := &TokenCredentials{
		AccessToken:  "sk-ant-token",
		RefreshToken: "sk-ant-refresh",
		ExpiresAt:    time.Now().Truncate(time.Millisecond),
		Metadata: &ClaudeMetadata{
			Scopes:           []string{"scope1", "scope2"},
			SubscriptionType: "pro",
			IsMax:            false,
			RateLimitTier:    "default",
		},
	}

	// Write
	store := NewClaudeStore(path)
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Read
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Verify
	if loaded.AccessToken != original.AccessToken || loaded.RefreshToken != original.RefreshToken {
		t.Fatalf("tokens mismatch: %+v vs %+v", loaded, original)
	}

	loadedMeta, ok := loaded.Metadata.(*ClaudeMetadata)
	if !ok {
		t.Fatalf("expected ClaudeMetadata, got %T", loaded.Metadata)
	}

	originalMeta := original.Metadata.(*ClaudeMetadata)
	if !reflect.DeepEqual(loadedMeta, originalMeta) {
		t.Fatalf("metadata mismatch: %+v vs %+v", loadedMeta, originalMeta)
	}
}

func TestChatGPTCredentialSourceRefreshAndPersist(t *testing.T) {
	var callCount atomic.Int32

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"access_token":"new-token",
			"refresh_token":"new-refresh",
			"account_id":"acct-123",
			"expires_in":120
		}`)
	}))
	defer tokenServer.Close()

	path := filepath.Join(t.TempDir(), "chatgpt", "auth.json")

	// Create credential manager
	source, err := NewChatGPTCredentials(
		path,
		tokenServer.URL,
		chatGPTClientID,
		chatGPTScope,
		"seed-refresh",
		30*time.Millisecond,
		20*time.Millisecond,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new chatgpt credentials: %v", err)
	}

	// Start should trigger initial refresh
	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer source.Shutdown(context.Background())

	// Verify new token is being used
	header, err := source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("authorization header: %v", err)
	}
	if header != "Bearer new-token" {
		t.Fatalf("expected new token, got: %q", header)
	}

	// Verify AccountID header
	extraHeaders, err := source.ExtraHeaders(context.Background())
	if err != nil {
		t.Fatalf("extra headers: %v", err)
	}
	if extraHeaders.Get("ChatGPT-Account-Id") != "acct-123" {
		t.Fatalf("expected ChatGPT-Account-Id header, got: %+v", extraHeaders)
	}

	// Verify credentials were persisted
	store := NewChatGPTStore(path)
	persistedCreds, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load persisted credentials: %v", err)
	}
	if persistedCreds.AccessToken != "new-token" {
		t.Fatalf("credentials not persisted correctly")
	}

	meta, ok := persistedCreds.Metadata.(*ChatGPTMetadata)
	if !ok || meta.AccountID != "acct-123" {
		t.Fatalf("metadata not persisted correctly: %+v", persistedCreds.Metadata)
	}
}

func TestChatGPTDefaultTokenExpiry(t *testing.T) {
	var callCount atomic.Int32

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// No expires_in or expires_at - should use default 8 days
		io.WriteString(w, `{"access_token":"token","refresh_token":"refresh"}`)
	}))
	defer tokenServer.Close()

	path := filepath.Join(t.TempDir(), "chatgpt", "auth.json")

	source, err := NewChatGPTCredentials(
		path,
		tokenServer.URL,
		chatGPTClientID,
		chatGPTScope,
		"seed-refresh",
		time.Hour,
		time.Hour,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new chatgpt credentials: %v", err)
	}

	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer source.Shutdown(context.Background())

	// Load persisted credentials and check expiry
	store := NewChatGPTStore(path)
	creds, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}

	if creds.ExpiresAt.IsZero() {
		t.Fatalf("expiresAt should not be zero")
	}

	// ExpiresAt should be ~8 days from now (default expiry)
	expectedExpiry := time.Now().Add(chatGPTDefaultTokenExpiry)
	diff := creds.ExpiresAt.Sub(expectedExpiry)
	if diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("expiresAt should be ~8 days from now, got diff=%v (expiresAt=%v, expected=%v)", diff, creds.ExpiresAt, expectedExpiry)
	}
}

func TestChatGPTLoadCredentialsWithLastRefresh(t *testing.T) {
	// Test that expiry is calculated from LastRefresh when loading from file
	path := filepath.Join(t.TempDir(), "chatgpt", "auth.json")
	lastRefresh := time.Now().UTC().Add(-24 * time.Hour) // 1 day ago

	// Manually write file with LastRefresh field
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	data := map[string]any{
		"tokens": map[string]string{
			"access_token":  "test-access",
			"refresh_token": "test-refresh",
		},
		"last_refresh": lastRefresh.Format(time.RFC3339Nano),
	}
	jsonData, _ := json.MarshalIndent(data, "", "  ")
	if err := os.WriteFile(path, jsonData, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Create credential manager (won't start, just load)
	_, err := NewChatGPTCredentials(
		path,
		"http://dummy",
		chatGPTClientID,
		chatGPTScope,
		"",
		time.Hour,
		time.Hour,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new chatgpt credentials: %v", err)
	}

	// Verify expiry was calculated from LastRefresh
	store := NewChatGPTStore(path)
	creds, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}

	if creds.ExpiresAt.IsZero() {
		t.Fatalf("expiresAt should not be zero when LastRefresh is set")
	}

	expectedExpiry := lastRefresh.Add(chatGPTDefaultTokenExpiry)
	diff := creds.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Fatalf("expiresAt should be LastRefresh + 8 days, got diff=%v (expiresAt=%v, expected=%v)", diff, creds.ExpiresAt, expectedExpiry)
	}
}

func TestCredentialSourceIsAvailableAfterSuccessfulRefresh(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "claude", ".credentials.json")

	writeClaudeTestFile(t, credsPath, &TokenCredentials{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(200 * time.Millisecond),
		Metadata:     &ClaudeMetadata{},
	})

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":120}`)
	}))
	defer tokenServer.Close()

	source, err := NewClaudeCredentials(
		credsPath,
		tokenServer.URL,
		300*time.Millisecond,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new claude credentials: %v", err)
	}

	if !source.IsAvailable() {
		t.Fatal("expected IsAvailable=true before Start when token is still valid")
	}

	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer source.Shutdown(context.Background())

	if !source.IsAvailable() {
		t.Fatal("expected IsAvailable=true after successful refresh")
	}
}

func TestCredentialSourceIsAvailableAfterFailedRefresh(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "claude", ".credentials.json")

	writeClaudeTestFile(t, credsPath, &TokenCredentials{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(200 * time.Millisecond),
		Metadata:     &ClaudeMetadata{},
	})

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tokenServer.Close()

	source, err := NewClaudeCredentials(
		credsPath,
		tokenServer.URL,
		300*time.Millisecond,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new claude credentials: %v", err)
	}

	if !source.IsAvailable() {
		t.Fatal("expected IsAvailable=true before Start when token is still valid")
	}

	if err := source.Start(context.Background()); err != nil {
		t.Fatal("Start should not fail even if initial refresh fails")
	}
	defer source.Shutdown(context.Background())

	if !source.IsAvailable() {
		t.Fatal("expected IsAvailable=true after failed refresh while token is still valid")
	}

	time.Sleep(350 * time.Millisecond)

	if source.IsAvailable() {
		t.Fatal("expected IsAvailable=false after token expiry without refresh")
	}

	_, err = source.AuthorizationHeader(context.Background())
	if err == nil {
		t.Fatal("expected error when getting auth header for expired token")
	}
}

func TestProviderIsAvailableDelegatesToCredentialSource(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "claude", ".credentials.json")

	writeClaudeTestFile(t, credsPath, &TokenCredentials{
		AccessToken:  "token",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
		Metadata:     &ClaudeMetadata{},
	})

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":120}`)
	}))
	defer tokenServer.Close()

	creds, err := NewClaudeCredentials(
		credsPath,
		tokenServer.URL,
		time.Hour,
		&http.Client{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new claude credentials: %v", err)
	}

	provider, err := NewClaudeProvider(creds, nil)
	if err != nil {
		t.Fatalf("new claude provider: %v", err)
	}

	if !provider.IsAvailable() {
		t.Fatal("expected provider IsAvailable=true before Start when token is still valid")
	}

	if err := creds.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer creds.Shutdown(context.Background())

	if !provider.IsAvailable() {
		t.Fatal("expected provider IsAvailable=true after credential source started")
	}
}
