// Package session provides in-memory session storage.
package session

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-memory session store
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*memoryEntry
}

type memoryEntry struct {
	data      *Data
	expiresAt time.Time
}

// NewMemoryStore creates a new in-memory session store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*memoryEntry),
	}
}

// Get retrieves a session by key
func (s *MemoryStore) Get(_ context.Context, key string) (*Data, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked(time.Now())

	entry, ok := s.sessions[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return cloneData(entry.data), true
}

// Set stores a session with the given TTL
func (s *MemoryStore) Set(_ context.Context, key string, data *Data, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())

	s.sessions[key] = &memoryEntry{
		data:      cloneData(data),
		expiresAt: time.Now().Add(ttl),
	}
}

// Delete removes a session
func (s *MemoryStore) Delete(_ context.Context, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, key)
}

func (s *MemoryStore) cleanupExpiredLocked(now time.Time) {
	for key, entry := range s.sessions {
		if now.After(entry.expiresAt) {
			delete(s.sessions, key)
		}
	}
}

func (s *MemoryStore) Close() error {
	return nil
}
