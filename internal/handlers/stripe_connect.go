package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/services"
)

// StripeOnboardAccount starts the Standard connected account onboarding flow.
func (h *Handlers) StripeOnboardAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess, err := h.sessionManager.GetSession(ctx, r)
	if err != nil || sess.ShopID == uuid.Nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	linkURL, err := h.stripeConnectService.StartOnboarding(ctx, sess.ShopID, h.config.BaseURL)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrStripeConnectUnavailable):
			h.loggerFromContext(ctx).Error("stripe connect service is unavailable", "error", err)
			http.Error(w, "Stripe integration unavailable", http.StatusServiceUnavailable)
		case errors.Is(err, services.ErrStripeConnectCreateAccount):
			h.loggerFromContext(ctx).Error("failed to create stripe account", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Failed to create Stripe account", http.StatusInternalServerError)
		case errors.Is(err, services.ErrStripeConnectCreateLink):
			h.loggerFromContext(ctx).Error("failed to create stripe onboarding link", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Failed to create onboarding link", http.StatusInternalServerError)
		case errors.Is(err, services.ErrStripeConnectShopNotFound):
			h.loggerFromContext(ctx).Error("failed to find shop for stripe onboarding", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		default:
			h.loggerFromContext(ctx).Error("failed to start stripe onboarding", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, linkURL, http.StatusSeeOther)
}

// StripeOnboardCallback handles the return from Stripe after account onboarding.
func (h *Handlers) StripeOnboardCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	errorCode := strings.TrimSpace(r.URL.Query().Get("error"))
	errorDesc := strings.TrimSpace(r.URL.Query().Get("error_description"))
	if errorCode != "" {
		h.loggerFromContext(ctx).Error("stripe onboarding callback returned error", "error", errorCode, "description", errorDesc)
		http.Redirect(w, r, "/admin/setup?error=stripe_onboarding_failed", http.StatusSeeOther)
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		h.loggerFromContext(ctx).Error("missing stripe onboarding state parameter")
		http.Redirect(w, r, "/admin/setup?error=missing_state", http.StatusSeeOther)
		return
	}

	result, err := h.stripeConnectService.CompleteOnboarding(ctx, state)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrStripeConnectUnavailable):
			h.loggerFromContext(ctx).Error("stripe connect service is unavailable", "error", err)
			http.Redirect(w, r, "/admin/setup?error=stripe_not_configured", http.StatusSeeOther)
		case errors.Is(err, services.ErrStripeConnectInvalidState):
			h.loggerFromContext(ctx).Error("invalid stripe onboarding state", "error", err)
			http.Redirect(w, r, "/admin/setup?error=invalid_state", http.StatusSeeOther)
		case errors.Is(err, services.ErrStripeConnectShopNotFound):
			h.loggerFromContext(ctx).Error("shop not found during stripe onboarding callback", "error", err)
			http.Redirect(w, r, "/admin/setup?error=shop_not_found", http.StatusSeeOther)
		case errors.Is(err, services.ErrStripeConnectGetAccount):
			h.loggerFromContext(ctx).Error("failed to get stripe account during onboarding callback", "error", err)
			http.Redirect(w, r, "/admin/setup?error=stripe_api_error", http.StatusSeeOther)
		default:
			h.loggerFromContext(ctx).Error("unexpected stripe onboarding callback failure", "error", err)
			http.Redirect(w, r, "/admin/setup?error=stripe_api_error", http.StatusSeeOther)
		}
		return
	}

	if result.Connected {
		http.Redirect(w, r, "/admin/setup?stripe=connected", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/setup?stripe=pending", http.StatusSeeOther)
}

// StripeConnectionStatusBadge returns the connection status badge as HTML.
func (h *Handlers) StripeConnectionStatusBadge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess, err := h.sessionManager.GetSession(ctx, r)
	if err != nil || sess.ShopID == uuid.Nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	status, err := h.stripeConnectService.GetConnectionStatus(ctx, sess.ShopID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrStripeConnectUnavailable):
			w.WriteHeader(http.StatusServiceUnavailable)
		case errors.Is(err, services.ErrStripeConnectShopNotFound):
			h.loggerFromContext(ctx).Error("failed to get shop for stripe badge", "error", err, "shop_id", sess.ShopID)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			h.loggerFromContext(ctx).Error("failed to build stripe badge status", "error", err, "shop_id", sess.ShopID)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	switch status.Status {
	case "not_connected":
		if _, err := fmt.Fprint(w, `<div class="p-4 rounded-md bg-amber-50 text-amber-800">No Stripe account connected</div>`); err != nil {
			h.loggerFromContext(ctx).Warn("failed to write stripe badge", "error", err)
		}
	case "error":
		h.loggerFromContext(ctx).Error("failed to verify stripe connection", "error", status.Error, "shop_id", sess.ShopID)
		if _, err := fmt.Fprint(w, `<div class="p-4 rounded-md bg-red-50 text-red-800">Failed to verify Stripe connection</div>`); err != nil {
			h.loggerFromContext(ctx).Warn("failed to write stripe badge", "error", err)
		}
	case "connected":
		if _, err := fmt.Fprint(w, `<div class="p-4 rounded-md bg-green-50 text-green-800">✓ Stripe connected and ready to accept payments</div>`); err != nil {
			h.loggerFromContext(ctx).Warn("failed to write stripe badge", "error", err)
		}
	case "pending_verification":
		if _, err := fmt.Fprint(w, `<div class="p-4 rounded-md bg-amber-50 text-amber-800">⏳ Stripe account pending verification</div>`); err != nil {
			h.loggerFromContext(ctx).Warn("failed to write stripe badge", "error", err)
		}
	default:
		if _, err := fmt.Fprint(w, `<div class="p-4 rounded-md bg-blue-50 text-blue-800">🔗 Stripe onboarding in progress</div>`); err != nil {
			h.loggerFromContext(ctx).Warn("failed to write stripe badge", "error", err)
		}
	}
}

// StripeConnectionStatus returns the connection status for a shop as JSON.
func (h *Handlers) StripeConnectionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess, err := h.sessionManager.GetSession(ctx, r)
	if err != nil || sess.ShopID == uuid.Nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	status, err := h.stripeConnectService.GetConnectionStatus(ctx, sess.ShopID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrStripeConnectUnavailable):
			http.Error(w, "Stripe integration unavailable", http.StatusServiceUnavailable)
		case errors.Is(err, services.ErrStripeConnectShopNotFound):
			h.loggerFromContext(ctx).Error("failed to get shop for stripe connection status", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		default:
			h.loggerFromContext(ctx).Error("failed to build stripe connection status", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
		return
	}

	response := map[string]any{
		"connected": status.Connected,
		"status":    status.Status,
	}
	if status.AccountID != "" {
		response["account_id"] = status.AccountID
		if status.Status == "error" {
			response["error"] = status.Error
		} else {
			response["details_submitted"] = status.DetailsSubmitted
			response["charges_enabled"] = status.ChargesEnabled
			response["payouts_enabled"] = status.PayoutsEnabled
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.loggerFromContext(ctx).Error("failed to encode stripe connection status response", "error", err)
	}
}

// StripeReconnectAccount creates a new account link for a shop that needs to complete onboarding.
func (h *Handlers) StripeReconnectAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess, err := h.sessionManager.GetSession(ctx, r)
	if err != nil || sess.ShopID == uuid.Nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	linkURL, err := h.stripeConnectService.ReconnectOnboarding(ctx, sess.ShopID, h.config.BaseURL)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrStripeConnectUnavailable):
			http.Error(w, "Stripe integration unavailable", http.StatusServiceUnavailable)
		case errors.Is(err, services.ErrStripeConnectNoAccount), errors.Is(err, services.ErrStripeConnectShopNotFound):
			http.Error(w, "No Stripe account found", http.StatusNotFound)
		case errors.Is(err, services.ErrStripeConnectCreateLink):
			h.loggerFromContext(ctx).Error("failed to create stripe reconnection link", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Failed to create reconnection link", http.StatusInternalServerError)
		default:
			h.loggerFromContext(ctx).Error("failed to reconnect stripe account", "error", err, "shop_id", sess.ShopID)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, linkURL, http.StatusSeeOther)
}

// StripeDisconnect disconnects the Stripe account from a shop.
func (h *Handlers) StripeDisconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess, err := h.sessionManager.GetSession(ctx, r)
	if err != nil || sess.ShopID == uuid.Nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if err := h.stripeConnectService.Disconnect(ctx, sess.ShopID); err != nil {
		if errors.Is(err, services.ErrStripeConnectShopNotFound) {
			h.loggerFromContext(ctx).Error("failed to get shop for stripe disconnect", "error", err, "shop_id", sess.ShopID)
			http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
			return
		}

		h.loggerFromContext(ctx).Error("failed to disconnect stripe account", "error", err, "shop_id", sess.ShopID)
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}
