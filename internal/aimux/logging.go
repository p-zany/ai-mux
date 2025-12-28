package aimux

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newZapLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "json"
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.Level = zap.NewAtomicLevel()
	if level == "" {
		level = "info"
	}
	if err := cfg.Level.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		return nil, err
	}
	return cfg.Build()
}

func NewLogger(level string) (*zap.Logger, error) {
	return newZapLogger(level)
}
