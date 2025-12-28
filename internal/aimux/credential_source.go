package aimux

import (
	"context"
	"net/http"
	"os"
	"time"
)

const (
	defaultRefreshInterval             = 10 * time.Minute
	defaultFilePerm        os.FileMode = 0o600
	maxResponseSize                    = 1 << 20 // 1MB limit for OAuth responses
)

type CredentialSource interface {
	AuthorizationHeader(ctx context.Context) (string, error)
	ExtraHeaders(ctx context.Context) (http.Header, error)
	IsAvailable() bool
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// maskToken masks a token for safe logging, showing only a short prefix.
func maskToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:8] + "..."
}
