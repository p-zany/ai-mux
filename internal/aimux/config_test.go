package aimux

import (
	"context"
	"testing"
	"time"
)

func TestValidateChatGPTRequiresCredentials(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []string{"chatgpt"}
	cfg.StateDir = t.TempDir()

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error when credential file is missing")
	}
}

func TestValidateBothProvidersWork(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []string{"claude", "chatgpt"}
	cfg.StateDir = t.TempDir()

	// Create Claude credentials using Store
	claudeStore := NewClaudeStore(cfg.CredentialPath())
	if err := claudeStore.Save(context.Background(), &TokenCredentials{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		Metadata:     &ClaudeMetadata{},
	}); err != nil {
		t.Fatalf("write claude credentials: %v", err)
	}

	// Create ChatGPT credentials using Store
	chatgptStore := NewChatGPTStore(cfg.ChatGPTCredentialPath())
	if err := chatgptStore.Save(context.Background(), &TokenCredentials{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		Metadata:     &ChatGPTMetadata{},
	}); err != nil {
		t.Fatalf("write chatgpt credentials: %v", err)
	}

	// Both providers should validate successfully (hardcoded prefixes don't overlap)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation failure with both providers: %v", err)
	}
}
