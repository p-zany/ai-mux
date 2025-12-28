package aimux

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TokenCredentials represents the unified domain model for OAuth credentials
type TokenCredentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time

	// Provider-specific metadata stored as opaque data
	// Providers can store additional fields here and retrieve them via type assertion
	Metadata any
}

type CredentialStore interface {
	Load(ctx context.Context) (*TokenCredentials, error)
	Save(ctx context.Context, creds *TokenCredentials) error
}

type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (*TokenCredentials, error)
}

type ExtraHeaderProvider interface {
	ExtraHeaders(metadata any) (http.Header, error)
}

type CredentialManagerOptions struct {
	Store           CredentialStore
	Refresher       TokenRefresher
	HeaderProvider  ExtraHeaderProvider
	Logger          *zap.Logger
	RefreshInterval time.Duration // how long before expiry to refresh
	CheckInterval   time.Duration // how often to check if refresh is needed
}

type CredentialManager struct {
	store           CredentialStore
	refresher       TokenRefresher
	headerProvider  ExtraHeaderProvider
	logger          *zap.Logger
	refreshInterval time.Duration
	checkInterval   time.Duration

	mu      sync.RWMutex
	creds   *TokenCredentials
	started bool
	stopCh  chan struct{}
}

func NewCredentialManager(opts CredentialManagerOptions) (*CredentialManager, error) {
	if opts.Store == nil {
		return nil, errors.New("credential store is required")
	}
	if opts.Refresher == nil {
		return nil, errors.New("token refresher is required")
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = defaultRefreshInterval
	}
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = time.Minute
	}

	m := &CredentialManager{
		store:           opts.Store,
		refresher:       opts.Refresher,
		headerProvider:  opts.HeaderProvider,
		logger:          opts.Logger,
		refreshInterval: opts.RefreshInterval,
		checkInterval:   opts.CheckInterval,
	}

	if err := m.load(nil); err != nil {
		return nil, err
	}

	return m, nil
}

// Start kicks off background refresh. If the initial refresh fails, it will retry later.
func (m *CredentialManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.stopCh = make(chan struct{})
	interval := m.checkInterval
	m.mu.Unlock()

	if err := m.refreshIfNeeded(ctx, "startup"); err != nil {
		m.logger.Warn("initial credential refresh failed, will retry in background", zap.Error(err))
	}

	go m.refreshLoop(ctx, interval)
	return nil
}

func (m *CredentialManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	stop := m.stopCh
	m.stopCh = nil
	m.started = false
	m.mu.Unlock()

	close(stop)
	return nil
}

func (m *CredentialManager) AuthorizationHeader(ctx context.Context) (string, error) {
	m.mu.RLock()
	valid := m.tokenValidLocked(time.Now())
	token := ""
	if valid {
		token = m.creds.AccessToken
	}
	m.mu.RUnlock()

	if !valid {
		return "", errors.New("provider is not available: credentials not ready")
	}
	return "Bearer " + token, nil
}

func (m *CredentialManager) ExtraHeaders(ctx context.Context) (http.Header, error) {
	if m.headerProvider == nil {
		return nil, nil
	}

	m.mu.RLock()
	var metadata any
	if m.creds != nil {
		metadata = m.creds.Metadata
	}
	m.mu.RUnlock()

	return m.headerProvider.ExtraHeaders(metadata)
}

func (m *CredentialManager) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tokenValidLocked(time.Now())
}

func (m *CredentialManager) load(ctx context.Context) error {
	creds, err := m.store.Load(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.creds = creds
	m.mu.Unlock()

	return nil
}

func (m *CredentialManager) refreshLoop(ctx context.Context, interval time.Duration) {
	m.logger.Info("credential refresh loop started",
		zap.Duration("check_interval", interval),
		zap.Duration("refresh_interval", m.refreshInterval),
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.refreshIfNeeded(context.Background(), "ticker"); err != nil {
				m.logger.Warn("periodic credential refresh failed, will retry on next interval", zap.Error(err))
			}
		case <-m.stopCh:
			m.logger.Info("credential refresh loop stopped")
			return
		case <-ctx.Done():
			m.logger.Info("credential refresh loop cancelled")
			return
		}
	}
}

// refreshIfNeeded uses double-check locking to avoid lock contention
func (m *CredentialManager) refreshIfNeeded(ctx context.Context, reason string) error {
	now := time.Now()

	m.mu.RLock()
	needs := m.needsRefreshLocked(now)
	m.mu.RUnlock()

	if !needs {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.needsRefreshLocked(time.Now()) {
		return nil
	}

	return m.refreshLocked(ctx, reason)
}

// needsRefreshLocked must be called with at least read lock held
func (m *CredentialManager) needsRefreshLocked(now time.Time) bool {
	if m.creds == nil || m.creds.AccessToken == "" {
		return true
	}
	if !m.creds.ExpiresAt.IsZero() {
		return m.creds.ExpiresAt.Before(now.Add(m.refreshInterval))
	}
	return true
}

// refreshLocked must be called with write lock held
func (m *CredentialManager) refreshLocked(ctx context.Context, reason string) error {
	if m.creds == nil || m.creds.RefreshToken == "" {
		return errors.New("refresh token is missing")
	}

	newCreds, err := m.refresher.Refresh(ctx, m.creds.RefreshToken)
	if err != nil {
		return err
	}

	if newCreds.AccessToken == "" {
		return errors.New("refresh returned empty access token")
	}

	m.creds = newCreds

	if err := m.store.Save(ctx, newCreds); err != nil {
		m.logger.Warn("failed to persist refreshed credentials", zap.Error(err))
	}

	m.logger.Info("credentials refreshed",
		zap.String("reason", reason),
		zap.String("access_token", maskToken(newCreds.AccessToken)),
		zap.String("refresh_token", maskToken(newCreds.RefreshToken)),
		zap.Time("expires_at", newCreds.ExpiresAt),
	)

	return nil
}

// tokenValidLocked assumes the caller holds at least a read lock.
func (m *CredentialManager) tokenValidLocked(now time.Time) bool {
	if m.creds == nil || m.creds.AccessToken == "" {
		return false
	}
	if m.creds.ExpiresAt.IsZero() {
		return true
	}
	return m.creds.ExpiresAt.After(now)
}
