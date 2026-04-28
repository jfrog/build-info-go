package flexpack

import (
	"os"
	"strconv"
	"time"

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
	// Returns information about the dependency relationship hierarchy.
	CalculateRequestedBy() map[string][]string
}

// DependencyInfo represents detailed information about a dependency
type DependencyInfo struct {
	Type         string           `json:"type"`
	SHA1         string           `json:"sha1"`
	SHA256       string           `json:"sha256"`
	MD5          string           `json:"md5"`
	ID           string           `json:"id"`
	Scopes       []string         `json:"scopes,omitempty"`
	RequestedBy  []string         `json:"requestedBy,omitempty"`
	Version      string           `json:"version"`
	Name         string           `json:"name"`
	Path         string           `json:"path,omitempty"`
	Repository   string           `json:"-"`
	Dependencies []DependencyInfo `json:"dependencies,omitempty"`
	// DirectURL is set for packages installed from a direct URL (not a registry).
	// These packages are not in Artifactory so sha1/md5 enrichment is skipped.
	DirectURL string `json:"-"`
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

// UVConfig holds configuration specific to UV operations
type UVConfig struct {
	// WorkingDirectory is the directory where UV should operate
	WorkingDirectory string

	// IncludeDevDependencies indicates whether to include development dependencies.
	// Only used when InstalledPackages is nil (e.g. for lock/build commands where no
	// venv is available). When InstalledPackages is set this field is ignored.
	IncludeDevDependencies bool

	// InstalledPackages is the ground-truth set of what uv actually installed,
	// keyed by normalised package name (lowercase, hyphens) → version string.
	// When non-nil, only packages present in this map are included in build-info,
	// which correctly handles --no-dev, --only-dev, --group, --no-group and all
	// other uv sync flag combinations without any flag parsing on our side.
	InstalledPackages map[string]string

	// LockFilePath overrides the default uv.lock path. Used for PEP 723 inline scripts
	// where the lock file is adjacent to the script (e.g. myscript.py.lock).
	LockFilePath string

	// ProjectName and ProjectVersion override the values read from pyproject.toml.
	// Set these when there is no pyproject.toml (e.g. for standalone PEP 723 scripts).
	ProjectName    string
	ProjectVersion string
}

// GradleConfig holds configuration specific to Gradle operations
type GradleConfig struct {
	// WorkingDirectory is the directory where Gradle should operate
	WorkingDirectory string

	// IncludeTestDependencies indicates whether to include test dependencies
	IncludeTestDependencies bool

	// GradleExecutable is the path to the Gradle executable (optional, will be auto-detected)
	GradleExecutable string

	// CommandTimeout is the maximum duration for Gradle commands (optional, defaults to 10 minutes)
	CommandTimeout time.Duration
}

// IsFlexPackEnabled checks if the FlexPack (native) implementation should be used
// Returns true if JFROG_RUN_NATIVE environment variable is set to "true"
func IsFlexPackEnabled() bool {
	value, err := strconv.ParseBool(os.Getenv("JFROG_RUN_NATIVE"))
	if err != nil {
		// Invalid value or not set - default to false (legacy)
		return false
	}
	return value
}
