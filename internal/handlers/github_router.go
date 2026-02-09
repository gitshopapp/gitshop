package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/go-github/v66/github"

	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/services"
)

type GitHubEventRouter struct {
	orderService        *services.OrderService
	installationService *services.InstallationService
	repoService         *services.RepositoryService
	logger              *slog.Logger
}

func NewGitHubEventRouter(orderService *services.OrderService, installationService *services.InstallationService, repoService *services.RepositoryService, logger *slog.Logger) *GitHubEventRouter {
	return &GitHubEventRouter{
		orderService:        orderService,
		installationService: installationService,
		repoService:         repoService,
		logger:              logger,
	}
}

func (r *GitHubEventRouter) Handle(ctx context.Context, eventType string, payload []byte) error {
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		return fmt.Errorf("failed to parse GitHub webhook: %w", err)
	}

	logger := logging.FromContext(ctx, r.logger)

	switch e := event.(type) {
	case *github.IssuesEvent:
		if e.GetAction() != "opened" {
			return nil
		}
		issue := e.GetIssue()
		repo := e.GetRepo()
		installation := e.GetInstallation()
		if issue == nil || repo == nil || installation == nil {
			return fmt.Errorf("missing issue, repository, or installation data")
		}
		if !services.IsOrderIssue(issue) {
			return nil
		}
		username := ""
		if issue.User != nil {
			username = issue.User.GetLogin()
		}
		return r.orderService.HandleIssueOpened(ctx, services.IssueOpenedInput{
			InstallationID: installation.GetID(),
			RepoID:         repo.GetID(),
			RepoFullName:   repo.GetFullName(),
			IssueNumber:    issue.GetNumber(),
			IssueURL:       issue.GetHTMLURL(),
			IssueTitle:     issue.GetTitle(),
			IssueUsername:  username,
			IssueBody:      issue.GetBody(),
		})
	case *github.IssueCommentEvent:
		if e.GetAction() != "created" {
			return nil
		}
		comment := e.GetComment()
		issue := e.GetIssue()
		repo := e.GetRepo()
		installation := e.GetInstallation()
		if comment == nil || issue == nil || repo == nil || installation == nil {
			return fmt.Errorf("missing comment, issue, repository, or installation data")
		}
		commenter := ""
		if comment.User != nil {
			commenter = comment.User.GetLogin()
		}
		return r.orderService.HandleIssueCommentCreated(ctx, services.IssueCommentCreatedInput{
			InstallationID: installation.GetID(),
			RepoID:         repo.GetID(),
			RepoFullName:   repo.GetFullName(),
			IssueNumber:    issue.GetNumber(),
			CommentBody:    comment.GetBody(),
			CommenterLogin: commenter,
		})
	case *github.PushEvent:
		repo := e.GetRepo()
		if repo == nil {
			return nil
		}
		commits := make([]services.PushCommitInput, 0, len(e.Commits))
		for _, c := range e.Commits {
			commits = append(commits, services.PushCommitInput{
				Added:    append([]string{}, c.Added...),
				Modified: append([]string{}, c.Modified...),
			})
		}
		return r.repoService.HandlePushEvent(ctx, services.PushEventInput{
			RepoID:       repo.GetID(),
			RepoFullName: repo.GetFullName(),
			Commits:      commits,
		})
	case *github.InstallationEvent:
		installation := e.GetInstallation()
		if installation == nil {
			return fmt.Errorf("invalid installation data")
		}
		repositories := make([]services.RepositoryInput, 0, len(e.Repositories))
		for _, repo := range e.Repositories {
			if repo == nil {
				continue
			}
			repositories = append(repositories, services.RepositoryInput{
				ID:       repo.GetID(),
				FullName: repo.GetFullName(),
			})
		}
		return r.installationService.HandleInstallationEvent(ctx, services.InstallationEventInput{
			Action:         e.GetAction(),
			InstallationID: installation.GetID(),
			Repositories:   repositories,
		})
	case *github.InstallationRepositoriesEvent:
		installation := e.GetInstallation()
		if installation == nil {
			return fmt.Errorf("invalid installation data")
		}
		added := make([]services.RepositoryInput, 0, len(e.RepositoriesAdded))
		for _, repo := range e.RepositoriesAdded {
			if repo == nil {
				continue
			}
			added = append(added, services.RepositoryInput{
				ID:       repo.GetID(),
				FullName: repo.GetFullName(),
			})
		}
		removed := make([]services.RepositoryInput, 0, len(e.RepositoriesRemoved))
		for _, repo := range e.RepositoriesRemoved {
			if repo == nil {
				continue
			}
			removed = append(removed, services.RepositoryInput{
				ID:       repo.GetID(),
				FullName: repo.GetFullName(),
			})
		}
		return r.installationService.HandleInstallationRepositoriesEvent(ctx, services.InstallationRepositoriesEventInput{
			Action:              e.GetAction(),
			InstallationID:      installation.GetID(),
			RepositoriesAdded:   added,
			RepositoriesRemoved: removed,
		})
	default:
		logger.Info("unhandled GitHub event type", "type", eventType)
		return nil
	}
}
