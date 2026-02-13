package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTermsOfUse_RendersPage(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/terms", nil)
	rec := httptest.NewRecorder()

	h.TermsOfUse(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Terms of Service") {
		t.Fatalf("expected page to include terms heading")
	}
	if !strings.Contains(body, `href="/privacy"`) {
		t.Fatalf("expected footer to include privacy policy link")
	}
}

func TestPrivacyPolicy_RendersPage(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	rec := httptest.NewRecorder()

	h.PrivacyPolicy(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Privacy Policy") {
		t.Fatalf("expected page to include privacy heading")
	}
	if !strings.Contains(body, `href="/terms"`) {
		t.Fatalf("expected footer to include terms of service link")
	}
}
