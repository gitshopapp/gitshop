// Package githubapp provides order template creation helpers.
package githubapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v66/github"
)

type FileCreationResult struct {
	Created      bool
	Method       string // "commit", "pr", or "exists"
	URL          string
	PRNumber     int
	ErrorMessage string
}

func (c *Client) EnsureOrderTemplate(ctx context.Context, owner, repo, templateContent string) (*FileCreationResult, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	templatePath := ".github/ISSUE_TEMPLATE/order.yaml"

	_, _, _, err = client.Repositories.GetContents(ctx, owner, repo, templatePath, nil)
	if err == nil {
		if c.logger != nil {
			c.logger.Info("order template already exists", "repo", fmt.Sprintf("%s/%s", owner, repo))
		}
		return &FileCreationResult{
			Created: false,
			Method:  "exists",
		}, nil
	}

	defaultBranch, err := c.getDefaultBranch(ctx, client, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	message := "Add GitShop order template"
	commitURL, err := c.createFileDirectlyWithPath(ctx, client, owner, repo, defaultBranch, templatePath, templateContent, message)
	if err == nil {
		if c.logger != nil {
			c.logger.Info("order template created via direct commit", "repo", fmt.Sprintf("%s/%s", owner, repo))
		}
		return &FileCreationResult{
			Created: true,
			Method:  "commit",
			URL:     commitURL,
		}, nil
	}

	if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "protected") {
		if c.logger != nil {
			c.logger.Info("Direct commit failed, trying PR approach", "repo", fmt.Sprintf("%s/%s", owner, repo), "error", err)
		}
		prTitle := "Setup GitShop - Add order template"
		prBody := "This PR adds the GitShop order issue template.\n\nPlease review and merge to start accepting orders via GitHub issues."
		return c.createFileViaPRWithPath(ctx, client, owner, repo, defaultBranch, templatePath, templateContent, prTitle, prBody, "gitshop/setup-order-template")
	}

	return nil, fmt.Errorf("failed to create order template: %w", err)
}

func (c *Client) CreateOrUpdateOrderTemplate(ctx context.Context, owner, repo, templateContent string) (*FileCreationResult, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	templatePath := ".github/ISSUE_TEMPLATE/order.yaml"
	defaultBranch, err := c.getDefaultBranch(ctx, client, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	existing, _, _, err := client.Repositories.GetContents(ctx, owner, repo, templatePath, nil)
	if err == nil && existing != nil && existing.SHA != nil {
		message := "Sync GitShop order template"
		if url, updateErr := c.updateFileDirectlyWithPath(ctx, client, owner, repo, defaultBranch, templatePath, templateContent, message, *existing.SHA); updateErr == nil {
			return &FileCreationResult{Created: true, Method: "commit", URL: url}, nil
		} else if strings.Contains(updateErr.Error(), "409") || strings.Contains(updateErr.Error(), "protected") {
			prTitle := "Sync GitShop order template"
			prBody := "This PR synchronizes the GitShop order issue template with your current `gitshop.yaml`."
			return c.createFileViaPRWithPath(ctx, client, owner, repo, defaultBranch, templatePath, templateContent, prTitle, prBody, "gitshop/sync-order-template")
		} else {
			return nil, fmt.Errorf("failed to update order template: %w", updateErr)
		}
	}

	return c.EnsureOrderTemplate(ctx, owner, repo, templateContent)
}

func (c *Client) CreateOrUpdateFileWithPR(ctx context.Context, owner, repo, path, content, message, prTitle, prBody, branchName string) (*FileCreationResult, error) {
	client, err := c.getGitHubClient(ctx)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := c.getDefaultBranch(ctx, client, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	existing, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, nil)
	if err == nil && existing != nil && existing.SHA != nil {
		if url, updateErr := c.updateFileDirectlyWithPath(ctx, client, owner, repo, defaultBranch, path, content, message, *existing.SHA); updateErr == nil {
			return &FileCreationResult{Created: true, Method: "commit", URL: url}, nil
		} else if strings.Contains(updateErr.Error(), "409") || strings.Contains(updateErr.Error(), "protected") {
			return c.createFileViaPRWithPath(ctx, client, owner, repo, defaultBranch, path, content, prTitle, prBody, branchName)
		} else {
			return nil, fmt.Errorf("failed to update file: %w", updateErr)
		}
	}

	if url, createErr := c.createFileDirectlyWithPath(ctx, client, owner, repo, defaultBranch, path, content, message); createErr == nil {
		return &FileCreationResult{Created: true, Method: "commit", URL: url}, nil
	} else if strings.Contains(createErr.Error(), "409") || strings.Contains(createErr.Error(), "protected") {
		return c.createFileViaPRWithPath(ctx, client, owner, repo, defaultBranch, path, content, prTitle, prBody, branchName)
	} else {
		return nil, fmt.Errorf("failed to create file: %w", createErr)
	}
}

func (c *Client) createFileDirectlyWithPath(ctx context.Context, client *github.Client, owner, repo, branch, path, content, message string) (string, error) {
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(content),
		Branch:  &branch,
	}

	_, _, err := client.Repositories.CreateFile(ctx, owner, repo, path, opts)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, branch, path), nil
}

func (c *Client) createFileViaPRWithPath(ctx context.Context, client *github.Client, owner, repo, defaultBranch, path, content, prTitle, prBody, branchName string) (*FileCreationResult, error) {
	ref, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to get ref: %w", err)
	}

	newRef := &github.Reference{
		Ref: github.String("refs/heads/" + branchName),
		Object: &github.GitObject{
			SHA: ref.Object.SHA,
		},
	}
	_, _, err = client.Git.CreateRef(ctx, owner, repo, newRef)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("Failed to create branch, may already exist", "error", err)
		}
	}

	message := "Add GitShop order template"
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(content),
		Branch:  &branchName,
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create file on branch: %w", err)
	}

	pr := &github.NewPullRequest{
		Title: &prTitle,
		Body:  &prBody,
		Head:  &branchName,
		Base:  &defaultBranch,
	}

	createdPR, _, err := client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	if c.logger != nil {
		c.logger.Info("order template created via PR", "repo", fmt.Sprintf("%s/%s", owner, repo), "pr_number", *createdPR.Number)
	}

	return &FileCreationResult{
		Created:  true,
		Method:   "pr",
		URL:      *createdPR.HTMLURL,
		PRNumber: *createdPR.Number,
	}, nil
}

func (c *Client) updateFileDirectlyWithPath(ctx context.Context, client *github.Client, owner, repo, branch, path, content, message, sha string) (string, error) {
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(content),
		Branch:  &branch,
		SHA:     &sha,
	}

	_, _, err := client.Repositories.CreateFile(ctx, owner, repo, path, opts)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, branch, path), nil
}
