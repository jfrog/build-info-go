package cienv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockProvider is a test implementation of CIProvider
type mockProvider struct {
	name    string
	active  bool
	vcsInfo CIVcsInfo
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) IsActive() bool {
	return m.active
}

func (m *mockProvider) GetVcsInfo() CIVcsInfo {
	return m.vcsInfo
}

func TestCIVcsInfoIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		info     CIVcsInfo
		expected bool
	}{
		{
			name:     "Empty info",
			info:     CIVcsInfo{},
			expected: true,
		},
		{
			name:     "Only provider set",
			info:     CIVcsInfo{Provider: "github"},
			expected: false,
		},
		{
			name:     "Only org set",
			info:     CIVcsInfo{Org: "jfrog"},
			expected: false,
		},
		{
			name:     "Only repo set",
			info:     CIVcsInfo{Repo: "jfrog-client-go"},
			expected: false,
		},
		{
			name: "All fields set",
			info: CIVcsInfo{
				Provider: "github",
				Org:      "jfrog",
				Repo:     "jfrog-client-go",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.info.IsEmpty())
		})
	}
}

func TestProviderRegistry(t *testing.T) {
	// Clear any existing providers
	ClearProviders()
	defer ClearProviders()

	// Set CI=true for this test using helper
	setEnvForTest(t, CIEnvVar, "true")

	// Initially no providers
	assert.Empty(t, GetRegisteredProviders())
	assert.Nil(t, GetActiveProvider())
	assert.False(t, IsRunningInCI())

	// Register inactive provider
	inactiveProvider := &mockProvider{
		name:   "inactive",
		active: false,
	}
	RegisterProvider(inactiveProvider)
	assert.Len(t, GetRegisteredProviders(), 1)
	assert.Nil(t, GetActiveProvider())
	assert.False(t, IsRunningInCI())

	// Register active provider
	activeProvider := &mockProvider{
		name:   "active",
		active: true,
		vcsInfo: CIVcsInfo{
			Provider: "test",
			Org:      "testorg",
			Repo:     "testrepo",
		},
	}
	RegisterProvider(activeProvider)
	assert.Len(t, GetRegisteredProviders(), 2)
	assert.NotNil(t, GetActiveProvider())
	assert.Equal(t, "active", GetActiveProvider().Name())
	assert.True(t, IsRunningInCI())

	// Verify GetCIVcsInfo
	info := GetCIVcsInfo()
	assert.Equal(t, "test", info.Provider)
	assert.Equal(t, "testorg", info.Org)
	assert.Equal(t, "testrepo", info.Repo)
}

func TestCIEnvVarRequired(t *testing.T) {
	// Clear any existing providers
	ClearProviders()
	defer ClearProviders()

	// Ensure CI is not set using helper
	unsetEnvForTest(t, CIEnvVar)

	// Register an active provider
	activeProvider := &mockProvider{
		name:   "active",
		active: true,
	}
	RegisterProvider(activeProvider)

	// Without CI=true, GetActiveProvider should return nil
	assert.Nil(t, GetActiveProvider())
	assert.False(t, IsRunningInCI())

	// Set CI=true using helper
	setEnvForTest(t, CIEnvVar, "true")

	// Now it should return the provider
	assert.NotNil(t, GetActiveProvider())
	assert.True(t, IsRunningInCI())
}
