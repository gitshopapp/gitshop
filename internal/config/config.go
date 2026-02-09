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
	GitHubWebhookSecret    string `env:"GITHUB_WEBHOOK_SECRET,required" validate:"required"`
	GitHubPrivateKeyBase64 string `env:"GITHUB_PRIVATE_KEY_BASE64,required" validate:"required"`

	GitHubClientID     string `env:"GITHUB_CLIENT_ID"`
	GitHubClientSecret string `env:"GITHUB_CLIENT_SECRET"`

	StripePlatformSecretKey string `env:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret     string `env:"STRIPE_WEBHOOK_SECRET,required" validate:"required"`

	StripeConnectClientID string `env:"STRIPE_CONNECT_CLIENT_ID"`
	BaseURL               string `env:"BASE_URL" validate:"omitempty,url"`

	CacheProvider        string `env:"CACHE_PROVIDER" envDefault:"memory" validate:"omitempty,oneof=memory redis"`
	SessionStoreProvider string `env:"SESSION_STORE_PROVIDER" envDefault:"memory" validate:"omitempty,oneof=memory redis"`
	RedisAddr            string `env:"REDIS_ADDR" envDefault:"localhost:6379" validate:"required_if=CacheProvider redis,required_if=SessionStoreProvider redis"`
	RedisPassword        string `env:"REDIS_PASSWORD"`
	RedisDB              int    `env:"REDIS_DB" envDefault:"0"`

	EncryptionKey string `env:"ENCRYPTION_KEY,required" validate:"required,len=32"`

	EmailProvider string `env:"EMAIL_PROVIDER" validate:"omitempty,oneof=postmark mailgun resend"`
	EmailFrom     string `env:"EMAIL_FROM" validate:"required_if=EmailProvider mailgun,required_if=EmailProvider postmark,required_if=EmailProvider resend"`

	MailgunAPIKey  string `env:"MAILGUN_API_KEY" validate:"required_if=EmailProvider mailgun"`
	MailgunDomain  string `env:"MAILGUN_DOMAIN" validate:"required_if=EmailProvider mailgun"`
	MailgunBaseURL string `env:"MAILGUN_BASE_URL" envDefault:"https://api.mailgun.net/v3"`

	PostmarkAPIKey string `env:"POSTMARK_API_KEY" validate:"required_if=EmailProvider postmark"`
	ResendAPIKey   string `env:"RESEND_API_KEY" validate:"required_if=EmailProvider resend"`

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
