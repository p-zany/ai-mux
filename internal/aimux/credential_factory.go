package aimux

import (
	"errors"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
)

// NewChatGPTCredentials creates a ChatGPT credential manager using the new architecture
func NewChatGPTCredentials(
	path string,
	tokenEndpoint string,
	clientID string,
	scope string,
	refreshToken string,
	refreshInterval time.Duration,
	checkInterval time.Duration,
	httpClient *http.Client,
	logger *zap.Logger,
) (CredentialSource, error) {
	// Create store
	store := NewChatGPTStore(path)

	// Load existing credentials or prepare for initial setup
	// Check if we have a refresh token (either from file or parameter)
	if refreshToken == "" {
		// Try loading from file
		po, err := store.readFile()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil && po.Tokens.RefreshToken != "" {
			refreshToken = po.Tokens.RefreshToken
		}
	}

	if refreshToken == "" {
		return nil, errors.New("chatgpt refresh token is required")
	}

	// Inject initial refresh token if file doesn't exist
	// by creating minimal credentials
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		initialCreds := &TokenCredentials{
			RefreshToken: refreshToken,
			Metadata:     &ChatGPTMetadata{},
		}
		if err := store.Save(nil, initialCreds); err != nil {
			logger.Warn("failed to save initial credentials", zap.Error(err))
		}
	}

	// Create refresher
	refresher := NewChatGPTRefresher(ChatGPTRefresherOptions{
		TokenEndpoint: tokenEndpoint,
		ClientID:      clientID,
		Scope:         scope,
		HTTPClient:    httpClient,
	})

	// Create header provider
	headerProvider := &ChatGPTHeaderProvider{}

	// Create credential manager
	return NewCredentialManager(CredentialManagerOptions{
		Store:           store,
		Refresher:       refresher,
		HeaderProvider:  headerProvider,
		Logger:          logger,
		RefreshInterval: refreshInterval,
		CheckInterval:   checkInterval,
	})
}

// NewClaudeCredentials creates a Claude credential manager using the new architecture
func NewClaudeCredentials(
	path string,
	tokenEndpoint string,
	refreshInterval time.Duration,
	httpClient *http.Client,
	logger *zap.Logger,
) (CredentialSource, error) {
	// Validate that credentials file exists
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	// Create store
	store := NewClaudeStore(path)

	// Create refresher
	refresher := NewClaudeRefresher(ClaudeRefresherOptions{
		TokenEndpoint: tokenEndpoint,
		HTTPClient:    httpClient,
	})

	// Create header provider
	headerProvider := &ClaudeHeaderProvider{}

	// Create credential manager
	return NewCredentialManager(CredentialManagerOptions{
		Store:           store,
		Refresher:       refresher,
		HeaderProvider:  headerProvider,
		Logger:          logger,
		RefreshInterval: refreshInterval,
		CheckInterval:   time.Minute, // Default check interval for Claude
	})
}
