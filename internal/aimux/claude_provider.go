package aimux

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// Claude API and OAuth constants
	claudeBaseURL            = "https://api.anthropic.com"
	claudeTokenEndpoint      = "https://console.anthropic.com/v1/oauth/token"
	claudeOAuthClientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeBetaValue          = "oauth-2025-04-20"
	claudeTokenRefreshBuffer = 60 * 1000 // 60 seconds in milliseconds
	claudePrefix             = "/claude"
)

type ClaudeProviderOptions struct {
	BaseURL       string
	TokenEndpoint string
}

type ClaudeProvider struct {
	baseProvider
	base *url.URL
}

func NewClaudeProvider(creds CredentialSource, opts *ClaudeProviderOptions) (*ClaudeProvider, error) {
	if creds == nil {
		return nil, fmt.Errorf("claude credentials missing")
	}
	baseURL := claudeBaseURL
	if opts != nil && opts.BaseURL != "" {
		baseURL = opts.BaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse anthropic base url: %w", err)
	}
	return &ClaudeProvider{
		baseProvider: baseProvider{creds: creds},
		base:         parsed,
	}, nil
}

func (p *ClaudeProvider) ID() string { return "claude" }

func (p *ClaudeProvider) BuildUpstreamRequest(ctx context.Context, downstream *http.Request, trimmedPath string) (*http.Request, error) {
	upstreamURL := p.buildURL(trimmedPath, downstream.URL.RawQuery)

	req, err := http.NewRequestWithContext(ctx, downstream.Method, upstreamURL, downstream.Body)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header = make(http.Header)
	copyHeaders(req.Header, downstream.Header)

	// Set the beta header
	clientBeta := req.Header.Get("anthropic-beta")
	if clientBeta == "" {
		req.Header.Set("anthropic-beta", claudeBetaValue)
	} else {
		req.Header.Set("anthropic-beta", claudeBetaValue+","+clientBeta)
	}

	authHeader, err := p.creds.AuthorizationHeader(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)

	extra, err := p.creds.ExtraHeaders(ctx)
	if err != nil {
		return nil, err
	}
	for key, values := range extra {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return req, nil
}

func (p *ClaudeProvider) buildURL(path, rawQuery string) string {
	u := *p.base
	u.Path = strings.TrimSuffix(p.base.Path, "/") + path
	u.RawQuery = rawQuery
	return u.String()
}
