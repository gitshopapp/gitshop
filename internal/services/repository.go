package services

import (
	"context"
	"log/slog"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/logging"
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
	if len(event.Commits) == 0 {
		return nil
	}

	shop, err := s.shopStore.GetByRepoID(ctx, event.RepoID)
	if err != nil {
		s.loggerFromContext(ctx).Error("failed to find shop for repo", "error", err, "repo", event.RepoFullName)
		return nil
	}
	if shop == nil {
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
		return nil
	}

	s.loggerFromContext(ctx).Info("gitshop.yaml modified, skipping template sync (manual setup)", "repo", event.RepoFullName)
	return nil
}
