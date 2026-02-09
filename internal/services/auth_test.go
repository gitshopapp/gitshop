package services

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"golang.org/x/oauth2"

	"github.com/gitshopapp/gitshop/internal/db"
)

func TestAuthService_StartGitHubLogin_Unavailable(t *testing.T) {
	t.Parallel()

	service := &AuthService{}
	_, err := service.StartGitHubLogin()
	if !errors.Is(err, ErrAuthUnavailable) {
		t.Fatalf("expected ErrAuthUnavailable, got %v", err)
	}
}

func TestAuthService_CompleteGitHubOAuth_InvalidCode(t *testing.T) {
	t.Parallel()

	service := &AuthService{
		shopStore:   &db.ShopStore{},
		oauthConfig: &oauth2.Config{},
		httpClient:  &http.Client{},
	}

	_, err := service.CompleteGitHubOAuth(context.Background(), CompleteGitHubOAuthInput{
		Code: "   ",
	})
	if !errors.Is(err, ErrAuthInvalidCode) {
		t.Fatalf("expected ErrAuthInvalidCode, got %v", err)
	}
}

func TestPickAuthorizedInstallationID_PrefersAllowedPreferredID(t *testing.T) {
	t.Parallel()

	installations := []GitHubInstallation{
		{ID: 10, AppID: 999},
		{ID: 20, AppID: 1000},
		{ID: 30, AppID: 1000},
	}

	got := pickAuthorizedInstallationID(installations, 1000, []int64{9999, 30, 20})
	if got != 30 {
		t.Fatalf("unexpected installation id: got=%d want=%d", got, 30)
	}
}

func TestPickAuthorizedInstallationID_FallsBackToFirstAllowed(t *testing.T) {
	t.Parallel()

	installations := []GitHubInstallation{
		{ID: 10, AppID: 1},
		{ID: 20, AppID: 1},
	}

	got := pickAuthorizedInstallationID(installations, 1, []int64{999, 0, -1})
	if got != 10 {
		t.Fatalf("unexpected installation id: got=%d want=%d", got, 10)
	}
}
