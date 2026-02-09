package cache

import (
	"context"
	"errors"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type MemoryProvider struct {
	cache *lru.Cache[string, item]
}

type item struct {
	value     string
	expiresAt time.Time
}

const defaultMemoryCacheSize = 10_000

func NewMemoryProvider() (*MemoryProvider, error) {
	c, err := lru.New[string, item](defaultMemoryCacheSize)
	if err != nil {
		return nil, err
	}
	return &MemoryProvider{cache: c}, nil
}

func (m *MemoryProvider) Get(ctx context.Context, key string) (string, error) {
	_ = ctx
	cached, exists := m.cache.Get(key)
	if !exists {
		return "", ErrNotFound
	}

	if time.Now().After(cached.expiresAt) {
		m.cache.Remove(key)
		return "", ErrNotFound
	}

	return cached.value, nil
}

func (m *MemoryProvider) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	_ = ctx
	m.cache.Add(key, item{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (m *MemoryProvider) Delete(ctx context.Context, key string) error {
	_ = ctx
	m.cache.Remove(key)
	return nil
}

func (m *MemoryProvider) Close() error {
	return nil
}

var ErrNotFound = errors.New("key not found")
