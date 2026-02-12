package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/internal/handlers"
	uiassets "github.com/gitshopapp/gitshop/ui/assets"
	"github.com/gitshopapp/gitshop/ui/views"
)

type Server struct {
	cfg        *config.Config
	logger     *slog.Logger
	handlers   *handlers.Handlers
	httpServer *http.Server
}

func New(cfg *config.Config, logger *slog.Logger, h *handlers.Handlers) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if h == nil {
		return nil, fmt.Errorf("handlers are required")
	}

	s := &Server{
		cfg:      cfg,
		logger:   logger,
		handlers: h,
	}

	router := s.buildRouter()
	s.httpServer = &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	return s, nil
}

func (s *Server) Run() error {
	s.logger.Info("server starting", "port", s.cfg.Port)

	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}

	s.logger.Info("server shutting down")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	s.logger.Info("server stopped")
	return nil
}

func (s *Server) buildRouter() *mux.Router {
	h := s.handlers

	r := mux.NewRouter()
	r.Use(h.RequestLogger)
	r.Use(h.SecurityHeaders)
	r.HandleFunc("/", h.Root).Methods("GET").Name("root")
	r.HandleFunc("/health", h.Health).Methods("GET").Name("health")
	r.HandleFunc("/webhooks/github", h.GitHubWebhook).Methods("POST").Name("webhooks.github")
	r.HandleFunc("/webhooks/stripe", h.StripeWebhook).Methods("POST").Name("webhooks.stripe")

	// 404 handler - must be last
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if err := views.NotFoundPage().Render(r.Context(), w); err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	})

	// Static assets - must be before admin router
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.FS(uiassets.FS)))).Name("assets")

	r.HandleFunc("/auth/github/login", h.GitHubLogin).Methods("GET").Name("auth.github.login")
	r.HandleFunc("/auth/github/callback", h.GitHubCallback).Methods("GET").Name("auth.github.callback")
	r.HandleFunc("/auth/logout", h.Logout).Methods("GET").Name("auth.logout")

	// Public admin routes
	r.HandleFunc("/admin/login", h.AdminLogin).Methods("GET").Name("admin.login")

	// Protected admin routes - require authentication
	adminRouter := r.PathPrefix("/admin").Subrouter()
	adminRouter.Use(h.SessionMiddleware)
	adminRouter.Use(h.RequireAuth)
	adminRouter.Use(h.RequireSameOrigin)
	adminRouter.HandleFunc("", h.AdminSetup).Methods("GET").Name("admin.root")
	adminRouter.HandleFunc("/setup", h.AdminSetup).Methods("GET").Name("admin.setup")
	adminRouter.HandleFunc("/setup/stripe", h.AdminSetupStripe).Methods("POST").Name("admin.setup.stripe")
	adminRouter.HandleFunc("/setup/labels", h.AdminSetupLabels).Methods("POST").Name("admin.setup.labels")
	adminRouter.HandleFunc("/setup/yaml", h.AdminSetupYAML).Methods("POST").Name("admin.setup.yaml")
	adminRouter.HandleFunc("/setup/template", h.AdminSetupTemplate).Methods("POST").Name("admin.setup.template")
	adminRouter.HandleFunc("/shops", h.ShopSelection).Methods("GET").Name("admin.shops")
	adminRouter.HandleFunc("/shops/select", h.SelectShop).Methods("POST").Name("admin.shops.select")
	adminRouter.HandleFunc("/dashboard", h.AdminDashboard).Methods("GET").Name("admin.dashboard")
	adminRouter.HandleFunc("/dashboard/storefront", h.AdminDashboardStorefront).Methods("GET").Name("admin.dashboard.storefront")
	adminRouter.HandleFunc("/dashboard/orders", h.AdminDashboardOrders).Methods("GET").Name("admin.dashboard.orders")
	adminRouter.HandleFunc("/settings", h.AdminSettings).Methods("GET").Name("admin.settings")
	adminRouter.HandleFunc("/settings/email", h.AdminSettingsEmail).Methods("POST").Name("admin.settings.email")
	adminRouter.HandleFunc("/orders/{id}/ship", h.AdminShipOrder).Methods("POST").Name("admin.orders.ship")
	adminRouter.HandleFunc("/template/sync", h.AdminSyncTemplate).Methods("POST").Name("admin.template.sync")
	adminRouter.HandleFunc("/no-installations", h.NoInstallation).Methods("GET").Name("admin.no_installations")

	// Stripe Connect Standard Account onboarding routes
	adminRouter.HandleFunc("/stripe/onboard", h.StripeOnboardAccount).Methods("POST").Name("admin.stripe.onboard")
	adminRouter.HandleFunc("/stripe/onboard/callback", h.StripeOnboardCallback).Methods("GET").Name("admin.stripe.onboard.callback")
	adminRouter.HandleFunc("/stripe/status", h.StripeConnectionStatus).Methods("GET").Name("admin.stripe.status")
	adminRouter.HandleFunc("/stripe/disconnect", h.StripeDisconnect).Methods("POST").Name("admin.stripe.disconnect")

	return r
}
