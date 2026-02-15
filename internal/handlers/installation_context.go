package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/observability"
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
	meter := observability.MeterFromContext(ctx)
	route := strings.TrimSpace(req.Route)
	if route == "" {
		route = "unknown"
	}
	meter.SetAttributes(
		attribute.String("component", "admin.context"),
		attribute.String("admin.route", route),
	)
	meter.Count("admin.context.evaluated", 1)
	recordDecision := func(decision AdminContextDecision, reason string) {
		attrs := []attribute.Builder{attribute.String("decision", string(decision))}
		if reason != "" {
			attrs = append(attrs, attribute.String("reason", reason))
		}
		meter.Count("admin.context.decision", 1, sentry.WithAttributes(attrs...))
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
			recordDecision(AdminContextDecisionAllow, "anonymous_allowed")
			return result
		}
		recordDecision(AdminContextDecisionRedirect, "missing_session")
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
				recordDecision(AdminContextDecisionBadRequest, "invalid_installation_id")
				return AdminContextResult{
					Decision:   AdminContextDecisionBadRequest,
					StatusCode: http.StatusBadRequest,
					Message:    "Invalid installation_id",
					Session:    sess,
				}
			}
			if requestedInstallationID != sess.InstallationID {
				recordDecision(AdminContextDecisionRedirect, "installation_override")
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
		recordDecision(AdminContextDecisionRedirect, "no_installation_context")
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: "/admin/no-installations",
			Session:     sess,
		}
	}

	if sess.InstallationID == 0 {
		h.loggerFromContext(ctx).Info("installation id in session is 0", "route", req.Route, "username", sess.GitHubUsername)
		recordDecision(AdminContextDecisionRedirect, "installation_id_zero")
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: oauthLoginRedirectURL(0),
			Session:     sess,
		}
	}

	if !req.RequireShop {
		recordDecision(AdminContextDecisionAllow, "shop_not_required")
		return result
	}

	missingShopRedirectURL := adminMissingShopRedirectURL(req)
	if sess.ShopID == uuid.Nil {
		recordDecision(AdminContextDecisionRedirect, "missing_shop")
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: missingShopRedirectURL,
		}
	}

	if h.adminService == nil {
		recordDecision(AdminContextDecisionInternalError, "admin_service_unavailable")
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
			recordDecision(AdminContextDecisionInternalError, "shop_lookup_failed")
			return AdminContextResult{
				Decision:   AdminContextDecisionInternalError,
				StatusCode: http.StatusInternalServerError,
				Message:    "Failed to load shop",
			}
		}

		h.loggerFromContext(ctx).Warn("active shop is no longer available for installation", "error", err, "route", req.Route, "shop_id", sess.ShopID, "installation_id", sess.InstallationID)
		if clearErr := h.clearUnavailableShopFromSession(ctx, r, sess, req.Route); clearErr != nil {
			recordDecision(AdminContextDecisionInternalError, "clear_unavailable_shop_failed")
			return AdminContextResult{
				Decision:   AdminContextDecisionInternalError,
				StatusCode: http.StatusInternalServerError,
				Message:    "Failed to update session",
			}
		}

		result.Session = sess
		recordDecision(AdminContextDecisionRedirect, "shop_not_found")
		return AdminContextResult{
			Decision:    AdminContextDecisionRedirect,
			RedirectURL: missingShopRedirectURL,
			Session:     sess,
		}
	}

	if !shop.IsConnected() {
		redirectURL, clearErr := h.clearStaleInstallationContext(ctx, r, sess, req.Route)
		if clearErr != nil {
			recordDecision(AdminContextDecisionInternalError, "clear_stale_installation_failed")
			return AdminContextResult{
				Decision:   AdminContextDecisionInternalError,
				StatusCode: http.StatusInternalServerError,
				Message:    "Failed to update session",
			}
		}

		recordDecision(AdminContextDecisionRedirect, "shop_disconnected")
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
				recordDecision(AdminContextDecisionRedirect, "onboarding_incomplete")
				return AdminContextResult{
					Decision:    AdminContextDecisionRedirect,
					RedirectURL: "/admin/setup",
					Session:     sess,
					Shop:        shop,
				}
			}

			if err := h.adminService.MarkOnboarded(ctx, shop); err != nil {
				h.loggerFromContext(ctx).Error("failed to mark shop as onboarded", "error", err, "route", req.Route, "shop_id", shop.ID)
				recordDecision(AdminContextDecisionInternalError, "mark_onboarded_failed")
				return AdminContextResult{
					Decision:   AdminContextDecisionInternalError,
					StatusCode: http.StatusInternalServerError,
					Message:    "Failed to update shop onboarding status",
				}
			}
			shop.OnboardedAt = time.Now().UTC()
		}
	}

	recordDecision(AdminContextDecisionAllow, "ok")
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
