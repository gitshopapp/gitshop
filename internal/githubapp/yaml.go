// Package githubapp provides gitshop.yaml creation and management.
package githubapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v66/github"
)

const DefaultGitShopYAML = `# GitShop Configuration
# This file defines your product catalog and store settings.
# The product below is a sample. Replace it with your own items.

shop:
  name: "%s"
  currency: "usd"
  manager: ""
  shipping:
    flat_rate_cents: 500
    carrier: "USPS"

products:
  - sku: "EXAMPLE_TSHIRT"
    name: "Example T-Shirt"
    description: "Sample product — replace this with your own catalog."
    unit_price_cents: 2500
    active: true
    options:
      - name: "size"
        label: "Size"
        type: "dropdown"
        required: true
        values: ["S", "M", "L", "XL"]
`

type YAMLCreationResult struct {
	Created      bool
	Method       string // "commit", "pr", or "exists"
	URL          string
	PRNumber     int
	ErrorMessage string
}

// EnsureGitShopYAML checks if gitshop.yaml exists in the repo and creates it if not.
// It attempts to commit directly first, and falls back to creating a PR if the branch is protected.
func (c *Client) EnsureGitShopYAML(ctx context.Context, client *github.Client, owner, repo, shopName string) (*YAMLCreationResult, error) {
	// Check if gitshop.yaml already exists
	_, _, _, err := client.Repositories.GetContents(ctx, owner, repo, "gitshop.yaml", nil)
	if err == nil {
		if c.logger != nil {
			c.logger.Info("gitshop.yaml already exists", "repo", fmt.Sprintf("%s/%s", owner, repo))
		}
		return &YAMLCreationResult{
			Created: false,
			Method:  "exists",
		}, nil
	}

	// File doesn't exist, create it
	yamlContent := fmt.Sprintf(DefaultGitShopYAML, shopName)

	// Try to commit directly to main/master branch
	defaultBranch, err := c.getDefaultBranch(ctx, client, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	// Try direct commit first
	commitResult, err := c.createFileDirectly(ctx, client, owner, repo, defaultBranch, yamlContent)
	if err == nil {
		if c.logger != nil {
			c.logger.Info("gitshop.yaml created via direct commit", "repo", fmt.Sprintf("%s/%s", owner, repo))
		}
		return &YAMLCreationResult{
			Created: true,
			Method:  "commit",
			URL:     commitResult,
		}, nil
	}

	// If direct commit failed (likely due to branch protection), try creating a PR
	if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "protect") {
		if c.logger != nil {
			c.logger.Info("Direct commit failed, trying PR approach", "repo", fmt.Sprintf("%s/%s", owner, repo), "error", err)
		}
		return c.createFileViaPR(ctx, client, owner, repo, defaultBranch, yamlContent, shopName)
	}

	return nil, fmt.Errorf("failed to create gitshop.yaml: %w", err)
}

func (c *Client) getDefaultBranch(ctx context.Context, client *github.Client, owner, repo string) (string, error) {
	repository, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "main", err // Default to main if we can't determine
	}
	if repository.DefaultBranch != nil {
		return *repository.DefaultBranch, nil
	}
	return "main", nil
}

func (c *Client) createFileDirectly(ctx context.Context, client *github.Client, owner, repo, branch, content string) (string, error) {
	message := "Initialize GitShop configuration"
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(content),
		Branch:  &branch,
	}

	_, _, err := client.Repositories.CreateFile(ctx, owner, repo, "gitshop.yaml", opts)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/gitshop.yaml", owner, repo, branch), nil
}

func (c *Client) createFileViaPR(ctx context.Context, client *github.Client, owner, repo, defaultBranch, yamlContent, shopName string) (*YAMLCreationResult, error) {
	// Create a new branch for the PR
	branchName := "gitshop/setup"

	// Get the SHA of the default branch
	ref, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to get ref: %w", err)
	}

	// Create new branch
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

	// Create the file on the new branch
	message := "Initialize GitShop configuration"
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(yamlContent),
		Branch:  &branchName,
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, "gitshop.yaml", opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create file on branch: %w", err)
	}

	// Create PR
	title := "Setup GitShop - Add gitshop.yaml"
	body := fmt.Sprintf("This PR initializes GitShop configuration for %s.\n\nPlease review and merge to start using GitShop.", shopName)
	pr := &github.NewPullRequest{
		Title: &title,
		Body:  &body,
		Head:  &branchName,
		Base:  &defaultBranch,
	}

	createdPR, _, err := client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	if c.logger != nil {
		c.logger.Info("gitshop.yaml created via PR", "repo", fmt.Sprintf("%s/%s", owner, repo), "pr_number", *createdPR.Number)
	}

	return &YAMLCreationResult{
		Created:  true,
		Method:   "pr",
		URL:      *createdPR.HTMLURL,
		PRNumber: *createdPR.Number,
	}, nil
}
