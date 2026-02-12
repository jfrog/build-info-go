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
		serverURL  string
		sha        string
		ref        string
		headRef    string
		expected   CIVcsInfo
	}{
		{
			name:       "Standard org/repo format with url revision branch",
			owner:      "jfrog",
			repository: "jfrog/jfrog-client-go",
			serverURL:  "https://github.com",
			sha:        "abc123",
			ref:        "refs/heads/main",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "jfrog",
				Repo:     "jfrog-client-go",
				Url:      "https://github.com/jfrog/jfrog-client-go",
				Revision: "abc123",
				Branch:   "main",
			},
		},
		{
			name:       "Pull request event - GITHUB_HEAD_REF takes precedence",
			owner:      "jfrog",
			repository: "jfrog/jfrog-client-go",
			serverURL:  "https://github.com",
			sha:        "def456",
			ref:        "refs/pull/42/merge",
			headRef:    "feature-branch-1",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "jfrog",
				Repo:     "jfrog-client-go",
				Url:      "https://github.com/jfrog/jfrog-client-go",
				Revision: "def456",
				Branch:   "feature-branch-1",
			},
		},
		{
			name:       "Push event - no GITHUB_HEAD_REF falls back to GITHUB_REF",
			owner:      "jfrog",
			repository: "jfrog/jfrog-client-go",
			serverURL:  "https://github.com",
			sha:        "abc123",
			ref:        "refs/heads/develop",
			headRef:    "",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "jfrog",
				Repo:     "jfrog-client-go",
				Url:      "https://github.com/jfrog/jfrog-client-go",
				Revision: "abc123",
				Branch:   "develop",
			},
		},
		{
			name:       "Tag event - no GITHUB_HEAD_REF uses raw ref",
			owner:      "jfrog",
			repository: "jfrog/jfrog-client-go",
			serverURL:  "https://github.com",
			sha:        "abc123",
			ref:        "refs/tags/v1.0.0",
			headRef:    "",
			expected: CIVcsInfo{
				Provider: GitHubProviderName,
				Org:      "jfrog",
				Repo:     "jfrog-client-go",
				Url:      "https://github.com/jfrog/jfrog-client-go",
				Revision: "abc123",
				Branch:   "refs/tags/v1.0.0",
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

			if tt.serverURL != "" {
				setEnvForTest(t, GitHubServerURLEnvVar, tt.serverURL)
			} else {
				unsetEnvForTest(t, GitHubServerURLEnvVar)
			}
			if tt.sha != "" {
				setEnvForTest(t, GitHubSHAEnvVar, tt.sha)
			} else {
				unsetEnvForTest(t, GitHubSHAEnvVar)
			}
			if tt.ref != "" {
				setEnvForTest(t, GitHubRefEnvVar, tt.ref)
			} else {
				unsetEnvForTest(t, GitHubRefEnvVar)
			}
			if tt.headRef != "" {
				setEnvForTest(t, GitHubHeadRefEnvVar, tt.headRef)
			} else {
				unsetEnvForTest(t, GitHubHeadRefEnvVar)
			}

			info := provider.GetVcsInfo()
			assert.Equal(t, tt.expected.Provider, info.Provider)
			assert.Equal(t, tt.expected.Org, info.Org)
			assert.Equal(t, tt.expected.Repo, info.Repo)
			assert.Equal(t, tt.expected.Url, info.Url)
			assert.Equal(t, tt.expected.Revision, info.Revision)
			assert.Equal(t, tt.expected.Branch, info.Branch)
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
	setEnvForTest(t, GitHubServerURLEnvVar, "https://github.com")
	setEnvForTest(t, GitHubSHAEnvVar, "abc123def")
	setEnvForTest(t, GitHubRefEnvVar, "refs/heads/main")
	// Ensure GITHUB_HEAD_REF is not set (simulating a push event, not PR)
	unsetEnvForTest(t, GitHubHeadRefEnvVar)

	// Should detect GitHub Actions
	assert.True(t, IsRunningInCI())

	provider := GetActiveProvider()
	assert.NotNil(t, provider)
	assert.Equal(t, GitHubProviderName, provider.Name())

	info := GetCIVcsInfo()
	assert.Equal(t, "github", info.Provider)
	assert.Equal(t, "jfrog", info.Org)
	assert.Equal(t, "jfrog-client-go", info.Repo)
	assert.Equal(t, "https://github.com/jfrog/jfrog-client-go", info.Url)
	assert.Equal(t, "abc123def", info.Revision)
	assert.Equal(t, "main", info.Branch)
}
