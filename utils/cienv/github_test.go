package cienv

import (
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
			// Clear all relevant env vars for clean test state
			envVarsToRestore := []string{GitHubActionsEnvVar, GitHubWorkflowEnvVar, GitHubRunIDEnvVar}
			for _, key := range envVarsToRestore {
				unsetEnvForTest(t, key)
			}

			// Set test environment variables
			for key, val := range tt.envVars {
				setEnvForTest(t, key, val)
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
			if tt.owner != "" {
				setEnvForTest(t, GitHubRepositoryOwnerEnvVar, tt.owner)
			} else {
				unsetEnvForTest(t, GitHubRepositoryOwnerEnvVar)
			}

			if tt.repository != "" {
				setEnvForTest(t, GitHubRepositoryEnvVar, tt.repository)
			} else {
				unsetEnvForTest(t, GitHubRepositoryEnvVar)
			}

			info := provider.GetVcsInfo()
			assert.Equal(t, tt.expected.Provider, info.Provider)
			assert.Equal(t, tt.expected.Org, info.Org)
			assert.Equal(t, tt.expected.Repo, info.Repo)
		})
	}
}

func TestGitHubActionsIntegration(t *testing.T) {
	// Set all required env vars using helper
	setEnvForTest(t, CIEnvVar, "true")
	setEnvForTest(t, GitHubActionsEnvVar, "true")
	setEnvForTest(t, GitHubWorkflowEnvVar, "CI")
	setEnvForTest(t, GitHubRunIDEnvVar, "123456")
	setEnvForTest(t, GitHubRepositoryOwnerEnvVar, "jfrog")
	setEnvForTest(t, GitHubRepositoryEnvVar, "jfrog/jfrog-client-go")

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
