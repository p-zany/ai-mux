package aimux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Service struct {
	cfg      Config
	auth     *Authenticator
	client   *http.Client
	logger   *zap.Logger
	registry *providerRegistry

	startOnce sync.Once
	startErr  error
	creds     []CredentialSource
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

const maxLoggedErrorBodyBytes = 4096

func (lrw *loggingResponseWriter) WriteHeader(status int) {
	lrw.status = status
	lrw.ResponseWriter.WriteHeader(status)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += int64(n)
	return n, err
}

func (lrw *loggingResponseWriter) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func NewService(cfg Config, logger *zap.Logger) (*Service, error) {
	if logger == nil {
		var err error
		logger, err = newZapLogger(cfg.LogLevel)
		if err != nil {
			return nil, fmt.Errorf("init logger: %w", err)
		}
	}

	client := &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: cfg.RequestTimeout.Duration,
		},
	}

	var creds []CredentialSource
	var registrations []providerRegistration

	for _, providerName := range cfg.Providers {
		switch providerName {
		case "claude":
			logger.Info("initializing claude provider",
				zap.String("credential_path", cfg.CredentialPath()),
			)

			tokenEndpoint := claudeTokenEndpoint
			if cfg.TestClaudeTokenEndpoint != "" {
				tokenEndpoint = cfg.TestClaudeTokenEndpoint
			}

			claudeCreds, err := NewClaudeCredentials(
				cfg.CredentialPath(),
				tokenEndpoint,
				cfg.RefreshCheckInterval.Duration,
				client,
				logger.Named("claude_credentials"),
			)
			if err != nil {
				return nil, fmt.Errorf("load claude credentials: %w", err)
			}

			var claudeOpts *ClaudeProviderOptions
			if cfg.TestClaudeBaseURL != "" {
				claudeOpts = &ClaudeProviderOptions{
					BaseURL:       cfg.TestClaudeBaseURL,
					TokenEndpoint: tokenEndpoint,
				}
			}

			claudeProvider, err := NewClaudeProvider(claudeCreds, claudeOpts)
			if err != nil {
				return nil, fmt.Errorf("init claude provider: %w", err)
			}

			creds = append(creds, claudeCreds)
			registrations = append(registrations, providerRegistration{
				prefix:   claudePrefix,
				provider: claudeProvider,
			})
			logger.Info("claude provider initialized successfully")

		case "chatgpt":
			logger.Info("initializing chatgpt provider",
				zap.String("credential_path", cfg.ChatGPTCredentialPath()),
			)

			tokenEndpoint := chatGPTTokenEndpoint
			if cfg.TestChatGPTTokenEndpoint != "" {
				tokenEndpoint = cfg.TestChatGPTTokenEndpoint
			}

			refreshToken := ""
			if cfg.TestChatGPTRefreshToken != "" {
				refreshToken = cfg.TestChatGPTRefreshToken
			}

			chatgptSource, err := NewChatGPTCredentials(
				cfg.ChatGPTCredentialPath(),
				tokenEndpoint,
				chatGPTClientID,
				chatGPTScope,
				refreshToken,
				cfg.RefreshCheckInterval.Duration,
				cfg.RefreshCheckInterval.Duration,
				client,
				logger.Named("chatgpt_credentials"),
			)
			if err != nil {
				return nil, fmt.Errorf("init chatgpt credentials: %w", err)
			}

			var chatgptOpts *ChatGPTProviderOptions
			if cfg.TestChatGPTBaseURL != "" {
				chatgptOpts = &ChatGPTProviderOptions{
					BaseURL:       cfg.TestChatGPTBaseURL,
					TokenEndpoint: tokenEndpoint,
				}
			}

			chatgptProvider, err := NewChatGPTProvider(chatgptSource, chatgptOpts)
			if err != nil {
				return nil, fmt.Errorf("init chatgpt provider: %w", err)
			}

			creds = append(creds, chatgptSource)
			registrations = append(registrations, providerRegistration{
				prefix:   chatGPTPrefix,
				provider: chatgptProvider,
			})
			logger.Info("chatgpt provider initialized successfully")

		default:
			return nil, fmt.Errorf("unknown provider: %s", providerName)
		}
	}

	registry, err := newProviderRegistry(registrations)
	if err != nil {
		return nil, fmt.Errorf("provider registry: %w", err)
	}

	return &Service{
		cfg:      cfg,
		auth:     NewAuthenticator(cfg.Users),
		client:   client,
		logger:   logger,
		registry: registry,
		creds:    creds,
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		s.logger.Info("starting credential sources", zap.Int("count", len(s.creds)))
		for _, cred := range s.creds {
			if err := cred.Start(ctx); err != nil {
				s.startErr = err
				return
			}
		}
		if s.startErr == nil {
			s.logger.Info("all credential sources started successfully")
		}
	})
	return s.startErr
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	lrw := &loggingResponseWriter{ResponseWriter: w}
	userLabel := "anonymous"
	providerID := "-"
	upstreamHost := "-"

	if err := s.Start(context.Background()); err != nil {
		s.logger.Error("service start failed", zap.Error(err))
		http.Error(lrw, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	defer func() {
		status := lrw.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start).Round(time.Millisecond)
		s.logger.Info("request",
			zap.String("remote", r.RemoteAddr),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("user", userLabel),
			zap.String("provider", providerID),
			zap.Int("status", status),
			zap.Int64("bytes", lrw.bytes),
			zap.Duration("duration", duration),
			zap.String("upstream_host", upstreamHost),
		)
	}()

	provider, trimmed, ok := s.registry.Resolve(r.URL.Path)
	if !ok {
		s.logger.Warn("unknown provider prefix", zap.String("path", r.URL.Path))
		http.NotFound(lrw, r)
		return
	}
	providerID = provider.ID()

	if !provider.IsAvailable() {
		s.logger.Warn("provider not available",
			zap.String("provider", providerID),
			zap.String("path", r.URL.Path))
		http.Error(lrw, fmt.Sprintf("provider %s is not available: credentials not ready", providerID), http.StatusServiceUnavailable)
		return
	}

	username, ok := s.authenticate(r)
	if !ok {
		s.logger.Warn("authentication failed", zap.String("remote", r.RemoteAddr))
		http.Error(lrw, "unauthorized", http.StatusUnauthorized)
		return
	}
	if username != "" {
		userLabel = username
	}

	s.logger.Debug("headers inbound", zap.Any("headers", sanitizeHeaders(r.Header)))

	upstreamReq, err := provider.BuildUpstreamRequest(r.Context(), r, trimmed)
	if err != nil {
		s.logger.Error("build upstream request", zap.Error(err))
		http.Error(lrw, "bad request", http.StatusBadRequest)
		return
	}
	upstreamHost = upstreamReq.URL.Host
	s.logger.Debug("headers upstream", zap.Any("headers", sanitizeHeaders(upstreamReq.Header)))

	resp, err := s.client.Do(upstreamReq)
	if err != nil {
		s.logger.Error("upstream request", zap.Error(err), zap.String("host", upstreamReq.URL.Host))
		http.Error(lrw, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		if isHopByHop(key) {
			continue
		}
		lrw.Header()[key] = values
	}
	lrw.WriteHeader(resp.StatusCode)

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if strings.EqualFold(mediaType, "text/event-stream") {
		s.streamResponse(lrw, resp)
		return
	}

	logErrorBody := resp.StatusCode >= http.StatusBadRequest
	var bodyTee *limitedBuffer
	copyWriter := io.Writer(lrw)
	if logErrorBody {
		bodyTee = &limitedBuffer{limit: maxLoggedErrorBodyBytes}
		copyWriter = io.MultiWriter(lrw, bodyTee)
	}

	if _, err := io.Copy(copyWriter, resp.Body); err != nil {
		s.logger.Warn("copy response", zap.Error(err))
	}

	if logErrorBody && bodyTee != nil && bodyTee.Len() > 0 {
		body := strings.TrimSpace(bodyTee.String())
		if bodyTee.Truncated {
			body += " ... (truncated)"
		}
		s.logger.Warn("upstream error response",
			zap.String("provider", providerID),
			zap.String("path", r.URL.Path),
			zap.String("upstream_host", upstreamHost),
			zap.Int("status", resp.StatusCode),
			zap.Any("headers", sanitizeHeaders(resp.Header)),
			zap.String("message", body),
		)
	}
}

func (s *Service) authenticate(r *http.Request) (string, bool) {
	// If no users configured, allow all requests (no authentication required)
	if !s.auth.HasUsers() {
		return "", true
	}

	authHeader := r.Header.Get("Authorization")

	// If no Authorization header provided, allow the request (anonymous access)
	if authHeader == "" {
		return "", true
	}

	// If Authorization header is provided, validate it
	prefix := "bearer "
	if len(authHeader) < len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		s.logger.Warn("authentication failed: invalid authorization format", zap.String("remote", r.RemoteAddr))
		return "", false
	}

	token := strings.TrimSpace(authHeader[len(prefix):])
	if token == "" {
		s.logger.Warn("authentication failed: empty token", zap.String("remote", r.RemoteAddr))
		return "", false
	}

	// Only reject if token is provided but not in user list
	username, ok := s.auth.Authenticate(token)
	if !ok {
		s.logger.Warn("authentication failed: unknown token", zap.String("remote", r.RemoteAddr))
		return "", false
	}
	return username, true
}

func (s *Service) streamResponse(w http.ResponseWriter, resp *http.Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Warn("streaming not supported")
		return
	}

	buffer := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				s.logger.Warn("write streaming response", zap.Error(writeErr))
				return
			}
			flusher.Flush()
		}
		if err != nil {
			return
		}
	}
}

func isHopByHop(header string) bool {
	h := strings.ToLower(header)
	if strings.HasPrefix(h, "proxy-") {
		return true
	}
	switch h {
	case "connection", "keep-alive", "te", "trailers", "transfer-encoding", "upgrade", "host":
		return true
	default:
		return false
	}
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHop(key) {
			continue
		}
		if strings.EqualFold(key, "Authorization") {
			continue
		}
		dst[key] = append([]string(nil), values...)
	}
}

func sanitizeHeaders(src http.Header) http.Header {
	dst := cloneHeaders(src)
	maskHeader(dst, "Authorization")
	maskHeader(dst, "Proxy-Authorization")
	maskHeader(dst, "OpenAI-Organization")
	maskHeader(dst, "ChatGPT-Account-Id")
	return dst
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	Truncated bool
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	if lb.limit <= 0 {
		return len(p), nil
	}
	remain := lb.limit - lb.buf.Len()
	if remain > 0 {
		if len(p) <= remain {
			_, _ = lb.buf.Write(p)
		} else {
			_, _ = lb.buf.Write(p[:remain])
			lb.Truncated = true
		}
	} else {
		lb.Truncated = true
	}
	return len(p), nil
}

func (lb *limitedBuffer) Len() int {
	return lb.buf.Len()
}

func (lb *limitedBuffer) String() string {
	return lb.buf.String()
}

func maskHeader(headers http.Header, key string) {
	if val := headers.Get(key); val != "" {
		headers.Set(key, maskToken(val))
	}
}

func cloneHeaders(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for k, vals := range src {
		dst[k] = append([]string(nil), vals...)
	}
	return dst
}

func (s *Service) Shutdown(ctx context.Context) error {
	var firstErr error
	for _, provider := range s.registry.providers() {
		if err := provider.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
