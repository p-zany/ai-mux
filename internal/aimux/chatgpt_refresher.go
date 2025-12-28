package aimux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatGPTRefresher handles OAuth token refresh for ChatGPT
type ChatGPTRefresher struct {
	tokenEndpoint string
	clientID      string
	scope         string
	httpClient    *http.Client
}

// ChatGPTRefresherOptions configures the ChatGPT refresher
type ChatGPTRefresherOptions struct {
	TokenEndpoint string
	ClientID      string
	Scope         string
	HTTPClient    *http.Client
}

// NewChatGPTRefresher creates a new ChatGPT token refresher
func NewChatGPTRefresher(opts ChatGPTRefresherOptions) *ChatGPTRefresher {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}
	return &ChatGPTRefresher{
		tokenEndpoint: opts.TokenEndpoint,
		clientID:      opts.ClientID,
		scope:         opts.Scope,
		httpClient:    opts.HTTPClient,
	}
}

// Refresh performs the ChatGPT OAuth refresh flow
func (r *ChatGPTRefresher) Refresh(ctx context.Context, refreshToken string) (*TokenCredentials, error) {
	if refreshToken == "" {
		return nil, errors.New("chatgpt refresh token is missing")
	}

	// Prepare request body
	body, err := json.Marshal(map[string]string{
		"client_id":     r.clientID,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"scope":         r.scope,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal chatgpt refresh body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build chatgpt refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatgpt refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return nil, fmt.Errorf("chatgpt refresh failed: %s %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	// Parse response
	var tokenResp struct {
		AccessToken  string  `json:"access_token"`
		IDToken      string  `json:"id_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		ExpiresAt    int64   `json:"expires_at"`
		AccountID    string  `json:"account_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode chatgpt refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, errors.New("chatgpt refresh missing access_token")
	}

	// Convert to domain model
	now := time.Now().UTC()
	creds := &TokenCredentials{
		AccessToken: tokenResp.AccessToken,
		Metadata: &ChatGPTMetadata{
			IDToken:   tokenResp.IDToken,
			AccountID: tokenResp.AccountID,
		},
	}

	// Use new refresh token if provided, otherwise keep the old one
	if tokenResp.RefreshToken != "" {
		creds.RefreshToken = tokenResp.RefreshToken
	} else {
		creds.RefreshToken = refreshToken
	}

	// Calculate expiry time
	switch {
	case tokenResp.ExpiresAt > 0:
		creds.ExpiresAt = time.Unix(tokenResp.ExpiresAt, 0)
	case tokenResp.ExpiresIn > 0:
		creds.ExpiresAt = now.Add(time.Duration(tokenResp.ExpiresIn * float64(time.Second)))
	default:
		// ChatGPT tokens default to 8 days expiry when no expiry info provided
		creds.ExpiresAt = now.Add(chatGPTDefaultTokenExpiry)
	}

	return creds, nil
}
