package flexpack

import (
	"os"

	"github.com/jfrog/build-info-go/entities"
)

// FlexPackManager defines the interface for flexible package manager support
// This interface allows different package managers (like Poetry) to implement
// standardized methods for dependency resolution and build info collection
type FlexPackManager interface {
	// GetDependency returns dependency information along with name and version
	// Returns a formatted string containing dependency details
	GetDependency() string

	// ParseDependencyToList parses and returns a list of dependencies with their name and version
	// Returns a slice of strings, each containing dependency name and version information
	ParseDependencyToList() []string

	// CalculateChecksum calculates checksums for dependencies in the provided list
	// Returns a slice of maps containing checksum information (sha1, sha256, md5) for each dependency
	CalculateChecksum() []map[string]interface{}

	// CalculateScopes calculates and returns the scopes for dependencies if any
	// Scopes represent different contexts where dependencies are used (e.g., runtime, compile, test)
	CalculateScopes() []string

	// CalculateRequestedBy determines which dependencies requested a particular package
	// Returns information about the dependency relationship hierarchy
	CalculateRequestedBy() map[string][]string
}

// DependencyInfo represents detailed information about a dependency
type DependencyInfo struct {
	Type         string           `json:"type"`
	SHA1         string           `json:"sha1"`
	SHA256       string           `json:"sha256"`
	MD5          string           `json:"md5"`
	ID           string           `json:"id"`
	Scopes       []string         `json:"scopes"`
	RequestedBy  []string         `json:"requestedBy,omitempty"`
	Version      string           `json:"version"`
	Name         string           `json:"name"`
	Path         string           `json:"path,omitempty"`
	Dependencies []DependencyInfo `json:"dependencies,omitempty"`
	IsDirect     bool             `json:"isDirect,omitempty"`
}

// BuildInfoCollector defines methods for collecting build information
type BuildInfoCollector interface {
	// CollectBuildInfo collects complete build information including dependencies
	CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error)

	// GetProjectDependencies returns all project dependencies with full details
	GetProjectDependencies() ([]DependencyInfo, error)

	// GetDependencyGraph returns the complete dependency graph showing relationships
	GetDependencyGraph() (map[string][]string, error)
}

// PoetryConfig holds configuration specific to Poetry operations
type PoetryConfig struct {
	// WorkingDirectory is the directory where Poetry should operate
	WorkingDirectory string

	// IncludeDevDependencies indicates whether to include development dependencies
	IncludeDevDependencies bool
}

// IsFlexPackEnabled checks if the FlexPack (native) implementation should be used
// Returns true if JFROG_RUN_NATIVE environment variable is set to "true"
func IsFlexPackEnabled() bool {
	return os.Getenv("JFROG_RUN_NATIVE") == "true"
}
