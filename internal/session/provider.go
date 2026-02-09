package session

import (
	"context"
	"fmt"
)

type Config struct {
	Provider              string
	RedisConnectionString string
}

func NewStore(ctx context.Context, cfg Config) (Store, error) {
	switch cfg.Provider {
	case "", "memory":
		return NewMemoryStore(), nil
	case "redis":
		return NewRedisStore(ctx, cfg.RedisConnectionString)
	default:
		return nil, fmt.Errorf("unsupported session store provider: %s", cfg.Provider)
	}
}
