package aimux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ClaudeMetadata contains Claude-specific credential metadata
type ClaudeMetadata struct {
	Scopes           []string
	SubscriptionType string
	IsMax            bool
	RateLimitTier    string
}

// claudeCredentialFile represents the persisted format (PO)
type claudeCredentialFile struct {
	Claude *claudeCredentialData `json:"claudeAiOauth"`
}

type claudeCredentialData struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // milliseconds since epoch
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	IsMax            bool     `json:"isMax,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

// ClaudeStore handles persistence for Claude credentials
type ClaudeStore struct {
	path string
}

// NewClaudeStore creates a new Claude credential store
func NewClaudeStore(path string) *ClaudeStore {
	return &ClaudeStore{path: path}
}

// Load reads Claude credentials from file and converts to domain model
func (s *ClaudeStore) Load(ctx context.Context) (*TokenCredentials, error) {
	po, err := s.readFile()
	if err != nil {
		return nil, err
	}

	// Convert PO to DO
	creds := &TokenCredentials{
		AccessToken:  po.AccessToken,
		RefreshToken: po.RefreshToken,
		Metadata: &ClaudeMetadata{
			Scopes:           po.Scopes,
			SubscriptionType: po.SubscriptionType,
			IsMax:            po.IsMax,
			RateLimitTier:    po.RateLimitTier,
		},
	}

	// Convert ExpiresAt from milliseconds to time.Time
	if po.ExpiresAt > 0 {
		creds.ExpiresAt = time.UnixMilli(po.ExpiresAt)
	}

	return creds, nil
}

// Save persists domain model credentials to Claude file format
func (s *ClaudeStore) Save(ctx context.Context, creds *TokenCredentials) error {
	// Convert DO to PO
	po := claudeCredentialData{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
	}

	// Convert ExpiresAt to milliseconds
	if !creds.ExpiresAt.IsZero() {
		po.ExpiresAt = creds.ExpiresAt.UnixMilli()
	}

	// Extract metadata if present
	if meta, ok := creds.Metadata.(*ClaudeMetadata); ok {
		po.Scopes = meta.Scopes
		po.SubscriptionType = meta.SubscriptionType
		po.IsMax = meta.IsMax
		po.RateLimitTier = meta.RateLimitTier
	}

	return s.writeFile(po)
}

// readFile reads the Claude credential file
func (s *ClaudeStore) readFile() (claudeCredentialData, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		return claudeCredentialData{}, fmt.Errorf("read credentials: %w", err)
	}

	// Security: enforce strict permissions
	if info.Mode().Perm()&0o077 != 0 {
		return claudeCredentialData{}, fmt.Errorf("claude credential file %s must have 0600 permissions", s.path)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return claudeCredentialData{}, fmt.Errorf("read credentials: %w", err)
	}

	var wrapper claudeCredentialFile
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return claudeCredentialData{}, fmt.Errorf("parse credentials: %w", err)
	}

	if wrapper.Claude == nil {
		return claudeCredentialData{}, errors.New("claudeAiOauth field not found in credentials")
	}

	return *wrapper.Claude, nil
}

// writeFile writes the Claude credential file
func (s *ClaudeStore) writeFile(po claudeCredentialData) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(map[string]any{
		"claudeAiOauth": po,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, defaultFilePerm)
}

// ClaudeHeaderProvider implements ExtraHeaderProvider for Claude
type ClaudeHeaderProvider struct{}

// ExtraHeaders returns Claude-specific headers (currently none)
func (p *ClaudeHeaderProvider) ExtraHeaders(metadata any) (http.Header, error) {
	// Claude doesn't require extra headers currently
	return nil, nil
}
