package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitshopapp/gitshop/internal/config"
)

func TestRequireSameOrigin_AllowsMatchingOrigin(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{BaseURL: "https://example.com"},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/admin/settings/email", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	h.RequireSameOrigin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestRequireSameOrigin_RejectsMissingOriginAndReferer(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{BaseURL: "https://example.com"},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/admin/settings/email", nil)
	rec := httptest.NewRecorder()

	h.RequireSameOrigin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestRequireSameOrigin_RejectsCrossOrigin(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{BaseURL: "https://example.com"},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/admin/settings/email", nil)
	req.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()

	h.RequireSameOrigin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestRequireSameOrigin_SkipsReadOnlyMethods(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{BaseURL: "https://example.com"},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/admin/dashboard", nil)
	rec := httptest.NewRecorder()

	h.RequireSameOrigin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}
