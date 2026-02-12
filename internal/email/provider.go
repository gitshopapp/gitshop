// Package email provides email provider interface.
package email

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gitshopapp/gitshop/internal/db"
)

type Provider interface {
	SendEmail(ctx context.Context, email *Email) error
	ValidateAPIKey(ctx context.Context) error
}

type Email struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Config struct {
	Provider string
	APIKey   string
	From     string
	Domain   string // For Mailgun
}

func NewProvider(config Config) (Provider, error) {
	switch config.Provider {
	case "postmark":
		return NewPostmarkProvider(config.APIKey, config.From), nil
	case "mailgun":
		return NewMailgunProvider(config.APIKey, config.Domain, config.From), nil
	case "resend":
		return NewResendProvider(config.APIKey, config.From), nil
	default:
		return nil, fmt.Errorf("EMAIL_PROVIDER must be either 'postmark', 'mailgun', or 'resend'")
	}
}

func NewProviderFromShop(shop *db.Shop) (Provider, error) {
	cfg, err := decodeShopEmailConfig(shop.EmailConfig)
	if err != nil {
		return nil, err
	}

	switch shop.EmailProvider {
	case "postmark":
		return NewPostmarkProvider(cfg.APIKey, cfg.FromEmail), nil
	case "mailgun":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://api.mailgun.net/v3"
		}
		return NewMailgunProviderWithBaseURL(cfg.APIKey, cfg.Domain, cfg.FromEmail, baseURL), nil
	case "resend":
		return NewResendProvider(cfg.APIKey, cfg.FromEmail), nil
	default:
		return nil, fmt.Errorf("shop email provider must be either 'postmark', 'mailgun', or 'resend'")
	}
}

type shopEmailConfig struct {
	APIKey    string `json:"api_key"`
	FromEmail string `json:"from_email"`
	Domain    string `json:"domain"`
	BaseURL   string `json:"base_url"`
}

func decodeShopEmailConfig(config map[string]any) (shopEmailConfig, error) {
	var decoded shopEmailConfig
	payload, err := json.Marshal(config)
	if err != nil {
		return decoded, fmt.Errorf("failed to encode shop email config: %w", err)
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return decoded, fmt.Errorf("failed to decode shop email config: %w", err)
	}

	return decoded, nil
}
