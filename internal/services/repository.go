package services

import (
	"context"
	"log/slog"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/observability"
)

type RepositoryService struct {
	shopStore *db.ShopStore
	logger    *slog.Logger
}

func NewRepositoryService(shopStore *db.ShopStore, logger *slog.Logger) *RepositoryService {
	return &RepositoryService{shopStore: shopStore, logger: logger}
}

func (s *RepositoryService) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, s.logger)
}

type PushEventInput struct {
	RepoID       int64
	RepoFullName string
	Commits      []PushCommitInput
}

type PushCommitInput struct {
	Added    []string
	Modified []string
}

func (s *RepositoryService) HandlePushEvent(ctx context.Context, event PushEventInput) error {
	span := sentry.StartSpan(
		ctx,
		"service.repository.handle_push_event",
		sentry.WithOpName("service.repository"),
		sentry.WithDescription("HandlePushEvent"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(
		attribute.String("event", "push"),
		attribute.String("service", "repository"),
	)
	meter.Count("repository.event.received", 1)
	recordFailed := func(reason string) {
		meter.Count("repository.event.failed", 1, sentry.WithAttributes(attribute.String("reason", reason)))
	}

	if len(event.Commits) == 0 {
		meter.Count("repository.event.ignored", 1, sentry.WithAttributes(attribute.String("reason", "no_commits")))
		return nil
	}

	shop, err := s.shopStore.GetByRepoID(ctx, event.RepoID)
	if err != nil {
		recordFailed("shop_lookup_failed")
		s.loggerFromContext(ctx).Error("failed to find shop for repo", "error", err, "repo", event.RepoFullName)
		return nil
	}
	if shop == nil {
		meter.Count("repository.event.ignored", 1, sentry.WithAttributes(attribute.String("reason", "shop_not_found")))
		return nil
	}

	gitshopYamlModified := false
	for _, commit := range event.Commits {
		for _, f := range append(commit.Added, commit.Modified...) {
			if f == "gitshop.yaml" || f == "gitshop.yml" {
				gitshopYamlModified = true
				break
			}
		}
		if gitshopYamlModified {
			break
		}
	}

	if !gitshopYamlModified {
		meter.Count("repository.event.ignored", 1, sentry.WithAttributes(attribute.String("reason", "gitshop_config_unchanged")))
		return nil
	}

	s.loggerFromContext(ctx).Info("gitshop.yaml modified, skipping template sync (manual setup)", "repo", event.RepoFullName)
	meter.Count("repository.event.processed", 1)
	span.SetData("repository.repo_id", event.RepoID)
	span.SetData("repository.repo_full_name", event.RepoFullName)
	span.Status = sentry.SpanStatusOK
	return nil
}
