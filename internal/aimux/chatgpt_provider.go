package aimux

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// ChatGPT OAuth and API constants
	chatGPTClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	chatGPTTokenEndpoint = "https://auth.openai.com/oauth/token"
	chatGPTBaseURL       = "https://chatgpt.com/backend-api/codex"
	chatGPTScope         = "openid profile email"
	chatGPTPrefix        = "/chatgpt"
)

type ChatGPTProviderOptions struct {
	BaseURL       string
	TokenEndpoint string
}

type ChatGPTProvider struct {
	baseProvider
	base *url.URL
}

func NewChatGPTProvider(creds CredentialSource, opts *ChatGPTProviderOptions) (*ChatGPTProvider, error) {
	if creds == nil {
		return nil, fmt.Errorf("chatgpt credentials missing")
	}
	baseURL := chatGPTBaseURL
	if opts != nil && opts.BaseURL != "" {
		baseURL = opts.BaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse chatgpt base url: %w", err)
	}
	return &ChatGPTProvider{
		baseProvider: baseProvider{creds: creds},
		base:         parsed,
	}, nil
}

func (p *ChatGPTProvider) ID() string { return "chatgpt" }

func (p *ChatGPTProvider) BuildUpstreamRequest(ctx context.Context, downstream *http.Request, trimmedPath string) (*http.Request, error) {
	upstreamURL := p.buildURL(trimmedPath, downstream.URL.RawQuery)
	req, err := http.NewRequestWithContext(ctx, downstream.Method, upstreamURL, downstream.Body)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header = make(http.Header)
	copyHeaders(req.Header, downstream.Header)

	// Remove Anthropic-only headers that should not be forwarded to ChatGPT
	req.Header.Del("anthropic-beta")

	authHeader, err := p.creds.AuthorizationHeader(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)

	// ChatGPT-Account-Id is handled by ExtraHeaders
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

func (p *ChatGPTProvider) buildURL(path, rawQuery string) string {
	u := *p.base
	// ChatGPT backend API doesn't use /v1 prefix, remove it if present
	trimmedPath := strings.TrimPrefix(path, "/v1")
	if trimmedPath == "" {
		trimmedPath = "/"
	}
	u.Path = strings.TrimSuffix(p.base.Path, "/") + trimmedPath
	u.RawQuery = rawQuery
	return u.String()
}
