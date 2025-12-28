package aimux

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test server: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = l
	server.Start()
	return server
}

func newAnthropicTokenServer(t *testing.T, accessToken, refreshToken string) *httptest.Server {
	t.Helper()
	return newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"access_token":"%s","refresh_token":"%s","expires_in":120}`, accessToken, refreshToken)))
	}))
}
