package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"golang.org/x/oauth2"

	"github.com/gitshopapp/gitshop/internal/db"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestGitHubOAuthRedirectURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "empty",
			baseURL: "",
			want:    "",
		},
		{
			name:    "without trailing slash",
			baseURL: "https://gitshop.example",
			want:    "https://gitshop.example/auth/github/callback",
		},
		{
			name:    "with trailing slash",
			baseURL: "https://gitshop.example/",
			want:    "https://gitshop.example/auth/github/callback",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := gitHubOAuthRedirectURL(tc.baseURL)
			if got != tc.want {
				t.Fatalf("unexpected redirect url: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestAuthService_getUserInstallations_Paginates(t *testing.T) {
	t.Parallel()

	requests := 0
	service := &AuthService{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++

				if req.URL.Path != "/user/installations" {
					t.Fatalf("unexpected path: %s", req.URL.Path)
				}
				if req.URL.Query().Get("per_page") != "100" {
					t.Fatalf("unexpected per_page: %s", req.URL.Query().Get("per_page"))
				}

				page := req.URL.Query().Get("page")
				payload := struct {
					Installations []GitHubInstallation `json:"installations"`
				}{}

				switch page {
				case "1":
					payload.Installations = make([]GitHubInstallation, 100)
					for i := range payload.Installations {
						payload.Installations[i] = GitHubInstallation{ID: int64(i + 1), AppID: 1}
					}
				case "2":
					payload.Installations = []GitHubInstallation{{ID: 101, AppID: 1}}
				default:
					t.Fatalf("unexpected page: %s", page)
				}

				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatalf("failed to marshal payload: %v", err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(body)),
					Request:    req,
				}, nil
			}),
		},
	}

	installations, err := service.getUserInstallations(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(installations) != 101 {
		t.Fatalf("unexpected installation count: got=%d want=%d", len(installations), 101)
	}
	if requests != 2 {
		t.Fatalf("unexpected request count: got=%d want=%d", requests, 2)
	}
}
