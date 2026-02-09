package githubapp

// Package githubapp provides GitHub API client functionality.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"
)

type Client struct {
	auth           *Auth
	installationID int64
	logger         *slog.Logger
}

func NewClient(auth *Auth, logger *slog.Logger) *Client {
	return &Client{
		auth:   auth,
		logger: logger,
	}
}

func (c *Client) WithInstallation(installationID int64) *Client {
	return &Client{
		auth:           c.auth,
		installationID: installationID,
		logger:         c.logger,
	}
}

func (c *Client) getGitHubClient(ctx context.Context) (*github.Client, error) {
	token, err := c.auth.GetInstallationToken(ctx, c.installationID)
	if err != nil {
		return nil, err
	}

	ts := oauth2.StaticTokenSource(token)
	tc := oauth2.NewClient(ctx, ts)
	tc.Timeout = 15 * time.Second

	return github.NewClient(tc), nil
}

func (c *Client) EnsureGitShopYAMLForRepo(ctx context.Context, owner, repo, shopName string) (*YAMLCreationResult, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}
	return c.EnsureGitShopYAML(ctx, client, owner, repo, shopName)
}

func (c *Client) GetFile(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s: %w", path, err)
	}

	if fileContent == nil {
		return nil, fmt.Errorf("file %s not found", path)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to decode file content: %w", err)
	}

	return []byte(content), nil
}

type FileStatus struct {
	Exists      bool
	HTMLURL     string
	LastUpdated time.Time
}

type RepoFile struct {
	Name    string
	Path    string
	HTMLURL string
}

type LabelDefinition struct {
	Name        string
	Color       string
	Description string
}

func (c *Client) GetFileStatus(ctx context.Context, repoFullName, path string) (*FileStatus, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, nil)
	if err != nil {
		if isNotFound(err) {
			return &FileStatus{Exists: false}, nil
		}
		return nil, fmt.Errorf("failed to get file status %s: %w", path, err)
	}

	status := &FileStatus{Exists: true}
	if fileContent != nil && fileContent.HTMLURL != nil {
		status.HTMLURL = *fileContent.HTMLURL
	}

	commits, _, err := client.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
		Path:        path,
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err == nil && len(commits) > 0 && commits[0].Commit != nil && commits[0].Commit.Committer != nil && commits[0].Commit.Committer.Date != nil {
		status.LastUpdated = commits[0].Commit.Committer.Date.Time
	}

	return status, nil
}

func (c *Client) ListDirectory(ctx context.Context, repoFullName, path string) ([]RepoFile, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	fileContent, dirContent, _, err := client.Repositories.GetContents(ctx, owner, repo, path, nil)
	if err != nil {
		if isNotFound(err) {
			return []RepoFile{}, nil
		}
		return nil, fmt.Errorf("failed to list directory %s: %w", path, err)
	}

	files := []RepoFile{}
	if fileContent != nil {
		files = append(files, RepoFile{
			Name:    fileContent.GetName(),
			Path:    fileContent.GetPath(),
			HTMLURL: fileContent.GetHTMLURL(),
		})
		return files, nil
	}

	for _, item := range dirContent {
		if item == nil || item.GetType() != "file" {
			continue
		}
		files = append(files, RepoFile{
			Name:    item.GetName(),
			Path:    item.GetPath(),
			HTMLURL: item.GetHTMLURL(),
		})
	}

	return files, nil
}

func isNotFound(err error) bool {
	var errResp *github.ErrorResponse
	if errors.As(err, &errResp) && errResp.Response != nil {
		return errResp.Response.StatusCode == 404
	}
	return false
}

func (c *Client) CreateOrUpdateFile(ctx context.Context, repoFullName, path, content, message string) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	// Try to get existing file to get SHA
	var sha *string
	existingFile, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, nil)
	if err == nil && existingFile != nil {
		sha = existingFile.SHA
	}

	// Create or update file
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(content),
		SHA:     sha,
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, path, opts)
	if err != nil {
		return fmt.Errorf("failed to create/update file %s: %w", path, err)
	}

	return nil
}

func (c *Client) CreateComment(ctx context.Context, repoFullName string, issueNumber int, body string) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	comment := &github.IssueComment{
		Body: &body,
	}

	_, _, err = client.Issues.CreateComment(ctx, owner, repo, issueNumber, comment)
	if err != nil {
		return fmt.Errorf("failed to create comment: %w", err)
	}

	return nil
}

func (c *Client) ListComments(ctx context.Context, repoFullName string, issueNumber int) ([]*github.IssueComment, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	opts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	comments := []*github.IssueComment{}
	for {
		pageComments, resp, err := client.Issues.ListComments(ctx, owner, repo, issueNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list comments: %w", err)
		}
		comments = append(comments, pageComments...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return comments, nil
}

func (c *Client) DeleteComment(ctx context.Context, repoFullName string, commentID int64) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	_, err = client.Issues.DeleteComment(ctx, owner, repo, commentID)
	if err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}

	return nil
}

func (c *Client) AddLabels(ctx context.Context, repoFullName string, issueNumber int, labels []string) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	_, _, err = client.Issues.AddLabelsToIssue(ctx, owner, repo, issueNumber, labels)
	if err != nil {
		return fmt.Errorf("failed to add labels: %w", err)
	}

	return nil
}

func (c *Client) RemoveLabel(ctx context.Context, repoFullName string, issueNumber int, label string) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	_, err = client.Issues.RemoveLabelForIssue(ctx, owner, repo, issueNumber, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}

	return nil
}

func (c *Client) CloseIssue(ctx context.Context, repoFullName string, issueNumber int) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	state := "closed"
	issueRequest := &github.IssueRequest{
		State: &state,
	}

	_, _, err = client.Issues.Edit(ctx, owner, repo, issueNumber, issueRequest)
	if err != nil {
		return fmt.Errorf("failed to close issue: %w", err)
	}

	return nil
}

func (c *Client) CreateIssue(ctx context.Context, repoFullName string, title, body string, labels []string, assignees []string) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	issueRequest := &github.IssueRequest{
		Title: &title,
		Body:  &body,
	}
	if len(labels) > 0 {
		issueRequest.Labels = &labels
	}
	if len(assignees) > 0 {
		issueRequest.Assignees = &assignees
	}

	_, _, err = client.Issues.Create(ctx, owner, repo, issueRequest)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	return nil
}

func (c *Client) UpdateIssueTitle(ctx context.Context, repoFullName string, issueNumber int, title string) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	issueRequest := &github.IssueRequest{
		Title: &title,
	}

	_, _, err = client.Issues.Edit(ctx, owner, repo, issueNumber, issueRequest)
	if err != nil {
		return fmt.Errorf("failed to update issue title: %w", err)
	}

	return nil
}

func (c *Client) AssignIssue(ctx context.Context, repoFullName string, issueNumber int, assignees []string) error {
	if len(assignees) == 0 {
		return nil
	}

	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	issueRequest := &github.IssueRequest{
		Assignees: &assignees,
	}
	_, _, err = client.Issues.Edit(ctx, owner, repo, issueNumber, issueRequest)
	if err != nil {
		return fmt.Errorf("failed to assign issue: %w", err)
	}
	return nil
}

func (c *Client) EnsureLabels(ctx context.Context, repoFullName string, labels []LabelDefinition) error {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	for _, label := range labels {
		params := &github.Label{
			Name:        github.String(label.Name),
			Color:       github.String(label.Color),
			Description: github.String(label.Description),
		}

		_, _, err := client.Issues.CreateLabel(ctx, owner, repo, params)
		if err != nil {
			if isLabelExists(err) {
				continue
			}
			return fmt.Errorf("failed to create label %s: %w", label.Name, err)
		}
	}

	return nil
}

func (c *Client) ListLabels(ctx context.Context, repoFullName string) (map[string]github.Label, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	labels := make(map[string]github.Label)
	opts := &github.ListOptions{PerPage: 100}
	for {
		pageLabels, resp, err := client.Issues.ListLabels(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list labels: %w", err)
		}
		for _, label := range pageLabels {
			if label.Name != nil {
				labels[*label.Name] = *label
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return labels, nil
}

func isLabelExists(err error) bool {
	var errResp *github.ErrorResponse
	if errors.As(err, &errResp) && errResp.Response != nil {
		return errResp.Response.StatusCode == 422
	}
	return false
}

type Installation struct {
	ID           int64
	AccountID    int64
	AccountLogin string
	AccountType  string
	AvatarURL    string
	Permissions  map[string]string
}

func (c *Client) CheckPermission(ctx context.Context, repoFullName, username string) (bool, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return false, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repo full name: %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	perm, _, err := client.Repositories.GetPermissionLevel(ctx, owner, repo, username)
	if err != nil {
		return false, fmt.Errorf("failed to check permission: %w", err)
	}

	if perm.Permission == nil {
		return false, nil
	}

	return *perm.Permission == "write" || *perm.Permission == "admin", nil
}

func (c *Client) GetInstallation(ctx context.Context, userAccessToken string, installationID int64) (*Installation, error) {
	client := github.NewClient(nil)
	client = client.WithAuthToken(userAccessToken)

	installation, _, err := client.Apps.GetInstallation(ctx, installationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation: %w", err)
	}

	perms := make(map[string]string)
	if installation.Permissions != nil {
		if v := installation.Permissions.Metadata; v != nil {
			perms["metadata"] = *v
		}
		if v := installation.Permissions.Contents; v != nil {
			perms["contents"] = *v
		}
		if v := installation.Permissions.Issues; v != nil {
			perms["issues"] = *v
		}
		if v := installation.Permissions.SingleFile; v != nil {
			perms["single_file"] = *v
		}
		if v := installation.Permissions.Checks; v != nil {
			perms["checks"] = *v
		}
		if v := installation.Permissions.Workflows; v != nil {
			perms["workflows"] = *v
		}
	}

	acct := installation.GetAccount()
	return &Installation{
		ID:           installation.GetID(),
		AccountID:    acct.GetID(),
		AccountLogin: acct.GetLogin(),
		AccountType:  acct.GetType(),
		AvatarURL:    acct.GetAvatarURL(),
		Permissions:  perms,
	}, nil
}
