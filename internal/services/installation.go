package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/githubapp"
	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/observability"
)

type InstallationService struct {
	shopStore    *db.ShopStore
	githubClient *githubapp.Client
	logger       *slog.Logger
}

type RepositoryInput struct {
	ID       int64
	FullName string
}

type InstallationEventInput struct {
	Action         string
	InstallationID int64
	Repositories   []RepositoryInput
}

type InstallationRepositoriesEventInput struct {
	Action              string
	InstallationID      int64
	RepositoriesAdded   []RepositoryInput
	RepositoriesRemoved []RepositoryInput
}

func NewInstallationService(shopStore *db.ShopStore, githubClient *githubapp.Client, logger *slog.Logger) *InstallationService {
	return &InstallationService{
		shopStore:    shopStore,
		githubClient: githubClient,
		logger:       logger,
	}
}

func (s *InstallationService) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, s.logger)
}

func (s *InstallationService) HandleInstallationEvent(ctx context.Context, event InstallationEventInput) (err error) {
	span := sentry.StartSpan(
		ctx,
		"service.installation.handle_installation_event",
		sentry.WithOpName("service.installation"),
		sentry.WithDescription("HandleInstallationEvent"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(
		attribute.String("event", "installation"),
		attribute.String("action", event.Action),
	)
	meter.Count("installation.event.received", 1)
	span.SetData("github.installation_id", event.InstallationID)
	defer func() {
		if err != nil {
			meter.Count("installation.event.failed", 1)
			span.Status = sentry.SpanStatusInternalError
			return
		}
		meter.Count("installation.event.processed", 1)
		span.Status = sentry.SpanStatusOK
	}()

	logger := s.loggerFromContext(ctx)

	switch event.Action {
	case "created":
		senderEmail := ""
		for _, repo := range event.Repositories {
			repoID := repo.ID
			repoFullName := repo.FullName

			existingShop, err := s.shopStore.GetByInstallationAndRepoID(ctx, event.InstallationID, repoID)
			if err == nil && existingShop != nil {
				if existingShop.IsConnected() {
					logger.Info("shop already exists and connected for repo",
						"installation_id", event.InstallationID,
						"repo_id", repoID,
						"repo", repoFullName,
						"shop_id", existingShop.ID)
					continue
				}
				logger.Info("reconnecting previously disconnected shop",
					"installation_id", event.InstallationID,
					"repo_id", repoID,
					"repo", repoFullName,
					"shop_id", existingShop.ID)
				if reconnectErr := s.shopStore.ReconnectShop(ctx, event.InstallationID, repoID); reconnectErr != nil {
					logger.Error("failed to reconnect shop", "error", reconnectErr, "shop_id", existingShop.ID)
				}
				continue
			}

			shop, err := s.shopStore.Create(ctx, event.InstallationID, repoID, repoFullName, senderEmail)
			if err != nil {
				logger.Error("failed to create shop", "error", err, "installation_id", event.InstallationID, "repo_id", repoID, "repo", repoFullName)
				continue
			}

			logger.Info("created shop for installation via webhook",
				"installation_id", event.InstallationID,
				"repo_id", repoID,
				"repo", repoFullName,
				"shop_id", shop.ID)
		}

	case "deleted":
		logger.Info("installation deleted - disconnecting shops", "installation_id", event.InstallationID)

		shops, err := s.shopStore.GetShopsByInstallationID(ctx, event.InstallationID)
		if err != nil {
			logger.Error("failed to get shops for disconnection", "error", err, "installation_id", event.InstallationID)
			return fmt.Errorf("failed to get shops: %w", err)
		}

		disconnectedCount := 0
		for _, shop := range shops {
			if err := s.shopStore.DisconnectShop(ctx, event.InstallationID, shop.GitHubRepoID); err != nil {
				logger.Error("failed to disconnect shop", "error", err, "shop_id", shop.ID, "repo_id", shop.GitHubRepoID)
			} else {
				logger.Info("shop disconnected", "shop_id", shop.ID, "repo_id", shop.GitHubRepoID)
				disconnectedCount++
			}
		}

		logger.Info("installation deletion completed",
			"installation_id", event.InstallationID,
			"shops_disconnected", disconnectedCount,
			"total_shops", len(shops))

	case "suspend":
		logger.Info("installation suspended - suspending shops", "installation_id", event.InstallationID)

		shops, err := s.shopStore.GetShopsByInstallationID(ctx, event.InstallationID)
		if err != nil {
			logger.Error("failed to get shops for suspension", "error", err, "installation_id", event.InstallationID)
			return fmt.Errorf("failed to get shops: %w", err)
		}

		suspendedCount := 0
		for _, shop := range shops {
			if err := s.shopStore.SuspendShop(ctx, event.InstallationID, shop.GitHubRepoID); err != nil {
				logger.Error("failed to suspend shop", "error", err, "shop_id", shop.ID, "repo_id", shop.GitHubRepoID)
			} else {
				logger.Info("shop suspended", "shop_id", shop.ID, "repo_id", shop.GitHubRepoID)
				suspendedCount++
			}
		}

		logger.Info("installation suspension completed",
			"installation_id", event.InstallationID,
			"shops_suspended", suspendedCount,
			"total_shops", len(shops))

	case "unsuspend":
		logger.Info("installation unsuspended - unsuspending shops", "installation_id", event.InstallationID)

		shops, err := s.shopStore.GetShopsByInstallationID(ctx, event.InstallationID)
		if err != nil {
			logger.Error("failed to get shops for unsuspension", "error", err, "installation_id", event.InstallationID)
			return fmt.Errorf("failed to get shops: %w", err)
		}

		unsuspendedCount := 0
		for _, shop := range shops {
			if err := s.shopStore.UnsuspendShop(ctx, event.InstallationID, shop.GitHubRepoID); err != nil {
				logger.Error("failed to unsuspend shop", "error", err, "shop_id", shop.ID, "repo_id", shop.GitHubRepoID)
			} else {
				logger.Info("shop unsuspended", "shop_id", shop.ID, "repo_id", shop.GitHubRepoID)
				unsuspendedCount++
			}
		}

		logger.Info("installation unsuspension completed",
			"installation_id", event.InstallationID,
			"shops_unsuspended", unsuspendedCount,
			"total_shops", len(shops))

	case "new_permissions_accepted":
		logger.Info("installation new permissions accepted",
			"installation_id", event.InstallationID,
			"event", "new_permissions_accepted")

	default:
		logger.Info("unhandled installation action", "action", event.Action)
	}

	return nil
}

func (s *InstallationService) HandleInstallationRepositoriesEvent(ctx context.Context, event InstallationRepositoriesEventInput) (err error) {
	span := sentry.StartSpan(
		ctx,
		"service.installation.handle_installation_repositories_event",
		sentry.WithOpName("service.installation"),
		sentry.WithDescription("HandleInstallationRepositoriesEvent"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(
		attribute.String("event", "installation_repositories"),
		attribute.String("action", event.Action),
	)
	meter.Count("installation.event.received", 1)
	span.SetData("github.installation_id", event.InstallationID)
	defer func() {
		if err != nil {
			meter.Count("installation.event.failed", 1)
			span.Status = sentry.SpanStatusInternalError
			return
		}
		meter.Count("installation.event.processed", 1)
		span.Status = sentry.SpanStatusOK
	}()

	logger := s.loggerFromContext(ctx)

	installationID := event.InstallationID
	githubClient := s.githubClient.WithInstallation(installationID)

	switch event.Action {
	case "added":
		repositoriesAdded := event.RepositoriesAdded
		logger.Info("repositories added to installation",
			"installation_id", installationID,
			"count", len(repositoriesAdded))

		for _, repo := range repositoriesAdded {
			repoID := repo.ID
			repoFullName := repo.FullName

			existingShop, err := s.shopStore.GetByInstallationAndRepoID(ctx, installationID, repoID)
			if err == nil && existingShop != nil {
				if existingShop.IsConnected() {
					logger.Info("shop already exists and connected for added repo",
						"installation_id", installationID,
						"repo_id", repoID,
						"repo", repoFullName,
						"shop_id", existingShop.ID)
					continue
				}
				logger.Info("reconnecting shop for added repo",
					"installation_id", installationID,
					"repo_id", repoID,
					"repo", repoFullName,
					"shop_id", existingShop.ID)
				if reconnectErr := s.shopStore.ReconnectShop(ctx, installationID, repoID); reconnectErr != nil {
					logger.Error("failed to reconnect shop for added repo", "error", reconnectErr, "shop_id", existingShop.ID)
				}
				continue
			}

			shop, err := s.shopStore.Create(ctx, installationID, repoID, repoFullName, "")
			if err != nil {
				logger.Error("failed to create shop for added repo", "error", err, "installation_id", installationID, "repo_id", repoID, "repo", repoFullName)
				continue
			}

			logger.Info("created shop for added repository",
				"installation_id", installationID,
				"repo_id", repoID,
				"repo", repoFullName,
				"shop_id", shop.ID)

			if githubClient != nil {
				logger.Info("shop created for added repo", "repo", repoFullName)
			}
		}

	case "removed":
		repositoriesRemoved := event.RepositoriesRemoved
		logger.Info("repositories removed from installation",
			"installation_id", installationID,
			"count", len(repositoriesRemoved))

		for _, repo := range repositoriesRemoved {
			repoID := repo.ID
			repoFullName := repo.FullName

			if err := s.shopStore.DisconnectShop(ctx, installationID, repoID); err != nil {
				logger.Error("failed to disconnect shop for removed repo", "error", err, "installation_id", installationID, "repo_id", repoID, "repo", repoFullName)
			} else {
				logger.Info("shop disconnected for removed repository",
					"installation_id", installationID,
					"repo_id", repoID,
					"repo", repoFullName)
			}
		}

	default:
		logger.Info("unhandled installation_repositories action", "action", event.Action)
	}

	return nil
}
