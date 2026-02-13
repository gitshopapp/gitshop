package githubapp

// Package githubapp provides GitHub App authentication and token management.

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	"github.com/gitshopapp/gitshop/internal/observability"
)

type tokenCacheEntry struct {
	token     *oauth2.Token
	expiresAt time.Time
}

type Auth struct {
	appID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client
	tokenCache map[int64]*tokenCacheEntry
	cacheMu    sync.RWMutex
}

func NewAuth(appIDStr, privateKeyBase64 string) (*Auth, error) {
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid GitHub App ID: %w", err)
	}

	keyData, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 private key: %w", err)
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return &Auth{
		appID:      appID,
		privateKey: privateKey,
		httpClient: observability.NewHTTPClient(10 * time.Second),
		tokenCache: make(map[int64]*tokenCacheEntry),
	}, nil
}

func (a *Auth) CreateJWT() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": a.appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(a.privateKey)
}

func (a *Auth) GetInstallationToken(ctx context.Context, installationID int64) (*oauth2.Token, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	// Check cache first
	a.cacheMu.RLock()
	if entry, exists := a.tokenCache[installationID]; exists {
		// Check if token is still valid (with 5 minute buffer)
		if time.Now().Before(entry.expiresAt.Add(-5 * time.Minute)) {
			a.cacheMu.RUnlock()
			return entry.token, nil
		}
	}
	a.cacheMu.RUnlock()

	// Create JWT for GitHub App authentication
	jwt, err := a.CreateJWT()
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT: %w", err)
	}

	// Make request to GitHub API to get installation token
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := a.httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response body: %w)", resp.StatusCode, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to close response body: %w)", resp.StatusCode, closeErr)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to decode response: %w (close error: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close response body: %w", err)
	}

	token := &oauth2.Token{
		AccessToken: result.Token,
		TokenType:   "token",
		Expiry:      result.ExpiresAt,
	}

	// Cache the token
	a.cacheMu.Lock()
	a.tokenCache[installationID] = &tokenCacheEntry{
		token:     token,
		expiresAt: result.ExpiresAt,
	}
	a.cacheMu.Unlock()

	return token, nil
}
