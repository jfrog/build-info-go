package cienv

import (
	"os"
	"strings"
)

const (
	// GitLab CI environment variable names
	// Reference: https://docs.gitlab.com/ee/ci/variables/predefined_variables.html
	GitLabCIEnvVar          = "GITLAB_CI"
	GitLabProjectPathEnvVar = "CI_PROJECT_PATH"
	GitLabPipelineIDEnvVar  = "CI_PIPELINE_ID"
	GitLabJobIDEnvVar       = "CI_JOB_ID"

	// Provider name constant
	GitLabProviderName = "gitlab"
)

// GitLabCIProvider implements CIProvider for GitLab CI.
type GitLabCIProvider struct{}

func init() {
	// Register GitLab CI provider during package initialization
	RegisterProvider(&GitLabCIProvider{})
}

// Name returns the provider identifier
func (g *GitLabCIProvider) Name() string {
	return GitLabProviderName
}

// IsActive checks if running in GitLab CI by verifying multiple environment variables.
// We check for GITLAB_CI=true plus the presence of CI_PIPELINE_ID and CI_JOB_ID
// to ensure we're truly in a GitLab CI environment.
func (g *GitLabCIProvider) IsActive() bool {
	if os.Getenv(GitLabCIEnvVar) != "true" {
		return false
	}
	// Additional validation: these variables are always set in GitLab CI
	return os.Getenv(GitLabPipelineIDEnvVar) != "" && os.Getenv(GitLabJobIDEnvVar) != ""
}

// GetVcsInfo extracts VCS information from GitLab CI environment variables.
// CI_PROJECT_PATH is in the format "group/subgroup/project" or "user/project"
func (g *GitLabCIProvider) GetVcsInfo() CIVcsInfo {
	info := CIVcsInfo{
		Provider: GitLabProviderName,
	}

	// Parse CI_PROJECT_PATH (format: "group/project" or "group/subgroup/project")
	projectPath := os.Getenv(GitLabProjectPathEnvVar)
	if projectPath != "" {
		parts := strings.SplitN(projectPath, "/", 2)
		if len(parts) == 2 {
			info.Org = parts[0]
			info.Repo = parts[1]
		} else if len(parts) == 1 {
			// Edge case: just project name without group
			info.Repo = parts[0]
		}
	}

	return info
}
