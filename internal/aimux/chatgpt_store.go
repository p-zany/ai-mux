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

// ChatGPTMetadata contains ChatGPT-specific credential metadata
type ChatGPTMetadata struct {
	IDToken   string
	AccountID string
	APIKey    string // Optional API key field
}

// chatGPTCredentialFile represents the persisted format (PO)
type chatGPTCredentialFile struct {
	APIKey      string            `json:"OPENAI_API_KEY"`
	Tokens      chatGPTTokensFile `json:"tokens"`
	LastRefresh time.Time         `json:"last_refresh"`
}

type chatGPTTokensFile struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
}

// chatGPTDefaultTokenExpiry is the default expiry time for ChatGPT tokens
// when no expires_in or expires_at is provided in the response
const chatGPTDefaultTokenExpiry = 8 * 24 * time.Hour // 8 days

// ChatGPTStore handles persistence for ChatGPT credentials
type ChatGPTStore struct {
	path string
}

// NewChatGPTStore creates a new ChatGPT credential store
func NewChatGPTStore(path string) *ChatGPTStore {
	return &ChatGPTStore{path: path}
}

// Load reads ChatGPT credentials from file and converts to domain model
func (s *ChatGPTStore) Load(ctx context.Context) (*TokenCredentials, error) {
	po, err := s.readFile()
	if err != nil {
		// Allow missing file - will be created on first refresh
		if errors.Is(err, os.ErrNotExist) {
			return &TokenCredentials{}, nil
		}
		return nil, err
	}

	// Convert PO to DO
	creds := &TokenCredentials{
		AccessToken:  po.Tokens.AccessToken,
		RefreshToken: po.Tokens.RefreshToken,
		Metadata: &ChatGPTMetadata{
			IDToken:   po.Tokens.IDToken,
			AccountID: po.Tokens.AccountID,
			APIKey:    po.APIKey,
		},
	}

	// Calculate ExpiresAt from LastRefresh
	// ChatGPT doesn't provide expiry in the file, so we estimate based on last refresh
	if !po.LastRefresh.IsZero() {
		creds.ExpiresAt = po.LastRefresh.Add(chatGPTDefaultTokenExpiry)
	}

	return creds, nil
}

// Save persists domain model credentials to ChatGPT file format
func (s *ChatGPTStore) Save(ctx context.Context, creds *TokenCredentials) error {
	// Convert DO to PO
	po := chatGPTCredentialFile{
		Tokens: chatGPTTokensFile{
			AccessToken:  creds.AccessToken,
			RefreshToken: creds.RefreshToken,
		},
		LastRefresh: time.Now().UTC(),
	}

	// Extract metadata if present
	if meta, ok := creds.Metadata.(*ChatGPTMetadata); ok {
		po.APIKey = meta.APIKey
		po.Tokens.IDToken = meta.IDToken
		po.Tokens.AccountID = meta.AccountID
	}

	return s.writeFile(po)
}

// readFile reads the ChatGPT credential file
func (s *ChatGPTStore) readFile() (chatGPTCredentialFile, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		return chatGPTCredentialFile{}, err
	}

	// Security: enforce strict permissions
	if info.Mode().Perm()&0o077 != 0 {
		return chatGPTCredentialFile{}, fmt.Errorf("chatgpt credential file %s must have 0600 permissions", s.path)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return chatGPTCredentialFile{}, fmt.Errorf("read chatgpt credentials: %w", err)
	}

	var po chatGPTCredentialFile
	if err := json.Unmarshal(data, &po); err != nil {
		return chatGPTCredentialFile{}, fmt.Errorf("parse chatgpt credentials: %w", err)
	}

	if po.Tokens.RefreshToken == "" {
		return chatGPTCredentialFile{}, errors.New("chatgpt credential file missing tokens.refresh_token")
	}

	return po, nil
}

// writeFile writes the ChatGPT credential file
func (s *ChatGPTStore) writeFile(po chatGPTCredentialFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(po, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, defaultFilePerm)
}

// ChatGPTHeaderProvider implements ExtraHeaderProvider for ChatGPT
type ChatGPTHeaderProvider struct{}

// ExtraHeaders returns ChatGPT-specific headers
func (p *ChatGPTHeaderProvider) ExtraHeaders(metadata any) (http.Header, error) {
	meta, ok := metadata.(*ChatGPTMetadata)
	if !ok || meta == nil {
		return nil, nil
	}

	if meta.AccountID == "" {
		return nil, nil
	}

	headers := make(http.Header)
	headers.Set("ChatGPT-Account-Id", meta.AccountID)
	return headers, nil
}
