package cienv

import (
	"os"
	"strings"
)

const (
	// GitHub Actions environment variable names
	// Reference: https://docs.github.com/en/actions/learn-github-actions/environment-variables
	GitHubActionsEnvVar         = "GITHUB_ACTIONS"
	GitHubRepositoryEnvVar      = "GITHUB_REPOSITORY"
	GitHubRepositoryOwnerEnvVar = "GITHUB_REPOSITORY_OWNER"
	GitHubWorkflowEnvVar        = "GITHUB_WORKFLOW"
	GitHubRunIDEnvVar           = "GITHUB_RUN_ID"

	// Provider name constant
	GitHubProviderName = "github"
)

// GitHubActionsProvider implements CIProvider for GitHub Actions.
type GitHubActionsProvider struct{}

func init() {
	// Register GitHub Actions provider during package initialization
	RegisterProvider(&GitHubActionsProvider{})
}

// Name returns the provider identifier
func (g *GitHubActionsProvider) Name() string {
	return GitHubProviderName
}

// IsActive checks if running in GitHub Actions by verifying multiple environment variables.
// We check for GITHUB_ACTIONS=true plus the presence of GITHUB_WORKFLOW and GITHUB_RUN_ID
// to ensure we're truly in a GitHub Actions environment.
func (g *GitHubActionsProvider) IsActive() bool {
	if os.Getenv(GitHubActionsEnvVar) != "true" {
		return false
	}
	// Additional validation: these variables are always set in GitHub Actions
	return os.Getenv(GitHubWorkflowEnvVar) != "" && os.Getenv(GitHubRunIDEnvVar) != ""
}

// GetVcsInfo extracts VCS information from GitHub Actions environment variables.
// Uses GITHUB_REPOSITORY_OWNER for org and derives repo name from GITHUB_REPOSITORY.
func (g *GitHubActionsProvider) GetVcsInfo() CIVcsInfo {
	info := CIVcsInfo{
		Provider: GitHubProviderName,
	}

	// GITHUB_REPOSITORY_OWNER contains the owner/org directly
	info.Org = os.Getenv(GitHubRepositoryOwnerEnvVar)

	// GITHUB_REPOSITORY is "owner/repo" - extract just the repo name
	fullRepo := os.Getenv(GitHubRepositoryEnvVar)
	if fullRepo != "" && info.Org != "" {
		// Remove "owner/" prefix to get just the repo name
		prefix := info.Org + "/"
		info.Repo = strings.TrimPrefix(fullRepo, prefix)
	} else if fullRepo != "" {
		// Fallback: if owner is empty, use the full value
		info.Repo = fullRepo
	}

	return info
}
