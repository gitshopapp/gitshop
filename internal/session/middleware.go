// Package session provides HTTP middleware for session management.
package session

import (
	"context"
	"net/http"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const (
	// ctxKey is the key used to store session data in context
	ctxKey contextKey = "session"
)

// Middleware creates a middleware that adds session data to the request context
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.GetSession(r.Context(), r)
		if err == nil {
			// Add session to context
			ctx := context.WithValue(r.Context(), ctxKey, session)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth is a middleware that requires a valid session
func (m *Manager) RequireAuth(redirectURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, err := m.GetSession(r.Context(), r)
			if err != nil {
				http.Redirect(w, r, redirectURL, http.StatusSeeOther)
				return
			}

			// Add session to context
			ctx := context.WithValue(r.Context(), ctxKey, session)
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

// GetSessionFromContext retrieves session data from the request context.
func GetSessionFromContext(ctx context.Context) *Data {
	if ctx == nil {
		return nil
	}
	session, ok := ctx.Value(ctxKey).(*Data)
	if !ok {
		return nil
	}
	return session
}
