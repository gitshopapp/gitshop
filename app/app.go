package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gitshopapp/gitshop/internal/cache"
	"github.com/gitshopapp/gitshop/internal/catalog"
	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/internal/crypto"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/email"
	"github.com/gitshopapp/gitshop/internal/githubapp"
	"github.com/gitshopapp/gitshop/internal/handlers"
	"github.com/gitshopapp/gitshop/internal/services"
	"github.com/gitshopapp/gitshop/internal/session"
	"github.com/gitshopapp/gitshop/internal/stripe"
)

type App struct {
	Config         *config.Config
	Logger         *slog.Logger
	DB             *pgxpool.Pool
	CacheProvider  cache.Provider
	SessionManager *session.Manager
	Handlers       *handlers.Handlers
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := newLogger(cfg)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startupCancel()

	database, err := db.Connect(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	githubAuth, err := githubapp.NewAuth(cfg.GitHubAppID, cfg.GitHubPrivateKeyBase64)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to initialize GitHub auth: %w", err)
	}

	cacheProvider, err := cache.NewProvider(cache.Config{
		Provider:      cfg.CacheProvider,
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDB,
	})
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to initialize cache provider: %w", err)
	}

	sessionStore, err := session.NewStore(startupCtx, session.Config{
		Provider:      cfg.SessionStoreProvider,
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDB,
	})
	if err != nil {
		closeCacheProvider(logger, cacheProvider)
		database.Close()
		return nil, fmt.Errorf("failed to initialize session store: %w", err)
	}
	sessionManager := session.NewManager(sessionStore, handlers.SecureCookiesFromConfig(cfg))

	encryptor, err := crypto.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		closeSessionManager(logger, sessionManager)
		closeCacheProvider(logger, cacheProvider)
		database.Close()
		return nil, fmt.Errorf("failed to initialize encryptor: %w", err)
	}

	shopStore, err := db.NewShopStore(database, encryptor)
	if err != nil {
		closeSessionManager(logger, sessionManager)
		closeCacheProvider(logger, cacheProvider)
		database.Close()
		return nil, fmt.Errorf("failed to initialize shop store: %w", err)
	}
	orderStore := db.NewOrderStore(database)
	githubClient := githubapp.NewClient(githubAuth, logger.With("component", "github_client"))
	authService, err := services.NewAuthService(cfg, shopStore, logger.With("component", "auth_service"))
	if err != nil {
		closeSessionManager(logger, sessionManager)
		closeCacheProvider(logger, cacheProvider)
		database.Close()
		return nil, fmt.Errorf("failed to initialize auth service: %w", err)
	}

	var stripePlatform *stripe.PlatformClient
	if cfg.StripeConnectClientID != "" && cfg.BaseURL != "" && cfg.StripePlatformSecretKey != "" {
		stripePlatform = stripe.NewPlatformClient(cfg.StripePlatformSecretKey, cfg.StripeConnectClientID, cfg.BaseURL)
	}

	parser := catalog.NewParser()
	validator := catalog.NewValidator()
	pricer := catalog.NewPricer()
	orderEmailer := services.NewShopOrderEmailSender(email.NewProviderFromShop)

	orderService := services.NewOrderService(
		shopStore,
		orderStore,
		githubClient,
		stripePlatform,
		parser,
		validator,
		pricer,
		orderEmailer,
		logger.With("component", "order_service"),
	)
	installationService := services.NewInstallationService(shopStore, githubClient, logger.With("component", "installation_service"))
	repoService := services.NewRepositoryService(shopStore, logger.With("component", "repo_service"))
	githubRouter := handlers.NewGitHubEventRouter(orderService, installationService, repoService, logger.With("component", "github_router"))
	stripeService := services.NewStripeService(shopStore, orderStore, githubClient, parser, orderEmailer, logger.With("component", "stripe_service"))
	stripeRouter := handlers.NewStripeEventRouter(stripeService, logger.With("component", "stripe_router"))
	stripeConnectService := services.NewStripeConnectService(shopStore, stripePlatform, cacheProvider, logger.With("component", "stripe_connect_service"))
	adminService := services.NewAdminService(
		shopStore,
		orderStore,
		githubClient,
		stripePlatform,
		parser,
		validator,
		orderEmailer,
		catalog.NewTemplateSyncer,
		email.NewProvider,
		logger.With("component", "admin_service"),
	)

	h, err := handlers.New(handlers.Dependencies{
		Config:               cfg,
		DB:                   database,
		ShopStore:            shopStore,
		OrderStore:           orderStore,
		CacheProvider:        cacheProvider,
		GitHubAuth:           githubAuth,
		GitHubClient:         githubClient,
		GitHubRouter:         githubRouter,
		StripeRouter:         stripeRouter,
		AuthService:          authService,
		StripeConnectService: stripeConnectService,
		SessionManager:       sessionManager,
		AdminService:         adminService,
		Logger:               logger,
	})
	if err != nil {
		closeSessionManager(logger, sessionManager)
		closeCacheProvider(logger, cacheProvider)
		database.Close()
		return nil, fmt.Errorf("failed to initialize handlers: %w", err)
	}

	return &App{
		Config:         cfg,
		Logger:         logger,
		DB:             database,
		CacheProvider:  cacheProvider,
		SessionManager: sessionManager,
		Handlers:       h,
	}, nil
}

func (a *App) Close() {
	if a == nil {
		return
	}
	if a.SessionManager != nil {
		closeSessionManager(a.Logger, a.SessionManager)
	}
	if a.CacheProvider != nil {
		closeCacheProvider(a.Logger, a.CacheProvider)
	}
	if a.DB != nil {
		a.DB.Close()
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}

	format := strings.ToLower(strings.TrimSpace(cfg.LogFormat))
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	case "text", "":
		return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level: cfg.LogLevel,
		}))
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{Level: cfg.LogLevel}))
}

func closeSessionManager(logger *slog.Logger, manager *session.Manager) {
	if manager == nil {
		return
	}
	if err := manager.Close(); err != nil && logger != nil {
		logger.Warn("failed to close session manager", "error", err)
	}
}

func closeCacheProvider(logger *slog.Logger, provider cache.Provider) {
	if provider == nil {
		return
	}
	if err := provider.Close(); err != nil && logger != nil {
		logger.Warn("failed to close cache provider", "error", err)
	}
}
