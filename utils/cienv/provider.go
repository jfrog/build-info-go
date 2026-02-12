// Package cienv provides CI environment detection and VCS information extraction.
//
// This package implements a plugin-based architecture for detecting CI environments.
// Multiple providers can be registered (GitHub Actions, GitLab CI, etc.), but only
// ONE provider will ever be active at runtime since a binary can only execute in
// a single CI environment at any given time.
//
// CI detection requires CI=true environment variable to be set (standard across
// most CI systems), plus provider-specific environment variables.
//
// # Adding a New CI Provider
//
// To add support for a new CI provider (e.g., Azure DevOps, Jenkins):
//
//  1. Create a new file (e.g., azure.go)
//
//  2. Implement the CIProvider interface
//
//  3. Register the provider in init():
//
//     func init() {
//     RegisterProvider(&AzureDevOpsProvider{})
//     }
//
// The provider will be automatically available - no other code changes needed.
package cienv

import "os"

const (
	// CIEnvVar is the standard environment variable set by most CI systems
	CIEnvVar = "CI"
)

// CIVcsInfo contains VCS information extracted from CI environment variables.
// This is used to populate vcs.provider, vcs.org, vcs.repo, vcs.url, vcs.revision,
// and vcs.branch properties on uploaded artifacts and in build info.
type CIVcsInfo struct {
	// Provider is the VCS provider name (e.g., "github", "gitlab", "bitbucket")
	Provider string
	// Org is the organization or owner name
	Org string
	// Repo is the repository name (without the org prefix)
	Repo string
	// Url is the repository URL (server_url + repo)
	Url string
	// Revision is the commit SHA
	Revision string
	// Branch is the branch name
	Branch string
}

// IsEmpty returns true if no VCS info was detected
func (v CIVcsInfo) IsEmpty() bool {
	return v.Provider == "" && v.Org == "" && v.Repo == ""
}

// CIProvider interface defines the contract for CI environment detection.
// Implement this interface to add support for new CI providers.
//
// Note: At runtime, only one provider can be active since a binary executes
// in a single CI environment. The registry pattern allows supporting multiple
// CI systems without code changes - just add the provider implementation.
type CIProvider interface {
	// Name returns the unique identifier for this CI provider
	Name() string

	// IsActive returns true if currently running in this CI environment.
	// Only one provider should return true at any given time.
	IsActive() bool

	// GetVcsInfo extracts VCS information from CI environment variables
	GetVcsInfo() CIVcsInfo
}

// providers holds all supported CI providers.
// Registration happens in init() (sequential), and the slice is read-only after that.
// Only one provider will be active at runtime since a binary executes in a single CI environment.
var providers []CIProvider

// RegisterProvider adds a CI provider to the registry.
// Must be called from init() functions only.
func RegisterProvider(p CIProvider) {
	providers = append(providers, p)
}

// GetActiveProvider returns the active CI provider, or nil if not running in any
// supported CI environment. Requires CI=true environment variable to be set,
// plus provider-specific environment variables.
func GetActiveProvider() CIProvider {
	// First check if CI=true (standard across most CI systems)
	if os.Getenv(CIEnvVar) != "true" {
		return nil
	}
	// Then find the specific provider
	for _, p := range providers {
		if p.IsActive() {
			return p
		}
	}
	return nil
}

// GetCIVcsInfo returns VCS information from the active CI provider.
// Returns empty CIVcsInfo if no CI environment is detected.
func GetCIVcsInfo() CIVcsInfo {
	provider := GetActiveProvider()
	if provider == nil {
		return CIVcsInfo{}
	}
	return provider.GetVcsInfo()
}

// IsRunningInCI returns true if running in any supported CI environment.
func IsRunningInCI() bool {
	return GetActiveProvider() != nil
}

// GetRegisteredProviders returns all registered providers.
// Useful for debugging and testing.
func GetRegisteredProviders() []CIProvider {
	return providers
}

// ClearProviders removes all registered providers.
// Intended for testing purposes only.
func ClearProviders() {
	providers = nil
}
