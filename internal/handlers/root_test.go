package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gitshopapp/gitshop/internal/config"
)

func TestRoot_RendersLandingPage(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{
			GitHubAppURL: "https://github.com/apps/gitshopapp",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.Root(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Turn any GitHub repository into a storefront.") {
		t.Fatalf("expected landing page headline")
	}
	if !strings.Contains(body, `href="https://github.com/apps/gitshopapp"`) {
		t.Fatalf("expected landing page to include install link")
	}
	if !strings.Contains(body, `href="/admin/login"`) {
		t.Fatalf("expected landing page to include sign-in link")
	}
}

func TestLanding_UsesDefaultInstallURLWhenConfigMissing(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.Landing(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `href="https://github.com/apps/gitshopapp"`) {
		t.Fatalf("expected landing page to include default install link")
	}
}
