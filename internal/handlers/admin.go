package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/services"
	"github.com/gitshopapp/gitshop/ui/views"
)

func (h *Handlers) renderError(w http.ResponseWriter, ctx context.Context, msg string) {
	if err := views.SettingsResult(msg, false).Render(ctx, w); err != nil {
		h.loggerFromContext(ctx).Error("failed to render error message", "error", err)
	}
}

func (h *Handlers) renderSuccess(w http.ResponseWriter, ctx context.Context, msg string) {
	if err := views.SettingsResult(msg, true).Render(ctx, w); err != nil {
		h.loggerFromContext(ctx).Error("failed to render success message", "error", err)
	}
}

func (h *Handlers) AdminSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                          "admin.setup",
		AllowInstallationQueryOverride: true,
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	sess := contextResult.Session

	if shopIDParam := r.URL.Query().Get("shop_id"); shopIDParam != "" {
		shopID, err := uuid.Parse(shopIDParam)
		if err != nil {
			http.Redirect(w, r, "/admin/shops", http.StatusSeeOther)
			return
		}

		_, err = h.adminService.GetShopForInstallation(ctx, sess.InstallationID, shopID)
		if err != nil {
			http.Redirect(w, r, "/admin/shops", http.StatusSeeOther)
			return
		}

		sess.ShopID = shopID
		if err := h.sessionManager.UpdateSession(ctx, r, sess); err != nil {
			logger.Error("failed to update session with shop selection", "error", err)
		}
	}

	if sess.ShopID == uuid.Nil {
		shops, err := h.adminService.GetInstallationShops(ctx, sess.InstallationID)
		if err != nil {
			logger.Error("failed to load installation shops for setup", "error", err, "installation_id", sess.InstallationID)
			http.Error(w, "Failed to load shops", http.StatusInternalServerError)
			return
		}
		switch len(shops) {
		case 0:
			if h.handleNoConnectedShops(ctx, w, r, sess, "admin.setup") {
				return
			}
			if err := views.NoShopsPage().Render(ctx, w); err != nil {
				logger.Error("failed to render no shops page", "error", err)
			}
			return
		case 1:
			sess.ShopID = shops[0].ID
			if err := h.sessionManager.UpdateSession(ctx, r, sess); err != nil {
				logger.Error("failed to update session", "error", err)
			}
		default:
			http.Redirect(w, r, "/admin/shops", http.StatusSeeOther)
			return
		}
	}

	shop, err := h.adminService.GetShopForInstallation(ctx, sess.InstallationID, sess.ShopID)
	if err != nil {
		logger.Warn("active shop is no longer available for installation", "error", err, "shop_id", sess.ShopID, "installation_id", sess.InstallationID)
		sess.ShopID = uuid.Nil
		if updateErr := h.sessionManager.UpdateSession(ctx, r, sess); updateErr != nil {
			logger.Error("failed to clear unavailable shop from session", "error", updateErr, "installation_id", sess.InstallationID)
			http.Error(w, "Failed to load shop", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/shops", http.StatusSeeOther)
		return
	}
	if !shop.IsConnected() {
		h.recoverStaleInstallationContext(ctx, w, r, sess, "admin.setup")
		return
	}
	if h.adminService.IsOnboarded(shop) {
		h.htmxRedirect(w, r, "/admin/dashboard")
		return
	}

	stripeReady := h.adminService.IsStripeReady(ctx, shop)
	needsStripe := !stripeReady
	needsEmail := !services.IsEmailConfigured(shop)

	ownerName := ""
	if parts := strings.Split(shop.GitHubRepoFullName, "/"); len(parts) > 0 {
		ownerName = parts[0]
	}

	repoCount := 1
	if shops, err := h.adminService.GetInstallationShops(ctx, sess.InstallationID); err == nil && len(shops) > 0 {
		repoCount = len(shops)
	}

	labelsStatus, yamlStatus, templateStatus, setupComplete := h.buildSetupStatus(ctx, shop, r.URL.Query(), stripeReady)

	if err := views.SetupPage(needsStripe, needsEmail, labelsStatus, yamlStatus, templateStatus, shop, ownerName, repoCount, setupComplete).Render(ctx, w); err != nil {
		logger.Error("failed to render setup page", "error", err)
	}
}

func isEmailConfigured(shop *db.Shop) bool {
	return services.IsEmailConfigured(shop)
}

func (h *Handlers) AdminSetupStripe(w http.ResponseWriter, r *http.Request) {
	h.StripeOnboardAccount(w, r)
}

func (h *Handlers) AdminSetupComplete(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/dashboard?toast=order_shipped", http.StatusSeeOther)
}

func (h *Handlers) AdminSetupLabels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                  "admin.setup.labels",
		RequireShop:            true,
		MissingShopRedirectURL: "/admin/setup",
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop

	if err := h.adminService.EnsureRepoLabels(ctx, shop); err != nil {
		http.Redirect(w, r, "/admin/setup?labels_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
}

func (h *Handlers) AdminSetupYAML(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                  "admin.setup.yaml",
		RequireShop:            true,
		MissingShopRedirectURL: "/admin/setup",
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop

	result, err := h.adminService.EnsureGitShopYAML(ctx, shop)
	if err != nil {
		http.Redirect(w, r, "/admin/setup?yaml_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if result != nil && result.Method == "pr" && result.URL != "" {
		http.Redirect(w, r, "/admin/setup?yaml_pr="+url.QueryEscape(result.URL), http.StatusSeeOther)
		return
	}
	if result != nil && result.URL != "" {
		http.Redirect(w, r, "/admin/setup?yaml_url="+url.QueryEscape(result.URL), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
}

func (h *Handlers) AdminSetupTemplate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                  "admin.setup.template",
		RequireShop:            true,
		MissingShopRedirectURL: "/admin/setup",
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop

	result, err := h.adminService.EnsureOrderTemplate(ctx, shop)
	if err != nil {
		http.Redirect(w, r, "/admin/setup?template_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if result != nil && result.Method == "pr" && result.URL != "" {
		http.Redirect(w, r, "/admin/setup?template_pr="+url.QueryEscape(result.URL), http.StatusSeeOther)
		return
	}
	if result != nil && result.URL != "" {
		http.Redirect(w, r, "/admin/setup?template_url="+url.QueryEscape(result.URL), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/setup", http.StatusSeeOther)
}

func (h *Handlers) AdminSyncTemplate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                  "admin.template.sync",
		RequireShop:            true,
		MissingShopRedirectURL: "/admin/setup",
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop

	prURL, err := h.adminService.SyncOrderTemplates(ctx, shop)
	if err != nil {
		http.Redirect(w, r, "/admin/dashboard?template_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if prURL != "" {
		http.Redirect(w, r, "/admin/dashboard?template_pr="+url.QueryEscape(prURL), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

func (h *Handlers) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                     "admin.dashboard",
		RequireShop:               true,
		RequireOnboardingComplete: true,
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop
	sess := contextResult.Session
	shopSwitcher := h.buildShopSwitcher(ctx, sess)

	var toastPayload *views.ToastPayload
	if r.URL.Query().Get("toast") == "order_shipped" {
		toastPayload = &views.ToastPayload{
			Title:       "Order marked as shipped",
			Description: "Customer tracking details were saved and sent.",
			Variant:     views.ToastVariantSuccess,
		}
	}

	if err := views.DashboardPage(shop, toastPayload, shopSwitcher).Render(ctx, w); err != nil {
		h.loggerFromContext(ctx).Error("failed to render dashboard page", "error", err)
	}
}

func (h *Handlers) AdminDashboardStorefront(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                     "admin.dashboard.storefront",
		RequireShop:               true,
		RequireOnboardingComplete: true,
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop

	repoStatus := h.buildRepoStatus(ctx, shop)
	if err := views.DashboardStorefrontSection(repoStatus).Render(ctx, w); err != nil {
		h.loggerFromContext(ctx).Error("failed to render dashboard storefront", "error", err)
	}
}

func (h *Handlers) AdminDashboardOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                     "admin.dashboard.orders",
		RequireShop:               true,
		RequireOnboardingComplete: true,
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop

	orders, err := h.adminService.GetRecentOrders(ctx, shop.ID, 20)
	if err != nil {
		h.loggerFromContext(ctx).Error("failed to get orders", "error", err, "shop_id", shop.ID)
		orders = []*db.Order{}
	}

	if err := views.DashboardOrdersSection(orders).Render(ctx, w); err != nil {
		h.loggerFromContext(ctx).Error("failed to render dashboard orders", "error", err)
	}
}

func (h *Handlers) htmxRedirect(w http.ResponseWriter, r *http.Request, url string) {
	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (h *Handlers) buildRepoStatus(ctx context.Context, shop *db.Shop) *views.RepoStatus {
	return repoStatusToView(h.adminService.BuildRepoStatus(ctx, shop))
}

func (h *Handlers) buildSetupStatus(ctx context.Context, shop *db.Shop, query url.Values, stripeReady bool) (*views.RepoLabelsStatus, *views.GitShopYAMLStatus, *views.OrderTemplateStatus, bool) {
	status := h.adminService.BuildSetupStatus(ctx, shop)
	labelsStatus := repoLabelsStatusToView(status.Labels)
	yamlStatus := yamlStatusToView(status.YAML)
	templateStatus := templateStatusToView(status.Template)

	if errMsg := query.Get("labels_error"); errMsg != "" && labelsStatus != nil {
		labelsStatus.ErrorMessage = errMsg
	}
	if errMsg := query.Get("yaml_error"); errMsg != "" && yamlStatus != nil {
		yamlStatus.ErrorMessage = errMsg
	}
	if prURL := query.Get("yaml_pr"); prURL != "" && yamlStatus != nil {
		yamlStatus.Method = "pr"
		yamlStatus.URL = prURL
	}
	if fileURL := query.Get("yaml_url"); fileURL != "" && yamlStatus != nil {
		yamlStatus.URL = fileURL
		if !yamlStatus.Exists {
			yamlStatus.Exists = true
		}
	}
	if errMsg := query.Get("template_error"); errMsg != "" && templateStatus != nil {
		templateStatus.ErrorMessage = errMsg
	}
	if prURL := query.Get("template_pr"); prURL != "" && templateStatus != nil {
		templateStatus.Method = "pr"
		templateStatus.URL = prURL
	}
	if fileURL := query.Get("template_url"); fileURL != "" && templateStatus != nil {
		templateStatus.URL = fileURL
		if !templateStatus.Exists {
			templateStatus.Exists = true
		}
	}

	setupComplete := stripeReady && isEmailConfigured(shop)
	setupComplete = setupComplete && labelsStatus != nil && labelsStatus.Ready
	setupComplete = setupComplete && yamlStatus != nil && yamlStatus.Valid
	setupComplete = setupComplete && templateStatus != nil && templateStatus.Valid

	return labelsStatus, yamlStatus, templateStatus, setupComplete
}

func repoStatusToView(status *services.RepoStatus) *views.RepoStatus {
	if status == nil {
		return nil
	}

	templateFiles := make([]views.TemplateFile, 0, len(status.TemplateFiles))
	for _, file := range status.TemplateFiles {
		templateFiles = append(templateFiles, views.TemplateFile{
			Name:  file.Name,
			URL:   file.URL,
			Valid: file.Valid,
		})
	}

	products := make([]views.ProductSummary, 0, len(status.Products))
	for _, product := range status.Products {
		products = append(products, views.ProductSummary{
			SKU:        product.SKU,
			Name:       product.Name,
			PriceCents: product.PriceCents,
			Active:     product.Active,
		})
	}

	return &views.RepoStatus{
		StripeReady:              status.StripeReady,
		EmailConfigured:          status.EmailConfigured,
		YAMLExists:               status.YAMLExists,
		YAMLValid:                status.YAMLValid,
		YAMLURL:                  status.YAMLURL,
		YAMLLastUpdatedLabel:     status.YAMLLastUpdatedLabel,
		TemplateExists:           status.TemplateExists,
		TemplateValid:            status.TemplateValid,
		TemplateURL:              status.TemplateURL,
		TemplateLastUpdatedLabel: status.TemplateLastUpdatedLabel,
		TemplateCount:            status.TemplateCount,
		TemplateFiles:            templateFiles,
		TemplateMissingSKUs:      status.TemplateMissingSKUs,
		TemplateExtraSKUs:        status.TemplateExtraSKUs,
		TemplatePriceMismatches:  status.TemplatePriceMismatches,
		TemplateOptionMismatches: status.TemplateOptionMismatches,
		TemplateSyncAvailable:    status.TemplateSyncAvailable,
		TemplateSyncMessage:      status.TemplateSyncMessage,
		Products:                 products,
	}
}

func repoLabelsStatusToView(status services.RepoLabelsStatus) *views.RepoLabelsStatus {
	return &views.RepoLabelsStatus{
		Ready:        status.Ready,
		Missing:      status.Missing,
		ErrorMessage: status.ErrorMessage,
	}
}

func yamlStatusToView(status services.GitShopYAMLStatus) *views.GitShopYAMLStatus {
	return &views.GitShopYAMLStatus{
		Exists:           status.Exists,
		Valid:            status.Valid,
		Method:           status.Method,
		URL:              status.URL,
		ErrorMessage:     status.ErrorMessage,
		LastUpdatedLabel: status.LastUpdatedLabel,
	}
}

func templateStatusToView(status services.OrderTemplateStatus) *views.OrderTemplateStatus {
	return &views.OrderTemplateStatus{
		Exists:           status.Exists,
		Valid:            status.Valid,
		Method:           status.Method,
		URL:              status.URL,
		ErrorMessage:     status.ErrorMessage,
		UnknownSKUs:      status.UnknownSKUs,
		PriceMismatches:  status.PriceMismatches,
		OptionMismatches: status.OptionMismatches,
		SyncAvailable:    status.SyncAvailable,
		SyncMessage:      status.SyncMessage,
		LastUpdatedLabel: status.LastUpdatedLabel,
		Count:            status.Count,
	}
}
func (h *Handlers) AdminSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                     "admin.settings",
		RequireShop:               true,
		RequireOnboardingComplete: true,
	})
	if h.WriteAdminContextDecision(w, r, contextResult) {
		return
	}
	shop := contextResult.Shop
	sess := contextResult.Session

	shopSwitcher := h.buildShopSwitcher(ctx, sess)
	if err := views.SettingsPage(shop, shopSwitcher).Render(ctx, w); err != nil {
		h.loggerFromContext(ctx).Error("failed to render settings page", "error", err)
	}
}

func (h *Handlers) AdminSettingsEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		h.renderError(w, ctx, "Failed to parse form")
		return
	}

	provider := r.FormValue("provider")

	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                  "admin.settings.email",
		RequireShop:            true,
		MissingShopRedirectURL: "/admin/setup",
	})
	if contextResult.Decision != AdminContextDecisionAllow {
		if contextResult.Decision == AdminContextDecisionInternalError {
			h.renderError(w, ctx, "Failed to load shop context")
			return
		}
		h.renderError(w, ctx, "Not authenticated")
		return
	}
	shopID := contextResult.Shop.ID

	apiKey := r.FormValue("api_key")
	from := r.FormValue("from_email")
	domain := r.FormValue("domain")

	if err := h.adminService.UpdateEmailSettings(ctx, shopID, provider, apiKey, from, domain); err != nil {
		var userErr services.UserError
		if errors.As(err, &userErr) {
			h.renderError(w, ctx, userErr.Message)
			return
		}
		h.loggerFromContext(ctx).Error("failed to update email config", "error", err, "shop_id", shopID)
		h.renderError(w, ctx, "Failed to save email settings")
		return
	}

	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		w.Header().Set("HX-Trigger", "email-settings-updated")
	}
	h.renderSuccess(w, ctx, "Email settings saved successfully!")
}

func (h *Handlers) AdminShipOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contextResult := h.ResolveAdminContext(ctx, r, AdminContextRequirements{
		Route:                  "admin.orders.ship",
		RequireShop:            true,
		MissingShopRedirectURL: "/admin/setup",
	})
	if contextResult.Decision != AdminContextDecisionAllow {
		if contextResult.Decision == AdminContextDecisionInternalError {
			http.Error(w, "Failed to load shop", http.StatusInternalServerError)
			return
		}
		if contextResult.Session == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Shop not found", http.StatusBadRequest)
		return
	}
	shopID := contextResult.Shop.ID

	vars := mux.Vars(r)
	orderIDStr := vars["id"]
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}

	if parseErr := r.ParseForm(); parseErr != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	err = h.adminService.ShipOrder(ctx, services.ShipOrderInput{
		ShopID:           shopID,
		OrderID:          orderID,
		TrackingNumber:   r.FormValue("tracking_number"),
		ShippingProvider: r.FormValue("shipping_provider"),
		Carrier:          r.FormValue("carrier"),
		OtherCarrier:     r.FormValue("carrier_other"),
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAdminInvalidShipmentInput):
			http.Error(w, "Tracking number and carrier are required", http.StatusBadRequest)
		case errors.Is(err, services.ErrAdminOrderNotFound):
			http.Error(w, "Order not found", http.StatusNotFound)
		case errors.Is(err, services.ErrAdminOrderStatusConflict):
			http.Error(w, "Only paid or shipped orders can be updated", http.StatusConflict)
		case errors.Is(err, services.ErrAdminShopNotFound):
			h.loggerFromContext(ctx).Error("failed to get shop while shipping order", "error", err, "shop_id", shopID, "order_id", orderID)
			http.Error(w, "Shop not found", http.StatusInternalServerError)
		default:
			h.loggerFromContext(ctx).Error("failed to ship order", "error", err, "order_id", orderID, "shop_id", shopID)
			http.Error(w, "Failed to update order", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}
