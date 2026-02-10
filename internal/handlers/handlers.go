package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gitshopapp/gitshop/internal/cache"
	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/githubapp"
	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/services"
	"github.com/gitshopapp/gitshop/internal/session"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MB

// Handlers provides HTTP request handlers for the GitShop admin panel.
type Handlers struct {
	config               *config.Config
	db                   *pgxpool.Pool
	shopStore            *db.ShopStore
	orderStore           *db.OrderStore
	cacheProvider        cache.Provider
	githubAuth           *githubapp.Auth
	githubClient         *githubapp.Client
	githubRouter         *GitHubEventRouter
	stripeRouter         *StripeEventRouter
	authService          *services.AuthService
	stripeConnectService *services.StripeConnectService
	sessionManager       *session.Manager
	adminService         *services.AdminService
	logger               *slog.Logger
}

type Dependencies struct {
	Config               *config.Config
	DB                   *pgxpool.Pool
	ShopStore            *db.ShopStore
	OrderStore           *db.OrderStore
	CacheProvider        cache.Provider
	GitHubAuth           *githubapp.Auth
	GitHubClient         *githubapp.Client
	GitHubRouter         *GitHubEventRouter
	StripeRouter         *StripeEventRouter
	AuthService          *services.AuthService
	StripeConnectService *services.StripeConnectService
	SessionManager       *session.Manager
	AdminService         *services.AdminService
	Logger               *slog.Logger
}

func New(deps Dependencies) (*Handlers, error) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	if deps.Config == nil {
		return nil, fmt.Errorf("handlers dependencies: config is required")
	}
	if deps.DB == nil {
		return nil, fmt.Errorf("handlers dependencies: db is required")
	}
	if deps.ShopStore == nil {
		return nil, fmt.Errorf("handlers dependencies: shopStore is required")
	}
	if deps.OrderStore == nil {
		return nil, fmt.Errorf("handlers dependencies: orderStore is required")
	}
	if deps.CacheProvider == nil {
		return nil, fmt.Errorf("handlers dependencies: cacheProvider is required")
	}
	if deps.GitHubAuth == nil {
		return nil, fmt.Errorf("handlers dependencies: githubAuth is required")
	}
	if deps.GitHubClient == nil {
		return nil, fmt.Errorf("handlers dependencies: githubClient is required")
	}
	if deps.GitHubRouter == nil {
		return nil, fmt.Errorf("handlers dependencies: githubRouter is required")
	}
	if deps.StripeRouter == nil {
		return nil, fmt.Errorf("handlers dependencies: stripeRouter is required")
	}
	if deps.AuthService == nil {
		return nil, fmt.Errorf("handlers dependencies: authService is required")
	}
	if deps.SessionManager == nil {
		return nil, fmt.Errorf("handlers dependencies: sessionManager is required")
	}
	if deps.AdminService == nil {
		return nil, fmt.Errorf("handlers dependencies: adminService is required")
	}
	if deps.StripeConnectService == nil {
		return nil, fmt.Errorf("handlers dependencies: stripeConnectService is required")
	}

	return &Handlers{
		config:               deps.Config,
		db:                   deps.DB,
		shopStore:            deps.ShopStore,
		orderStore:           deps.OrderStore,
		cacheProvider:        deps.CacheProvider,
		githubAuth:           deps.GitHubAuth,
		githubClient:         deps.GitHubClient,
		githubRouter:         deps.GitHubRouter,
		stripeRouter:         deps.StripeRouter,
		authService:          deps.AuthService,
		stripeConnectService: deps.StripeConnectService,
		sessionManager:       deps.SessionManager,
		adminService:         deps.AdminService,
		logger:               logger.With("component", "handlers"),
	}, nil
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)

	// Test database connection
	if err := h.db.Ping(ctx); err != nil {
		logger.Error("database health check failed", "error", err)
		http.Error(w, "Database unhealthy", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	}); err != nil {
		logger.Error("failed to encode health response", "error", err)
	}
}

// SessionMiddleware adds session data to the request context
func (h *Handlers) SessionMiddleware(next http.Handler) http.Handler {
	return h.sessionManager.Middleware(next)
}

func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return h.sessionManager.RequireAuth("/admin/login")(next)
}

func (h *Handlers) Root(w http.ResponseWriter, r *http.Request) {
	session, err := h.sessionManager.GetSession(r.Context(), r)
	if err != nil || session == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if session.InstallationID == 0 {
		http.Redirect(w, r, "/auth/github/login", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

func (h *Handlers) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, h.logger)
}

func SecureCookiesFromConfig(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL != "" {
		if parsed, err := url.Parse(baseURL); err == nil {
			return strings.EqualFold(parsed.Scheme, "https")
		}
	}

	return cfg.Port == "443" || cfg.Port == "8443"
}

func secureCookiesFromConfig(cfg *config.Config) bool {
	return SecureCookiesFromConfig(cfg)
}
