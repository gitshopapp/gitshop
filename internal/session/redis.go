package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisKeyPrefix = "session:"

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(ctx context.Context, addr, password string, db int) (*RedisStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to connect to redis: %w (and failed to close client: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Get(ctx context.Context, key string) (*Data, bool) {
	if r == nil || r.client == nil || key == "" || ctx == nil {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	val, err := r.client.Get(ctx, redisSessionKey(key)).Bytes()
	if errors.Is(err, redis.Nil) || err != nil {
		return nil, false
	}

	var data Data
	if err := json.Unmarshal(val, &data); err != nil {
		return nil, false
	}

	return &data, true
}

func (r *RedisStore) Set(ctx context.Context, key string, data *Data, ttl time.Duration) {
	if r == nil || r.client == nil || key == "" || data == nil || ctx == nil {
		return
	}

	val, err := json.Marshal(data)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := r.client.Set(ctx, redisSessionKey(key), val, ttl).Err(); err != nil {
		return
	}
}

func (r *RedisStore) Delete(ctx context.Context, key string) {
	if r == nil || r.client == nil || key == "" || ctx == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := r.client.Del(ctx, redisSessionKey(key)).Err(); err != nil {
		return
	}
}

func (r *RedisStore) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func redisSessionKey(id string) string {
	return redisKeyPrefix + id
}
