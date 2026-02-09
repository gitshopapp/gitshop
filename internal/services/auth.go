package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/internal/db"
)

var (
	ErrAuthUnavailable   = errors.New("auth service unavailable")
	ErrAuthInvalidCode   = errors.New("oauth code is required")
	ErrAuthCodeExchange  = errors.New("failed to exchange oauth code")
	ErrAuthGetGitHubUser = errors.New("failed to fetch github user")
	ErrAuthInstallations = errors.New("failed to fetch github installations")
	ErrAuthInvalidAppID  = errors.New("invalid github app id")
	ErrAuthGenerateState = errors.New("failed to generate oauth state")
)

type GitHubUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type GitHubInstallation struct {
	ID      int64 `json:"id"`
	AppID   int64 `json:"app_id"`
	Account struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

type StartGitHubLoginResult struct {
	State            string
	AuthorizationURL string
}

type CompleteGitHubOAuthInput struct {
	Code                     string
	PreferredInstallationIDs []int64
}

type CompleteGitHubOAuthResult struct {
	User            GitHubUser
	InstallationID  int64
	Shops           []*db.Shop
	ShopID          uuid.UUID
	ResolutionError error
}

type AuthService struct {
	shopStore   *db.ShopStore
	oauthConfig *oauth2.Config
	gitHubAppID int64
	httpClient  *http.Client
	logger      *slog.Logger
}

func NewAuthService(cfg *config.Config, shopStore *db.ShopStore, logger *slog.Logger) (*AuthService, error) {
	if cfg == nil {
		return nil, fmt.Errorf("auth service config is required")
	}
	if shopStore == nil {
		return nil, fmt.Errorf("auth service shop store is required")
	}

	gitHubAppID, err := strconv.ParseInt(cfg.GitHubAppID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAuthInvalidAppID, err)
	}

	return &AuthService{
		shopStore: shopStore,
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			Endpoint:     github.Endpoint,
			Scopes:       []string{"read:user", "user:email"},
			RedirectURL:  gitHubOAuthRedirectURL(cfg.BaseURL),
		},
		gitHubAppID: gitHubAppID,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		logger:      logger,
	}, nil
}

func gitHubOAuthRedirectURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}

	return strings.TrimRight(baseURL, "/") + "/auth/github/callback"
}

func (s *AuthService) StartGitHubLogin() (StartGitHubLoginResult, error) {
	result := StartGitHubLoginResult{}
	if s == nil || s.oauthConfig == nil {
		return result, ErrAuthUnavailable
	}

	state, err := generateOAuthState()
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrAuthGenerateState, err)
	}

	result.State = state
	result.AuthorizationURL = s.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOnline)

	return result, nil
}

func (s *AuthService) CompleteGitHubOAuth(ctx context.Context, input CompleteGitHubOAuthInput) (CompleteGitHubOAuthResult, error) {
	result := CompleteGitHubOAuthResult{}
	if s == nil || s.oauthConfig == nil || s.httpClient == nil || s.shopStore == nil {
		return result, ErrAuthUnavailable
	}

	code := strings.TrimSpace(input.Code)
	if code == "" {
		return result, ErrAuthInvalidCode
	}

	token, err := s.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrAuthCodeExchange, err)
	}

	user, err := s.getGitHubUser(ctx, token.AccessToken)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrAuthGetGitHubUser, err)
	}

	result.User = *user

	installationID, err := s.resolveAuthorizedInstallationID(ctx, token.AccessToken, input.PreferredInstallationIDs)
	if err != nil {
		result.ResolutionError = err
		return result, nil
	}

	result.InstallationID = installationID
	if installationID <= 0 {
		return result, nil
	}

	shops, err := s.shopStore.GetShopsByInstallationID(ctx, installationID)
	if err != nil {
		result.ResolutionError = fmt.Errorf("failed to get shops for installation %d: %w", installationID, err)
		result.Shops = []*db.Shop{}
		return result, nil
	}

	result.Shops = shops
	if len(shops) == 1 {
		result.ShopID = shops[0].ID
	}

	return result, nil
}

func (s *AuthService) getGitHubUser(ctx context.Context, accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && s.logger != nil {
			s.logger.Warn("failed to close github user response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("github API returned status %d (failed to read response body: %w)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *AuthService) getUserInstallations(ctx context.Context, accessToken string) ([]GitHubInstallation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/installations", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && s.logger != nil {
			s.logger.Warn("failed to close github installations response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("github API returned status %d (failed to read response body: %w)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Installations []GitHubInstallation `json:"installations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return payload.Installations, nil
}

func (s *AuthService) resolveAuthorizedInstallationID(ctx context.Context, accessToken string, preferredIDs []int64) (int64, error) {
	installations, err := s.getUserInstallations(ctx, accessToken)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrAuthInstallations, err)
	}

	return pickAuthorizedInstallationID(installations, s.gitHubAppID, preferredIDs), nil
}

func pickAuthorizedInstallationID(installations []GitHubInstallation, gitHubAppID int64, preferredIDs []int64) int64 {
	allowedInstallations := make(map[int64]struct{})
	firstAllowedInstallation := int64(0)

	for _, installation := range installations {
		if installation.AppID != gitHubAppID {
			continue
		}

		allowedInstallations[installation.ID] = struct{}{}
		if firstAllowedInstallation == 0 {
			firstAllowedInstallation = installation.ID
		}
	}

	for _, preferredID := range preferredIDs {
		if preferredID <= 0 {
			continue
		}
		if _, ok := allowedInstallations[preferredID]; ok {
			return preferredID
		}
	}

	return firstAllowedInstallation
}

func generateOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
