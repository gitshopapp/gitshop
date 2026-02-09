// Package session provides session management for authentication.
package session

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	cookieName = "gitshop_session"
	ttl        = 24 * time.Hour
)

// Data represents the data stored in a session
type Data struct {
	UserID         int64     `json:"user_id"`
	GitHubUsername string    `json:"github_username"`
	InstallationID int64     `json:"installation_id"`
	ShopID         uuid.UUID `json:"shop_id"`
	CreatedAt      int64     `json:"created_at"`
}

// Manager handles session creation, validation, and storage
type Manager struct {
	store  Store
	secure bool
}

// Store defines the interface for session storage
type Store interface {
	Get(ctx context.Context, key string) (*Data, bool)
	Set(ctx context.Context, key string, data *Data, ttl time.Duration)
	Delete(ctx context.Context, key string)
	Close() error
}

// NewManager creates a new session manager
func NewManager(store Store, secure bool) *Manager {
	return &Manager{
		store:  store,
		secure: secure,
	}
}

func (m *Manager) Close() error {
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.Close()
}

// CreateSession creates a new session and sets the cookie
func (m *Manager) CreateSession(ctx context.Context, w http.ResponseWriter, data *Data) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	if data == nil {
		return "", fmt.Errorf("session data is required")
	}

	sessionID := generateSessionID()

	sessionData := cloneData(data)
	sessionData.CreatedAt = time.Now().Unix()
	m.store.Set(ctx, sessionID, sessionData, ttl)

	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)

	return sessionID, nil
}

// GetSession retrieves the session data from the request
func (m *Manager) GetSession(ctx context.Context, r *http.Request) (*Data, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, fmt.Errorf("no session cookie found: %w", err)
	}

	if ctx == nil {
		ctx = r.Context()
	}

	data, ok := m.store.Get(ctx, cookie.Value)
	if !ok {
		return nil, fmt.Errorf("session not found or expired")
	}

	// Check if session is expired
	if time.Now().Unix()-data.CreatedAt > int64(ttl.Seconds()) {
		m.store.Delete(ctx, cookie.Value)
		return nil, fmt.Errorf("session expired")
	}

	return data, nil
}

// DestroySession removes the session and clears the cookie
func (m *Manager) DestroySession(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(cookieName)
	if ctx == nil {
		ctx = r.Context()
	}
	if err == nil {
		m.store.Delete(ctx, cookie.Value)
	}

	// Clear the cookie
	clearCookie := &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, clearCookie)

	return nil
}

// UpdateSession updates the existing session data without changing the session ID
// The session ID is obtained from the request cookie
func (m *Manager) UpdateSession(ctx context.Context, r *http.Request, data *Data) error {
	if data == nil {
		return fmt.Errorf("session data is required")
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return fmt.Errorf("no session cookie found: %w", err)
	}

	if ctx == nil {
		ctx = r.Context()
	}

	// Update the timestamp
	sessionData := cloneData(data)
	sessionData.CreatedAt = time.Now().Unix()

	// Update in store using the existing session ID
	m.store.Set(ctx, cookie.Value, sessionData, ttl)

	return nil
}

// generateSessionID generates a session ID.
func generateSessionID() string {
	return uuid.NewString()
}

func cloneData(data *Data) *Data {
	if data == nil {
		return nil
	}
	cloned := *data
	return &cloned
}
