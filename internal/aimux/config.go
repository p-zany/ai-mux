package aimux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration parses from human-friendly strings (e.g., "60s") or numeric seconds.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	}
	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return err
	}
	d.Duration = time.Duration(seconds) * time.Second
	return nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var text string
	if err := value.Decode(&text); err == nil {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	}
	var seconds int64
	if err := value.Decode(&seconds); err == nil {
		d.Duration = time.Duration(seconds) * time.Second
		return nil
	}
	return errors.New("invalid duration format")
}

type User struct {
	Name  string `json:"name" yaml:"name"`
	Token string `json:"token" yaml:"token"`
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	CertPath string `json:"cert_path" yaml:"cert_path"`
	KeyPath  string `json:"key_path" yaml:"key_path"`
}

// Config包含CCM服务的全局配置。
// Provider特定的配置（如BaseURL、TokenEndpoint等）已硬编码为常量。
type Config struct {
	Listen               string    `json:"listen" yaml:"listen"`
	StateDir             string    `json:"state_dir" yaml:"state_dir"`
	Users                []User    `json:"users" yaml:"users"`
	LogLevel             string    `json:"log_level" yaml:"log_level"`
	RequestTimeout       Duration  `json:"request_timeout" yaml:"request_timeout"`
	RefreshCheckInterval Duration  `json:"refresh_check_interval" yaml:"refresh_check_interval"`
	TLS                  TLSConfig `json:"tls" yaml:"tls"`
	Providers            []string  `json:"providers" yaml:"providers"` // 支持的值: "claude", "chatgpt"

	// Testing-only fields (not serialized)
	TestClaudeBaseURL        string `json:"-" yaml:"-"`
	TestClaudeTokenEndpoint  string `json:"-" yaml:"-"`
	TestChatGPTBaseURL       string `json:"-" yaml:"-"`
	TestChatGPTTokenEndpoint string `json:"-" yaml:"-"`
	TestChatGPTRefreshToken  string `json:"-" yaml:"-"` // For tests that need to set initial refresh token
}

// CredentialPath returns the path to the Claude credentials file
func (c *Config) CredentialPath() string {
	return filepath.Join(c.StateDir, "claude", ".credentials.json")
}

// ChatGPTCredentialPath returns the path to the ChatGPT credentials file
func (c *Config) ChatGPTCredentialPath() string {
	return filepath.Join(c.StateDir, "chatgpt", "auth.json")
}

func DefaultConfig() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return Config{
		Listen:               ":8080",
		StateDir:             filepath.Join(home, ".aimux"),
		LogLevel:             "info",
		RequestTimeout:       Duration{Duration: 60 * time.Second},
		RefreshCheckInterval: Duration{Duration: defaultRefreshInterval},
		Providers:            []string{},
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config: %w", err)
		}
		format := detectFormat(path)
		if err := decodeConfig(format, data, &cfg); err != nil {
			return cfg, fmt.Errorf("decode config: %w", err)
		}
	}

	ensureDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen address cannot be empty")
	}

	if c.StateDir == "" {
		return errors.New("state_dir cannot be empty")
	}

	// Validate TLS configuration
	if c.TLS.Enabled {
		if c.TLS.CertPath == "" || c.TLS.KeyPath == "" {
			return errors.New("tls.cert_path and tls.key_path must both be set when TLS is enabled")
		}
		if _, err := os.Stat(c.TLS.CertPath); err != nil {
			return fmt.Errorf("tls.cert_path: %w", err)
		}
		if _, err := os.Stat(c.TLS.KeyPath); err != nil {
			return fmt.Errorf("tls.key_path: %w", err)
		}
	}

	if c.RefreshCheckInterval.Duration <= 0 {
		return errors.New("refresh_check_interval must be positive")
	}

	// Validate timeout
	if c.RequestTimeout.Duration <= 0 {
		return errors.New("request_timeout must be positive")
	}

	// Validate user tokens
	if len(c.Users) > 0 {
		seen := make(map[string]string, len(c.Users))
		for _, user := range c.Users {
			if user.Name == "" {
				return errors.New("user name cannot be empty")
			}
			if user.Token == "" {
				return fmt.Errorf("user %s: token cannot be empty", user.Name)
			}
			if len(user.Token) < 16 {
				return fmt.Errorf("user %s: token too short (minimum 16 characters)", user.Name)
			}
			if existingUser, exists := seen[user.Token]; exists {
				return fmt.Errorf("duplicate token for users %s and %s", existingUser, user.Name)
			}
			seen[user.Token] = user.Name
		}
	}

	// Validate providers
	if len(c.Providers) == 0 {
		return errors.New("at least one provider must be configured")
	}
	for _, providerName := range c.Providers {
		switch providerName {
		case "claude":
			// Claude requires credential file to exist
			if _, err := os.Stat(c.CredentialPath()); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("claude credential file %s not found", c.CredentialPath())
				}
				return fmt.Errorf("claude credential file: %w", err)
			}
			// Validate file is readable and has correct format
			store := NewClaudeStore(c.CredentialPath())
			if _, err := store.Load(nil); err != nil {
				return fmt.Errorf("claude credential file invalid: %w", err)
			}
		case "chatgpt":
			// ChatGPT requires credential file to exist
			if _, err := os.Stat(c.ChatGPTCredentialPath()); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("chatgpt credential file %s not found", c.ChatGPTCredentialPath())
				}
				return fmt.Errorf("chatgpt credential file: %w", err)
			}
			// Validate file is readable and has correct format
			store := NewChatGPTStore(c.ChatGPTCredentialPath())
			if _, err := store.Load(nil); err != nil {
				return fmt.Errorf("chatgpt credential file invalid: %w", err)
			}
		default:
			return fmt.Errorf("unknown provider: %s", providerName)
		}
	}

	return nil
}

func detectFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	default:
		return "yaml" // prefer YAML when ambiguous
	}
}

func decodeConfig(format string, data []byte, cfg *Config) error {
	switch format {
	case "json":
		return json.Unmarshal(data, cfg)
	case "yaml":
		return yaml.Unmarshal(data, cfg)
	default:
		return fmt.Errorf("unsupported config format: %s", format)
	}
}

func ensureDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = DefaultConfig().Listen
	}
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultConfig().StateDir
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = DefaultConfig().LogLevel
	}
	if cfg.RequestTimeout.Duration == 0 {
		cfg.RequestTimeout = DefaultConfig().RequestTimeout
	}
	if cfg.RefreshCheckInterval.Duration == 0 {
		cfg.RefreshCheckInterval = DefaultConfig().RefreshCheckInterval
	}
	if cfg.Providers == nil {
		cfg.Providers = []string{}
	}
}
