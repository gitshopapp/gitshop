package session

import (
	"context"
	"fmt"
)

type Config struct {
	Provider      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

func NewStore(ctx context.Context, cfg Config) (Store, error) {
	switch cfg.Provider {
	case "", "memory":
		return NewMemoryStore(), nil
	case "redis":
		return NewRedisStore(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	default:
		return nil, fmt.Errorf("unsupported session store provider: %s", cfg.Provider)
	}
}
