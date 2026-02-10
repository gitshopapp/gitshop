package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitshopapp/gitshop/internal/session"
)

func TestParseInstallationID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		{name: "valid", value: "12345", want: 12345},
		{name: "empty", value: "", wantErr: true},
		{name: "zero", value: "0", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not numeric", value: "abc", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseInstallationID(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%q)", tc.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected installation id: got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestGitHubCallback_MissingStateCookie_InstallationStartRedirectsToLoginFlow(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?installation_id=12345", nil)
	rec := httptest.NewRecorder()

	h.GitHubCallback(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusSeeOther)
	}
	if location := resp.Header.Get("Location"); location != "/auth/github/login?installation_id=12345" {
		t.Fatalf("unexpected redirect location: got=%q", location)
	}
}

func TestGitHubCallback_MissingStateCookie_WithOAuthPayloadRedirectsToAdminLogin(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?installation_id=12345&code=test-code&state=test-state", nil)
	rec := httptest.NewRecorder()

	h.GitHubCallback(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusSeeOther)
	}
	if location := resp.Header.Get("Location"); location != "/admin/login" {
		t.Fatalf("unexpected redirect location: got=%q", location)
	}
}

func TestAdminLogin_AuthenticatedWithoutInstallationID_RedirectsToDashboard(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, 111)

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	h.AdminLogin(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusSeeOther)
	}
	if location := resp.Header.Get("Location"); location != "/admin/dashboard" {
		t.Fatalf("unexpected redirect location: got=%q", location)
	}
}

func TestAdminLogin_AuthenticatedWithInstallationID_RedirectsToGitHubLogin(t *testing.T) {
	t.Parallel()

	h, cookie := newAuthenticatedHandlerAndCookie(t, 111)

	req := httptest.NewRequest(http.MethodGet, "/admin/login?installation_id=222", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	h.AdminLogin(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusSeeOther)
	}
	if location := resp.Header.Get("Location"); location != "/auth/github/login?installation_id=222" {
		t.Fatalf("unexpected redirect location: got=%q", location)
	}
}

func TestAdminLogin_InvalidInstallationID_ReturnsBadRequest(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/login?installation_id=not-a-number", nil)
	rec := httptest.NewRecorder()

	h.AdminLogin(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: got=%d want=%d", resp.StatusCode, http.StatusBadRequest)
	}
}

func newAuthenticatedHandlerAndCookie(t *testing.T, installationID int64) (*Handlers, *http.Cookie) {
	t.Helper()

	sessionManager := session.NewManager(session.NewMemoryStore(), false)
	h := &Handlers{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionManager: sessionManager,
	}

	createRec := httptest.NewRecorder()
	_, err := sessionManager.CreateSession(context.Background(), createRec, &session.Data{
		UserID:         1,
		GitHubUsername: "octocat",
		InstallationID: installationID,
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	resp := createRec.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie to be set")
	}

	return h, cookies[0]
}
