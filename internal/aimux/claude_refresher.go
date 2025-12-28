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

// ClaudeRefresher handles OAuth token refresh for Claude
type ClaudeRefresher struct {
	tokenEndpoint string
	clientID      string
	httpClient    *http.Client
}

// ClaudeRefresherOptions configures the Claude refresher
type ClaudeRefresherOptions struct {
	TokenEndpoint string
	ClientID      string
	HTTPClient    *http.Client
}

// NewClaudeRefresher creates a new Claude token refresher
func NewClaudeRefresher(opts ClaudeRefresherOptions) *ClaudeRefresher {
	if opts.TokenEndpoint == "" {
		opts.TokenEndpoint = claudeTokenEndpoint
	}
	if opts.ClientID == "" {
		opts.ClientID = claudeOAuthClientID
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}
	return &ClaudeRefresher{
		tokenEndpoint: opts.TokenEndpoint,
		clientID:      opts.ClientID,
		httpClient:    opts.HTTPClient,
	}
}

// Refresh performs the Claude OAuth refresh flow
func (r *ClaudeRefresher) Refresh(ctx context.Context, refreshToken string) (*TokenCredentials, error) {
	if refreshToken == "" {
		return nil, errors.New("refresh token is empty")
	}

	// Prepare request body
	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     r.clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal refresh body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return nil, fmt.Errorf("refresh failed: %s %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	// Parse response
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		ExpiresAt    int64  `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, errors.New("refresh response missing access_token")
	}

	// Convert to domain model
	creds := &TokenCredentials{
		AccessToken: tokenResp.AccessToken,
		Metadata:    &ClaudeMetadata{},
	}

	// Use new refresh token if provided, otherwise keep the old one
	if tokenResp.RefreshToken != "" {
		creds.RefreshToken = tokenResp.RefreshToken
	} else {
		creds.RefreshToken = refreshToken
	}

	// Calculate expiry time
	now := time.Now()
	switch {
	case tokenResp.ExpiresAt > 0:
		creds.ExpiresAt = time.UnixMilli(tokenResp.ExpiresAt)
	case tokenResp.ExpiresIn > 0:
		creds.ExpiresAt = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return creds, nil
}
