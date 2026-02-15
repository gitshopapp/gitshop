package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/go-github/v66/github"

	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/observability"
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
	span := sentry.StartSpan(
		ctx,
		"handler.github_router.handle",
		sentry.WithOpName("handler.github_router"),
		sentry.WithDescription("GitHubEventRouter.Handle"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(
		attribute.String("webhook.provider", "github"),
		attribute.String("webhook.event_type", eventType),
	)
	meter.Count("webhook.router.received", 1)
	recordFailed := func(reason string) {
		meter.Count("webhook.router.failed", 1, sentry.WithAttributes(attribute.String("reason", reason)))
	}

	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		recordFailed("parse_failed")
		span.Status = sentry.SpanStatusInvalidArgument
		return fmt.Errorf("failed to parse GitHub webhook: %w", err)
	}

	logger := logging.FromContext(ctx, r.logger)

	switch e := event.(type) {
	case *github.IssuesEvent:
		if e.GetAction() != "opened" {
			meter.Count("webhook.router.ignored", 1, sentry.WithAttributes(attribute.String("reason", "issues_action_not_opened")))
			return nil
		}
		issue := e.GetIssue()
		repo := e.GetRepo()
		installation := e.GetInstallation()
		if issue == nil || repo == nil || installation == nil {
			recordFailed("missing_issue_repo_or_installation")
			return fmt.Errorf("missing issue, repository, or installation data")
		}
		if !services.IsOrderIssue(issue) {
			meter.Count("webhook.router.ignored", 1, sentry.WithAttributes(attribute.String("reason", "issue_not_order_template")))
			return nil
		}
		username := ""
		if issue.User != nil {
			username = issue.User.GetLogin()
		}
		err = r.orderService.HandleIssueOpened(ctx, services.IssueOpenedInput{
			InstallationID: installation.GetID(),
			RepoID:         repo.GetID(),
			RepoFullName:   repo.GetFullName(),
			IssueNumber:    issue.GetNumber(),
			IssueURL:       issue.GetHTMLURL(),
			IssueTitle:     issue.GetTitle(),
			IssueUsername:  username,
			IssueBody:      issue.GetBody(),
		})
		if err != nil {
			recordFailed("order_issue_opened_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	case *github.IssueCommentEvent:
		if e.GetAction() != "created" {
			meter.Count("webhook.router.ignored", 1, sentry.WithAttributes(attribute.String("reason", "issue_comment_action_not_created")))
			return nil
		}
		comment := e.GetComment()
		issue := e.GetIssue()
		repo := e.GetRepo()
		installation := e.GetInstallation()
		if comment == nil || issue == nil || repo == nil || installation == nil {
			recordFailed("missing_comment_issue_repo_or_installation")
			return fmt.Errorf("missing comment, issue, repository, or installation data")
		}
		commenter := ""
		if comment.User != nil {
			commenter = comment.User.GetLogin()
		}
		err = r.orderService.HandleIssueCommentCreated(ctx, services.IssueCommentCreatedInput{
			InstallationID: installation.GetID(),
			RepoID:         repo.GetID(),
			RepoFullName:   repo.GetFullName(),
			IssueNumber:    issue.GetNumber(),
			CommentBody:    comment.GetBody(),
			CommenterLogin: commenter,
		})
		if err != nil {
			recordFailed("order_issue_comment_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	case *github.PushEvent:
		repo := e.GetRepo()
		if repo == nil {
			meter.Count("webhook.router.ignored", 1, sentry.WithAttributes(attribute.String("reason", "missing_repo")))
			return nil
		}
		commits := make([]services.PushCommitInput, 0, len(e.Commits))
		for _, c := range e.Commits {
			commits = append(commits, services.PushCommitInput{
				Added:    append([]string{}, c.Added...),
				Modified: append([]string{}, c.Modified...),
			})
		}
		err = r.repoService.HandlePushEvent(ctx, services.PushEventInput{
			RepoID:       repo.GetID(),
			RepoFullName: repo.GetFullName(),
			Commits:      commits,
		})
		if err != nil {
			recordFailed("push_event_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	case *github.InstallationEvent:
		installation := e.GetInstallation()
		if installation == nil {
			recordFailed("missing_installation")
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
		err = r.installationService.HandleInstallationEvent(ctx, services.InstallationEventInput{
			Action:         e.GetAction(),
			InstallationID: installation.GetID(),
			Repositories:   repositories,
		})
		if err != nil {
			recordFailed("installation_event_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	case *github.InstallationRepositoriesEvent:
		installation := e.GetInstallation()
		if installation == nil {
			recordFailed("missing_installation")
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
		err = r.installationService.HandleInstallationRepositoriesEvent(ctx, services.InstallationRepositoriesEventInput{
			Action:              e.GetAction(),
			InstallationID:      installation.GetID(),
			RepositoriesAdded:   added,
			RepositoriesRemoved: removed,
		})
		if err != nil {
			recordFailed("installation_repositories_event_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	default:
		logger.Info("unhandled GitHub event type", "type", eventType)
		meter.Count("webhook.router.unhandled", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	}
}
