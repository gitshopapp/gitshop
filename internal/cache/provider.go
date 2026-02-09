package cache

// Package cache provides caching functionality for webhook idempotency.

import (
	"context"
	"fmt"
	"time"
)

// Provider defines the interface for caching webhook event IDs
type Provider interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}

type Config struct {
	Provider              string
	RedisConnectionString string
}

func NewProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "memory", "":
		return NewMemoryProvider()
	case "redis":
		return NewRedisProvider(cfg.RedisConnectionString)
	default:
		return nil, fmt.Errorf("unsupported cache provider: %s", cfg.Provider)
	}
}

func WebhookKey(source, eventID string) string {
	return fmt.Sprintf("webhook:%s:%s", source, eventID)
}
