package aimux

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type Provider interface {
	ID() string
	IsAvailable() bool
	BuildUpstreamRequest(ctx context.Context, downstream *http.Request, trimmedPath string) (*http.Request, error)
	Shutdown(ctx context.Context) error
}

type baseProvider struct {
	creds CredentialSource
}

func (b *baseProvider) IsAvailable() bool {
	return b.creds.IsAvailable()
}

func (b *baseProvider) Shutdown(ctx context.Context) error {
	return b.creds.Shutdown(ctx)
}

type providerRegistration struct {
	prefix   string
	provider Provider
}

type providerRegistry struct {
	entries []providerRegistration
}

func newProviderRegistry(entries []providerRegistration) (*providerRegistry, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("no providers configured")
	}
	normalized := make([]providerRegistration, len(entries))
	for i, e := range entries {
		prefix := strings.TrimSuffix(e.prefix, "/")
		if prefix == "" {
			return nil, fmt.Errorf("provider prefix cannot be empty")
		}
		normalized[i] = providerRegistration{
			prefix:   prefix,
			provider: e.provider,
		}
	}
	if err := validateProviderPrefixes(normalized); err != nil {
		return nil, err
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return len(normalized[i].prefix) > len(normalized[j].prefix)
	})
	return &providerRegistry{entries: normalized}, nil
}

func validateProviderPrefixes(entries []providerRegistration) error {
	for i := 0; i < len(entries); i++ {
		a := entries[i].prefix
		for j := i + 1; j < len(entries); j++ {
			b := entries[j].prefix
			if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
				return fmt.Errorf("provider prefixes %q and %q overlap", a, b)
			}
		}
	}
	return nil
}

func (r *providerRegistry) Resolve(path string) (Provider, string, bool) {
	for _, entry := range r.entries {
		if trimmed, ok := trimPrefix(path, entry.prefix); ok {
			return entry.provider, trimmed, true
		}
	}
	return nil, "", false
}

func trimPrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	if len(path) > len(prefix) && path[len(prefix)] != '/' {
		return "", false
	}
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "/", true
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return trimmed, true
}

func (r *providerRegistry) providers() []Provider {
	providers := make([]Provider, len(r.entries))
	for i, entry := range r.entries {
		providers[i] = entry.provider
	}
	return providers
}
