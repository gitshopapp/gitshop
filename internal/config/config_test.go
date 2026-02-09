package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestValidateEncryptionKeyLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		encryptionKey string
		wantErr       bool
	}{
		{
			name:          "valid 32-byte key",
			encryptionKey: strings.Repeat("k", 32),
			wantErr:       false,
		},
		{
			name:          "invalid short key",
			encryptionKey: "short",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.EncryptionKey = tt.encryptionKey

			err := cfg.validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestValidateSessionStoreProvider(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.SessionStoreProvider = "invalid"

	err := cfg.validate()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "SessionStoreProvider") || !strings.Contains(err.Error(), "oneof") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRedisAddrForSessionStore(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.SessionStoreProvider = "redis"
	cfg.RedisAddr = ""

	err := cfg.validate()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "RedisAddr") || !strings.Contains(err.Error(), "required_if") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGitHubOAuthCredentialsMustBePaired(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GitHubClientID = "client_id"
	cfg.GitHubClientSecret = ""

	err := cfg.validate()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBaseURLRequiredForOAuthOrStripeConnect(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GitHubClientID = "client_id"
	cfg.GitHubClientSecret = "client_secret"
	cfg.BaseURL = ""

	err := cfg.validate()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASE_URL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBaseURLRequiresHTTPSOutsideLocalhost(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.BaseURL = "http://example.com"

	err := cfg.validate()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASE_URL must use https") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBaseURLAllowsLocalhostHTTP(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.BaseURL = "http://localhost:8080"

	if err := cfg.validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func validConfig() *Config {
	return &Config{
		DatabaseURL:            "postgres://user:pass@localhost:5432/gitshop",
		GitHubAppID:            "12345",
		GitHubWebhookSecret:    "secret",
		GitHubPrivateKeyBase64: "base64pem",
		StripeWebhookSecret:    "whsec_123",
		CacheProvider:          "memory",
		SessionStoreProvider:   "memory",
		RedisAddr:              "localhost:6379",
		EncryptionKey:          strings.Repeat("k", 32),
		LogFormat:              "text",
	}
}

func TestLoadParsesUppercaseLogLevel(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/gitshop")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("GITHUB_PRIVATE_KEY_BASE64", "base64pem")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_123")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("k", 32))
	t.Setenv("LOG_LEVEL", "INFO")

	// Ensure unrelated env vars from host don't affect this test.
	t.Setenv("EMAIL_PROVIDER", "")
	t.Setenv("CACHE_PROVIDER", "")
	t.Setenv("SESSION_STORE_PROVIDER", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("expected INFO level, got %v", cfg.LogLevel)
	}
}
