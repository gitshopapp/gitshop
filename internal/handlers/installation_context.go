package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/services"
	"github.com/gitshopapp/gitshop/internal/session"
)

type AdminContextDecision string

const (
	AdminContextDecisionAllow         AdminContextDecision = "allow"
	AdminContextDecisionBadRequest    AdminContextDecision = "bad_request"
	AdminContextDecisionRedirect      AdminContextDecision = "redirect"
	AdminContextDecisionNotFound      AdminContextDecision = "not_found"
	AdminContextDecisionInternalError AdminContextDecision = "internal_error"
)

type AdminContextRequirements struct {
	Route                          string
	AllowAnonymous                 bool
	AllowInstallationQueryOverride bool
	RequireShop                    bool
	RequireOnboardingComplete      bool
	MissingShopRedirectURL         string
}

type AdminContextResult struct {
	Decision    AdminContextDecision
	Session     *session.Data
	Shop        *db.Shop
	RedirectURL string
	StatusCode  int
	Message     string
}

func (h *Handlers) ResolveAdminContext(ctx context.Context, r *http.Request, req AdminContextRequirements) AdminContextResult {
	result := AdminContextResult{Decision: AdminContextDecisionAllow}
	if req.RequireOnboardingComplete {
		req.RequireShop = true
	}
	if ctx == nil {
		ctx = r.Context()
	}

	sess := session.GetSessionFromContext(ctx)
	if sess == nil && h.sessionManager != nil {
		loaded, err := h.sessionManager.GetSession(ctx, r)
		if err == nil {
			sess = loaded
		}
	}
	if sess == nil {
		if req.AllowAnonymous {
			return result
		}
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: "/admin/login",
		}
	}
	result.Session = sess

	if req.AllowInstallationQueryOverride {
		rawInstallationID := strings.TrimSpace(r.URL.Query().Get("installation_id"))
		if rawInstallationID != "" {
			requestedInstallationID, err := parseInstallationID(rawInstallationID)
			if err != nil {
				return AdminContextResult{
					Decision:   AdminContextDecisionBadRequest,
					StatusCode: http.StatusBadRequest,
					Message:    "Invalid installation_id",
					Session:    sess,
				}
			}
			if requestedInstallationID != sess.InstallationID {
				return AdminContextResult{
					Decision:    AdminContextDecisionRedirect,
					RedirectURL: oauthLoginRedirectURL(requestedInstallationID),
					Session:     sess,
				}
			}
		}
	}

	if sess.InstallationID < 0 {
		h.loggerFromContext(ctx).Info("session has no installation context", "route", req.Route, "username", sess.GitHubUsername)
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: "/admin/no-installations",
			Session:     sess,
		}
	}

	if sess.InstallationID == 0 {
		h.loggerFromContext(ctx).Info("installation id in session is 0", "route", req.Route, "username", sess.GitHubUsername)
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: oauthLoginRedirectURL(0),
			Session:     sess,
		}
	}

	if !req.RequireShop {
		return result
	}

	missingShopRedirectURL := adminMissingShopRedirectURL(req)
	if sess.ShopID == uuid.Nil {
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: missingShopRedirectURL,
		}
	}

	if h.adminService == nil {
		return AdminContextResult{
			Decision:   AdminContextDecisionInternalError,
			StatusCode: http.StatusInternalServerError,
			Message:    "Admin service unavailable",
		}
	}

	shop, err := h.adminService.GetShopForInstallation(ctx, sess.InstallationID, sess.ShopID)
	if err != nil {
		if !errors.Is(err, services.ErrAdminShopNotFound) {
			h.loggerFromContext(ctx).Error("failed to load shop for admin context", "error", err, "route", req.Route, "shop_id", sess.ShopID, "installation_id", sess.InstallationID)
			return AdminContextResult{
				Decision:   AdminContextDecisionInternalError,
				StatusCode: http.StatusInternalServerError,
				Message:    "Failed to load shop",
			}
		}

		h.loggerFromContext(ctx).Warn("active shop is no longer available for installation", "error", err, "route", req.Route, "shop_id", sess.ShopID, "installation_id", sess.InstallationID)
		if clearErr := h.clearUnavailableShopFromSession(ctx, r, sess, req.Route); clearErr != nil {
			return AdminContextResult{
				Decision:   AdminContextDecisionInternalError,
				StatusCode: http.StatusInternalServerError,
				Message:    "Failed to update session",
			}
		}

		result.Session = sess
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: missingShopRedirectURL,
			Session:     sess,
		}
	}

	if !shop.IsConnected() {
		redirectURL, clearErr := h.clearStaleInstallationContext(ctx, r, sess, req.Route)
		if clearErr != nil {
			return AdminContextResult{
				Decision:   AdminContextDecisionInternalError,
				StatusCode: http.StatusInternalServerError,
				Message:    "Failed to update session",
			}
		}

		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: redirectURL,
			Session:     sess,
		}
	}

	result.Shop = shop
	if req.RequireOnboardingComplete {
		if !h.adminService.IsOnboarded(shop) {
			if !h.adminService.IsOnboardingComplete(ctx, shop) {
				return AdminContextResult{
					Decision:    AdminContextDecisionRedirect,
					RedirectURL: "/admin/setup",
					Session:     sess,
					Shop:        shop,
				}
			}

			if err := h.adminService.MarkOnboarded(ctx, shop); err != nil {
				h.loggerFromContext(ctx).Error("failed to mark shop as onboarded", "error", err, "route", req.Route, "shop_id", shop.ID)
				return AdminContextResult{
					Decision:   AdminContextDecisionInternalError,
					StatusCode: http.StatusInternalServerError,
					Message:    "Failed to update shop onboarding status",
				}
			}
			shop.OnboardedAt = time.Now().UTC()
		}
	}

	return result
}

func (h *Handlers) WriteAdminContextDecision(w http.ResponseWriter, r *http.Request, result AdminContextResult) bool {
	switch result.Decision {
	case AdminContextDecisionAllow:
		return false
	case AdminContextDecisionRedirect:
		target := strings.TrimSpace(result.RedirectURL)
		if target == "" {
			target = "/admin/login"
		}
		h.htmxRedirect(w, r, target)
		return true
	case AdminContextDecisionBadRequest:
		statusCode := result.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusBadRequest
		}
		message := result.Message
		if message == "" {
			message = "Bad request"
		}
		http.Error(w, message, statusCode)
		return true
	case AdminContextDecisionNotFound:
		statusCode := result.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusNotFound
		}
		message := result.Message
		if message == "" {
			message = "Not found"
		}
		http.Error(w, message, statusCode)
		return true
	case AdminContextDecisionInternalError:
		statusCode := result.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusInternalServerError
		}
		message := result.Message
		if message == "" {
			message = "Internal error"
		}
		http.Error(w, message, statusCode)
		return true
	default:
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return true
	}
}

func (h *Handlers) recoverStaleInstallationContext(ctx context.Context, w http.ResponseWriter, r *http.Request, sess *session.Data, route string) {
	redirectURL, err := h.clearStaleInstallationContext(ctx, r, sess, route)
	if err != nil {
		http.Error(w, "Failed to update session", http.StatusInternalServerError)
		return
	}
	h.htmxRedirect(w, r, redirectURL)
}

func (h *Handlers) clearUnavailableShopFromSession(ctx context.Context, r *http.Request, sess *session.Data, route string) error {
	if sess == nil {
		return fmt.Errorf("session is required")
	}
	if h.sessionManager == nil {
		return fmt.Errorf("session manager is required")
	}

	oldShopID := sess.ShopID
	sess.ShopID = uuid.Nil

	if err := h.sessionManager.UpdateSession(ctx, r, sess); err != nil {
		h.loggerFromContext(ctx).Error("failed to clear unavailable shop from session", "error", err, "route", route, "installation_id", sess.InstallationID, "old_shop_id", oldShopID)
		return err
	}

	return nil
}

func (h *Handlers) clearStaleInstallationContext(ctx context.Context, r *http.Request, sess *session.Data, route string) (string, error) {
	if sess == nil {
		return oauthLoginRedirectURL(0), nil
	}
	if h.sessionManager == nil {
		return "", fmt.Errorf("session manager is required")
	}

	oldInstallationID := sess.InstallationID
	oldShopID := sess.ShopID

	sess.InstallationID = 0
	sess.ShopID = uuid.Nil

	if err := h.sessionManager.UpdateSession(ctx, r, sess); err != nil {
		h.loggerFromContext(ctx).Error("failed to clear stale installation from session", "error", err, "route", route, "old_installation_id", oldInstallationID, "old_shop_id", oldShopID)
		return "", err
	}

	redirectURL := oauthLoginRedirectURL(oldInstallationID)
	h.loggerFromContext(ctx).Warn("stale installation in context, redirecting to auth", "route", route, "old_installation_id", oldInstallationID, "old_shop_id", oldShopID)
	return redirectURL, nil
}

func adminMissingShopRedirectURL(req AdminContextRequirements) string {
	redirectURL := strings.TrimSpace(req.MissingShopRedirectURL)
	if redirectURL == "" {
		return "/admin/setup"
	}
	return redirectURL
}

func (h *Handlers) handleNoConnectedShops(ctx context.Context, w http.ResponseWriter, r *http.Request, sess *session.Data, route string) bool {
	if sess == nil {
		h.htmxRedirect(w, r, "/admin/login")
		return true
	}

	totalShops, countErr := h.adminService.CountInstallationShops(ctx, sess.InstallationID)
	if countErr != nil {
		h.loggerFromContext(ctx).Error("failed to count installation shops", "error", countErr, "installation_id", sess.InstallationID)
		http.Error(w, "Failed to load shops", http.StatusInternalServerError)
		return true
	}
	if totalShops > 0 {
		// No connected shops but existing rows means installation context in session is stale.
		h.recoverStaleInstallationContext(ctx, w, r, sess, route)
		return true
	}

	return false
}
