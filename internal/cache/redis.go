package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisKeyPrefix = "cache:"

type RedisProvider struct {
	client *redis.Client
}

func NewRedisProvider(connectionString string) (*RedisProvider, error) {
	opts, err := redis.ParseURL(connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis connection string: %w", err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close() //nolint
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisProvider{client: client}, nil
}

func (r *RedisProvider) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, redisCacheKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (r *RedisProvider) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return r.client.Set(ctx, redisCacheKey(key), value, ttl).Err()
}

func (r *RedisProvider) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, redisCacheKey(key)).Err()
}

func (r *RedisProvider) Close() error {
	return r.client.Close()
}

func redisCacheKey(key string) string {
	return redisKeyPrefix + key
}
