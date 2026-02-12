package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

type Config struct {
	DatabaseURL            string `env:"DATABASE_URL,required" validate:"required"`
	GitHubAppID            string `env:"GITHUB_APP_ID,required" validate:"required"`
	GitHubAppURL           string `env:"GITHUB_APP_URL" envDefault:"https://github.com/apps/gitshopapp" validate:"required,url"`
	GitHubWebhookSecret    string `env:"GITHUB_WEBHOOK_SECRET,required" validate:"required"`
	GitHubPrivateKeyBase64 string `env:"GITHUB_PRIVATE_KEY_BASE64,required" validate:"required"`

	GitHubClientID     string `env:"GITHUB_CLIENT_ID"`
	GitHubClientSecret string `env:"GITHUB_CLIENT_SECRET"`

	StripePlatformSecretKey string `env:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret     string `env:"STRIPE_WEBHOOK_SECRET,required" validate:"required"`

	StripeConnectClientID string `env:"STRIPE_CONNECT_CLIENT_ID"`
	BaseURL               string `env:"BASE_URL" validate:"omitempty,url"`

	CacheProvider         string `env:"CACHE_PROVIDER" envDefault:"memory" validate:"omitempty,oneof=memory redis"`
	SessionStoreProvider  string `env:"SESSION_STORE_PROVIDER" envDefault:"memory" validate:"omitempty,oneof=memory redis"`
	RedisConnectionString string `env:"REDIS_CONNECTION_STRING" envDefault:"redis://localhost:6379/0" validate:"required_if=CacheProvider redis,required_if=SessionStoreProvider redis"`

	EncryptionKey string `env:"ENCRYPTION_KEY,required" validate:"required,len=32"`

	LogLevel  slog.Level `env:"LOG_LEVEL" envDefault:"INFO"`
	LogFormat string     `env:"LOG_FORMAT" envDefault:"text" validate:"omitempty,oneof=text json"`
	Port      string     `env:"PORT" envDefault:"8080"`
}

var configValidator = validator.New()

func Load() (*Config, error) {
	var cfg Config

	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if err := configValidator.Struct(c); err != nil {
		return err
	}

	hasGitHubClientID := strings.TrimSpace(c.GitHubClientID) != ""
	hasGitHubClientSecret := strings.TrimSpace(c.GitHubClientSecret) != ""
	if hasGitHubClientID != hasGitHubClientSecret {
		return fmt.Errorf("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET must be set together")
	}

	baseURL := strings.TrimSpace(c.BaseURL)
	if (hasGitHubClientID || strings.TrimSpace(c.StripeConnectClientID) != "") && baseURL == "" {
		return fmt.Errorf("BASE_URL is required when OAuth or Stripe Connect is enabled")
	}

	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Hostname() == "" {
			return fmt.Errorf("BASE_URL must be a valid absolute URL")
		}
		if !isLocalHost(parsed.Hostname()) && !strings.EqualFold(parsed.Scheme, "https") {
			return fmt.Errorf("BASE_URL must use https outside local development")
		}
	}

	return nil
}

func isLocalHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
