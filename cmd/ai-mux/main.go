package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"ai-mux/internal/aimux"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "", "path to configuration file (json or yaml)")
	flag.Parse()

	// Create a basic logger for early errors
	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("init logger: %v", err))
	}
	defer logger.Sync()

	cfg, err := aimux.LoadConfig(*configPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}

	// Recreate logger with configured log level
	logger, err = aimux.NewLogger(cfg.LogLevel)
	if err != nil {
		logger.Fatal("init logger with config", zap.Error(err))
	}
	defer logger.Sync()

	logger.Info("configuration loaded",
		zap.String("listen", cfg.Listen),
		zap.String("state_dir", cfg.StateDir),
		zap.String("log_level", cfg.LogLevel),
		zap.Strings("providers", cfg.Providers),
		zap.Int("users", len(cfg.Users)),
	)

	service, err := aimux.NewService(cfg, logger)
	if err != nil {
		logger.Fatal("init service", zap.Error(err))
	}

	if err := service.Start(context.Background()); err != nil {
		logger.Fatal("start service", zap.Error(err))
	}

	server := &http.Server{
		Addr:    cfg.Listen,
		Handler: service,
	}

	startServer := func() error {
		if cfg.TLS.Enabled && cfg.TLS.CertPath != "" && cfg.TLS.KeyPath != "" {
			logger.Info("starting http server", zap.String("listen", cfg.Listen), zap.Bool("tls", true))
			return server.ListenAndServeTLS(cfg.TLS.CertPath, cfg.TLS.KeyPath)
		}
		logger.Info("starting http server", zap.String("listen", cfg.Listen), zap.Bool("tls", false))
		return server.ListenAndServe()
	}

	logger.Info("aimux proxy ready to accept connections")

	serverErr := make(chan error, 1)
	go func() {
		if err := startServer(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		logger.Fatal("server error", zap.Error(err))
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown error", zap.Error(err))
	}
}
