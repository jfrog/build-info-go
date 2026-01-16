package cienv

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGitHubActionsProvider_Name(t *testing.T) {
	provider := &GitHubActionsProvider{}
	assert.Equal(t, GitHubProviderName, provider.Name())
}

func TestGitHubActionsProvider_IsActive(t *testing.T) {
	provider := &GitHubActionsProvider{}

	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "Not in GitHub Actions - empty",
			envVars:  map[string]string{},
			expected: false,
		},
		{
			name: "In GitHub Actions - all vars set",
			envVars: map[string]string{
				GitHubActionsEnvVar:  "true",
				GitHubWorkflowEnvVar: "CI",
				GitHubRunIDEnvVar:    "123456",
			},
			expected: true,
		},
		{
			name: "Not in GitHub Actions - GITHUB_ACTIONS=false",
			envVars: map[string]string{
				GitHubActionsEnvVar:  "false",
				GitHubWorkflowEnvVar: "CI",
				GitHubRunIDEnvVar:    "123456",
			},
			expected: false,
		},
		{
			name: "Not in GitHub Actions - missing GITHUB_WORKFLOW",
			envVars: map[string]string{
				GitHubActionsEnvVar: "true",
				GitHubRunIDEnvVar:   "123456",
			},
			expected: false,
		},
		{
			name: "Not in GitHub Actions - only GITHUB_ACTIONS set",
			envVars: map[string]string{
				GitHubActionsEnvVar: "true",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original values
			envVarsToRestore := []string{GitHubActionsEnvVar, GitHubWorkflowEnvVar, GitHubRunIDEnvVar}
			originals := make(map[string]string)
			for _, key := range envVarsToRestore {
				originals[key] = os.Getenv(key)
				os.Unsetenv(key)
			}
			defer func() {
				for key, val := range originals {
					if val == "" {
						os.Unsetenv(key)
					} else {
						os.Setenv(key, val)
					}
				}
			}()

			// Set test environment variables
			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			assert.Equal(t, tt.expected, provider.IsActive())
		})
	}
}

func TestGitHubActionsProvider_GetVcsInfo(t *testing.T) {
	provider := &GitHubActionsProvider{}

	tests := []struct {
		name       string
		owner      string
		repository string
		expected   CIVcsInfo
	}{
		{
			name:       "Standard org/repo format",
			owner:      "jfrog",
			repository: "jfrog/jfrog-client-go",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "jfrog",
				Repo:     "jfrog-client-go",
			},
		},
		{
			name:       "User repo format",
			owner:      "username",
			repository: "username/my-project",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "username",
				Repo:     "my-project",
			},
		},
		{
			name:       "Empty values",
			owner:      "",
			repository: "",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "",
				Repo:     "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original values
			origOwner := os.Getenv(GitHubRepositoryOwnerEnvVar)
			origRepo := os.Getenv(GitHubRepositoryEnvVar)
			defer func() {
				os.Setenv(GitHubRepositoryOwnerEnvVar, origOwner)
				os.Setenv(GitHubRepositoryEnvVar, origRepo)
			}()

			if tt.owner == "" {
				os.Unsetenv(GitHubRepositoryOwnerEnvVar)
			} else {
				os.Setenv(GitHubRepositoryOwnerEnvVar, tt.owner)
			}

			if tt.repository == "" {
				os.Unsetenv(GitHubRepositoryEnvVar)
			} else {
				os.Setenv(GitHubRepositoryEnvVar, tt.repository)
			}

			info := provider.GetVcsInfo()
			assert.Equal(t, tt.expected.Provider, info.Provider)
			assert.Equal(t, tt.expected.Org, info.Org)
			assert.Equal(t, tt.expected.Repo, info.Repo)
		})
	}
}

func TestGitHubActionsIntegration(t *testing.T) {
	// Test full integration: CI=true + GitHub env vars
	origCI := os.Getenv(CIEnvVar)
	origActions := os.Getenv(GitHubActionsEnvVar)
	origWorkflow := os.Getenv(GitHubWorkflowEnvVar)
	origRunID := os.Getenv(GitHubRunIDEnvVar)
	origOwner := os.Getenv(GitHubRepositoryOwnerEnvVar)
	origRepo := os.Getenv(GitHubRepositoryEnvVar)

	defer func() {
		os.Setenv(CIEnvVar, origCI)
		os.Setenv(GitHubActionsEnvVar, origActions)
		os.Setenv(GitHubWorkflowEnvVar, origWorkflow)
		os.Setenv(GitHubRunIDEnvVar, origRunID)
		os.Setenv(GitHubRepositoryOwnerEnvVar, origOwner)
		os.Setenv(GitHubRepositoryEnvVar, origRepo)
	}()

	// Set all required env vars
	os.Setenv(CIEnvVar, "true")
	os.Setenv(GitHubActionsEnvVar, "true")
	os.Setenv(GitHubWorkflowEnvVar, "CI")
	os.Setenv(GitHubRunIDEnvVar, "123456")
	os.Setenv(GitHubRepositoryOwnerEnvVar, "jfrog")
	os.Setenv(GitHubRepositoryEnvVar, "jfrog/jfrog-client-go")

	// Should detect GitHub Actions
	assert.True(t, IsRunningInCI())

	provider := GetActiveProvider()
	assert.NotNil(t, provider)
	assert.Equal(t, GitHubProviderName, provider.Name())

	info := GetCIVcsInfo()
	assert.Equal(t, "github", info.Provider)
	assert.Equal(t, "jfrog", info.Org)
	assert.Equal(t, "jfrog-client-go", info.Repo)
}
