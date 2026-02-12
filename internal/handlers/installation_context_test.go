package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitshopapp/gitshop/internal/session"
)

func TestResolveAdminContext_NoSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionManager: session.NewManager(session.NewMemoryStore(), false),
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{Route: "admin.setup"})

	if result.Decision != AdminContextDecisionRedirect {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionRedirect)
	}
	if result.RedirectURL != "/admin/login" {
		t.Fatalf("unexpected redirect URL: got=%q want=%q", result.RedirectURL, "/admin/login")
	}
}

func TestResolveAdminContext_NoSessionAllowedForAnonymous(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionManager: session.NewManager(session.NewMemoryStore(), false),
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{
		Route:          "admin.login",
		AllowAnonymous: true,
	})

	if result.Decision != AdminContextDecisionAllow {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionAllow)
	}
	if result.Session != nil {
		t.Fatalf("expected nil session for anonymous flow")
	}
}

func TestResolveAdminContext_InvalidInstallationQueryReturnsBadRequest(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, 111)

	req := httptest.NewRequest(http.MethodGet, "/admin/setup?installation_id=not-a-number", nil)
	req.AddCookie(cookie)

	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{
		Route:                          "admin.setup",
		AllowInstallationQueryOverride: true,
	})

	if result.Decision != AdminContextDecisionBadRequest {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionBadRequest)
	}
	if result.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got=%d want=%d", result.StatusCode, http.StatusBadRequest)
	}
}

func TestResolveAdminContext_InstallationQueryMismatchRedirectsToOAuth(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, 111)

	req := httptest.NewRequest(http.MethodGet, "/admin/setup?installation_id=222", nil)
	req.AddCookie(cookie)

	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{
		Route:                          "admin.setup",
		AllowInstallationQueryOverride: true,
	})

	if result.Decision != AdminContextDecisionRedirect {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionRedirect)
	}
	if result.RedirectURL != "/auth/github/login?installation_id=222" {
		t.Fatalf("unexpected redirect URL: got=%q want=%q", result.RedirectURL, "/auth/github/login?installation_id=222")
	}
}

func TestResolveAdminContext_MissingInstallationInSessionRedirectsToOAuth(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, 0)

	req := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	req.AddCookie(cookie)

	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{Route: "admin.setup"})

	if result.Decision != AdminContextDecisionRedirect {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionRedirect)
	}
	if result.RedirectURL != "/auth/github/login" {
		t.Fatalf("unexpected redirect URL: got=%q want=%q", result.RedirectURL, "/auth/github/login")
	}
}

func TestResolveAdminContext_NoInstallationsSessionRedirectsToNoInstallationsPage(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, noInstallationSessionInstallationID)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(cookie)

	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{Route: "admin.dashboard"})

	if result.Decision != AdminContextDecisionRedirect {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionRedirect)
	}
	if result.RedirectURL != "/admin/no-installations" {
		t.Fatalf("unexpected redirect URL: got=%q want=%q", result.RedirectURL, "/admin/no-installations")
	}
}

func TestResolveAdminContext_ValidInstallationAllowsRequest(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, 111)

	req := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	req.AddCookie(cookie)

	result := h.ResolveAdminContext(req.Context(), req, AdminContextRequirements{Route: "admin.setup"})

	if result.Decision != AdminContextDecisionAllow {
		t.Fatalf("unexpected decision: got=%q want=%q", result.Decision, AdminContextDecisionAllow)
	}
	if result.Session == nil {
		t.Fatal("expected resolved session")
	}
	if result.Session.InstallationID != 111 {
		t.Fatalf("unexpected installation id: got=%d want=%d", result.Session.InstallationID, 111)
	}
}

func TestWriteAdminContextDecision_RedirectsHTMXRequests(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	handled := h.WriteAdminContextDecision(rec, req, AdminContextResult{
		Decision:    AdminContextDecisionRedirect,
		RedirectURL: "/auth/github/login",
	})

	if !handled {
		t.Fatal("expected decision to be handled")
	}

	resp := rec.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusNoContent)
	}
	if got := resp.Header.Get("HX-Redirect"); got != "/auth/github/login" {
		t.Fatalf("unexpected HX-Redirect header: got=%q want=%q", got, "/auth/github/login")
	}
}
