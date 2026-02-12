package handlers

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/services"
	"github.com/gitshopapp/gitshop/ui/views"
)

// ShopSelection renders the shop selection page for users with multiple shops.
func (h *Handlers) ShopSelection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)

	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route: "admin.shops",
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	sess := contextResult.Session

	// Get all shops for this installation
	shops, err := h.adminService.GetInstallationShops(ctx, sess.InstallationID)
	if err != nil {
		logger.Error("failed to get shops", "error", err, "installation_id", sess.InstallationID)
		http.Error(w, "Failed to load shops", http.StatusInternalServerError)
		return
	}

	// If only one shop, select it and redirect appropriately
	if len(shops) == 1 {
		sess.ShopID = shops[0].ID
		if err := h.sessionManager.UpdateSession(ctx, r, sess); err != nil {
			logger.Error("failed to update session with shop selection", "error", err)
		}
		if h.adminService.IsOnboardingComplete(ctx, shops[0]) {
			http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
		return
	}

	// If no shops, redirect to setup
	if len(shops) == 0 {
		if h.handleNoConnectedShops(ctx, w, r, sess, "admin.shops") {
			return
		}
		http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
		return
	}

	serviceItems := h.adminService.BuildShopSelectionItems(ctx, shops)
	items := make([]views.ShopSelectionItem, 0, len(serviceItems))
	for _, item := range serviceItems {
		status := "Setup required"
		if item.Ready {
			status = "Ready"
		}
		items = append(items, views.ShopSelectionItem{
			ID:           item.ShopID.String(),
			RepoFullName: item.RepoFullName,
			Ready:        item.Ready,
			StatusLabel:  status,
		})
	}

	// Render shop selection page
	if err := views.ShopSelectionPage(items).Render(ctx, w); err != nil {
		logger.Error("failed to render shop selection page", "error", err)
	}
}

// SelectShop handles the shop selection form submission and updates the session.
func (h *Handlers) SelectShop(w http.ResponseWriter, r *http.Request) {
	logger := h.loggerFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	shopIDStr := r.FormValue("shop_id")
	if shopIDStr == "" {
		http.Error(w, "Shop ID required", http.StatusBadRequest)
		return
	}

	shopID, err := uuid.Parse(shopIDStr)
	if err != nil {
		http.Error(w, "Invalid shop ID", http.StatusBadRequest)
		return
	}

	// Get session and update it with the selected shop
	contextResult := h.ResolveAdminContext(r.Context(), r, AdminContextRequirements{
		Route: "admin.shops.select",
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	sess := contextResult.Session

	shop, err := h.adminService.GetShopForInstallation(r.Context(), sess.InstallationID, shopID)
	if err != nil {
		if errors.Is(err, services.ErrAdminShopNotFound) {
			http.Error(w, "Shop not found", http.StatusNotFound)
			return
		}
		logger.Error("failed to load selected shop", "error", err, "shop_id", shopID, "installation_id", sess.InstallationID)
		http.Error(w, "Failed to load shop", http.StatusInternalServerError)
		return
	}

	sess.ShopID = shopID
	if err := h.sessionManager.UpdateSession(r.Context(), r, sess); err != nil {
		logger.Error("failed to update session", "error", err)
		http.Error(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	logger.Info("shop selected", "shop_id", shopID, "username", sess.GitHubUsername)

	// Redirect to dashboard if setup is complete
	if h.adminService.IsOnboardingComplete(r.Context(), shop) {
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}

	// Otherwise send them to setup
	http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
}
