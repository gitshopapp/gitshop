package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gitshopapp/gitshop/internal/services"
	"github.com/gitshopapp/gitshop/internal/session"
	"github.com/gitshopapp/gitshop/ui/views"
)

// GitHubLogin redirects to GitHub OAuth authorization URL.
func (h *Handlers) GitHubLogin(w http.ResponseWriter, r *http.Request) {
	logger := h.loggerFromContext(r.Context())

	installationID := ""
	if rawInstallationID := strings.TrimSpace(r.URL.Query().Get("installation_id")); rawInstallationID != "" {
		parsedInstallationID, err := parseInstallationID(rawInstallationID)
		if err != nil {
			http.Error(w, "Invalid installation_id", http.StatusBadRequest)
			return
		}
		installationID = strconv.FormatInt(parsedInstallationID, 10)
	}

	loginResult, err := h.authService.StartGitHubLogin()
	if err != nil {
		logger.Error("failed to generate oauth state", "error", err)
		http.Error(w, "Failed to generate OAuth state", http.StatusInternalServerError)
		return
	}

	stateCookie := &http.Cookie{
		Name:     "oauth_state",
		Value:    loginResult.State,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   h.isSecure(),
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, stateCookie)

	if installationID != "" {
		installCookie := &http.Cookie{
			Name:     "installation_id",
			Value:    installationID,
			Path:     "/",
			MaxAge:   600, // 10 minutes
			HttpOnly: true,
			Secure:   h.isSecure(),
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, installCookie)
	}

	http.Redirect(w, r, loginResult.AuthorizationURL, http.StatusTemporaryRedirect)
}

// GitHubCallback handles the OAuth callback from GitHub.
// This handles both regular OAuth login and post-installation OAuth
// when "Request user authorization during installation" is enabled.
func (h *Handlers) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)

	var installationIDFromQuery int64
	if rawInstallationID := strings.TrimSpace(r.URL.Query().Get("installation_id")); rawInstallationID != "" {
		parsedInstallationID, err := parseInstallationID(rawInstallationID)
		if err != nil {
			logger.Warn("invalid installation_id in oauth callback", "value", rawInstallationID)
			http.Error(w, "Invalid installation_id", http.StatusBadRequest)
			return
		}
		installationIDFromQuery = parsedInstallationID
		logger.Info("received installation_id in OAuth callback", "installation_id", installationIDFromQuery)
	}

	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		state := strings.TrimSpace(r.URL.Query().Get("state"))
		code := strings.TrimSpace(r.URL.Query().Get("code"))

		if installationIDFromQuery > 0 && state == "" && code == "" {
			logger.Info("oauth callback from installation flow; restarting oauth", "installation_id", installationIDFromQuery)
			http.Redirect(w, r, fmt.Sprintf("/auth/github/login?installation_id=%d", installationIDFromQuery), http.StatusSeeOther)
			return
		}

		logger.Warn("oauth state cookie not found; returning to login", "error", err, "installation_id", installationIDFromQuery)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	clearStateCookie := &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.isSecure(),
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, clearStateCookie)

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" || state != stateCookie.Value {
		logger.Error("oauth state mismatch")
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		logger.Error("no code in oauth callback")
		http.Error(w, "No code provided", http.StatusBadRequest)
		return
	}

	preferredInstallationIDs := make([]int64, 0, 2)
	if installationIDFromQuery > 0 {
		preferredInstallationIDs = append(preferredInstallationIDs, installationIDFromQuery)
	}
	installCookie, installCookieErr := r.Cookie("installation_id")
	if installCookieErr == nil && strings.TrimSpace(installCookie.Value) != "" {
		cookieInstallationID, parseErr := parseInstallationID(installCookie.Value)
		if parseErr != nil {
			logger.Warn("invalid installation_id cookie", "error", parseErr)
		} else {
			preferredInstallationIDs = append(preferredInstallationIDs, cookieInstallationID)
		}
	}

	clearInstallCookie := &http.Cookie{
		Name:     "installation_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.isSecure(),
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, clearInstallCookie)

	oauthResult, err := h.authService.CompleteGitHubOAuth(ctx, services.CompleteGitHubOAuthInput{
		Code:                     code,
		PreferredInstallationIDs: preferredInstallationIDs,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAuthInvalidCode):
			http.Error(w, "No code provided", http.StatusBadRequest)
		case errors.Is(err, services.ErrAuthCodeExchange):
			logger.Error("failed to exchange oauth code", "error", err)
			http.Error(w, "Failed to authenticate", http.StatusInternalServerError)
		case errors.Is(err, services.ErrAuthGetGitHubUser):
			logger.Error("failed to get github user", "error", err)
			http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		default:
			logger.Error("failed to complete oauth callback", "error", err)
			http.Error(w, "Failed to authenticate", http.StatusInternalServerError)
		}
		return
	}

	if oauthResult.ResolutionError != nil {
		logger.Warn("failed to resolve authorized installation for session", "error", oauthResult.ResolutionError)
	}

	installationID := oauthResult.InstallationID
	shops := oauthResult.Shops
	shopID := oauthResult.ShopID

	if installationID > 0 {
		if len(shops) == 1 {
			logger.Info("found single shop for installation", "installation_id", installationID, "shop_id", shopID)
		} else if len(shops) > 1 {
			logger.Info("found multiple shops for installation", "installation_id", installationID, "count", len(shops))
		}
	}

	sessionData := &session.Data{
		UserID:         int64(oauthResult.User.ID),
		GitHubUsername: oauthResult.User.Login,
		InstallationID: installationID,
		ShopID:         shopID,
	}

	_, err = h.sessionManager.CreateSession(ctx, w, sessionData)
	if err != nil {
		logger.Error("failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	logger.Info("session created successfully", "username", oauthResult.User.Login, "installation_id", installationID, "shop_id", shopID)

	if installationID > 0 {
		switch len(shops) {
		case 0:
			http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
		case 1:
			if h.adminService.IsOnboardingComplete(ctx, shops[0]) {
				http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/admin/setup?shop_id=%s", shops[0].ID.String()), http.StatusSeeOther)
		default:
			http.Redirect(w, r, "/admin/shops", http.StatusSeeOther)
		}
		return
	}

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// Logout destroys the session and redirects to login.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.sessionManager.DestroySession(r.Context(), w, r); err != nil {
		h.loggerFromContext(r.Context()).Error("failed to destroy session", "error", err)
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// AdminLogin shows the login page.
func (h *Handlers) AdminLogin(w http.ResponseWriter, r *http.Request) {
	logger := h.loggerFromContext(r.Context())

	var requestedInstallationID int64
	if rawInstallationID := strings.TrimSpace(r.URL.Query().Get("installation_id")); rawInstallationID != "" {
		parsedInstallationID, err := parseInstallationID(rawInstallationID)
		if err != nil {
			http.Error(w, "Invalid installation_id", http.StatusBadRequest)
			return
		}
		requestedInstallationID = parsedInstallationID
	}

	_, err := h.sessionManager.GetSession(r.Context(), r)
	if err == nil {
		if requestedInstallationID > 0 {
			http.Redirect(w, r, fmt.Sprintf("/auth/github/login?installation_id=%d", requestedInstallationID), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}

	installationID := ""
	if requestedInstallationID > 0 {
		installationID = strconv.FormatInt(requestedInstallationID, 10)
	}
	if err := views.LoginPage(installationID).Render(r.Context(), w); err != nil {
		logger.Error("failed to render login page", "error", err)
	}
}

// isSecure returns true if we should use secure cookies.
func (h *Handlers) isSecure() bool {
	return secureCookiesFromConfig(h.config)
}

func parseInstallationID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("installation_id is empty")
	}

	installationID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || installationID <= 0 {
		return 0, fmt.Errorf("invalid installation_id: %s", value)
	}

	return installationID, nil
}
