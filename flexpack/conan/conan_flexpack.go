package conan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// ConanFlexPack implements the FlexPackManager interface for Conan package manager.
// It handles dependency resolution, checksum calculation, and build info collection.
type ConanFlexPack struct {
	config          ConanConfig
	dependencies    []entities.Dependency
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	user            string
	channel         string
	conanfilePath   string
	graphData       *ConanGraphOutput
	requestedByMap  map[string][]string
	initialized     bool
}

// NewConanFlexPack creates a new Conan FlexPack instance.
// Initialization is deferred until first use (lazy initialization).
func NewConanFlexPack(config ConanConfig) (*ConanFlexPack, error) {
	cf := &ConanFlexPack{
		config:          config,
		dependencies:    []entities.Dependency{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}
	// Set default executable if not provided (same pattern as other package managers)
	if cf.config.ConanExecutable == "" {
		execPath, err := findConanExecutable()
		if err != nil {
			log.Warn("Conan executable not found in PATH, will try 'conan' command: " + err.Error())
			cf.config.ConanExecutable = "conan"
		} else {
			cf.config.ConanExecutable = execPath
		}
	}
	return cf, nil
}

// CollectBuildInfo collects complete build information for Conan project.
// This method only collects dependencies from the local Conan cache.
// Artifacts are collected separately during upload by jfrog-cli-artifactory.
func (cf *ConanFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	log.Debug("Starting Conan build info collection")
	if err := cf.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}
	conanVersion := cf.getConanVersion()
	buildInfo := &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "build-info-go",
			Version: "1.0.0",
		},
		BuildAgent: &entities.Agent{
			Name:    "Conan",
			Version: conanVersion,
		},
		Modules: []entities.Module{},
	}
	module := entities.Module{
		Id:           cf.getProjectRootId(),
		Type:         entities.Conan,
		Dependencies: cf.dependencies,
	}
	buildInfo.Modules = append(buildInfo.Modules, module)
	log.Debug(fmt.Sprintf("Collected %d dependencies for module %s", len(cf.dependencies), module.Id))
	return buildInfo, nil
}

// ensureInitialized loads conanfile and parses dependencies if not already done.
// This implements lazy initialization pattern - conanfile is only loaded when needed.
func (cf *ConanFlexPack) ensureInitialized() error {
	if cf.initialized {
		return nil
	}
	log.Debug("Initializing Conan FlexPack")
	if err := cf.loadConanfile(); err != nil {
		return fmt.Errorf("failed to load conanfile: %w", err)
	}
	if err := cf.parseDependencies(); err != nil {
		return fmt.Errorf("failed to parse dependencies: %w", err)
	}
	cf.initialized = true
	log.Debug(fmt.Sprintf("Initialized with project %s, %d dependencies", cf.projectName, len(cf.dependencies)))
	return nil
}

// loadConanfile loads either conanfile.py or conanfile.txt and extracts project metadata.
// Conanfile.py is preferred as it contains more metadata (name, version, user, channel).
func (cf *ConanFlexPack) loadConanfile() error {
	conanfilePy := filepath.Join(cf.config.WorkingDirectory, "conanfile.py")
	if _, err := os.Stat(conanfilePy); err == nil {
		cf.conanfilePath = conanfilePy
		return cf.extractProjectInfoFromConanfilePy()
	}
	conanfileTxt := filepath.Join(cf.config.WorkingDirectory, "conanfile.txt")
	if _, err := os.Stat(conanfileTxt); err == nil {
		cf.conanfilePath = conanfileTxt
		cf.projectName = filepath.Base(cf.config.WorkingDirectory)
		cf.projectVersion = ""
		cf.user = "_"
		cf.channel = "_"
		return nil
	}
	return fmt.Errorf("no conanfile.py or conanfile.txt found in %s", cf.config.WorkingDirectory)
}

// extractProjectInfoFromConanfilePy extracts project metadata from conanfile.py.
// Parses Python class attributes like: name = "mylib", version = "1.0.0"
func (cf *ConanFlexPack) extractProjectInfoFromConanfilePy() error {
	content, err := os.ReadFile(cf.conanfilePath)
	if err != nil {
		return err
	}
	contentStr := string(content)
	cf.projectName = extractPythonAttribute(contentStr, "name")
	if cf.projectName == "" {
		cf.projectName = filepath.Base(cf.config.WorkingDirectory)
	}
	// Version can be empty in Conan for consumer-only recipes (conanfile.txt style usage)
	cf.projectVersion = extractPythonAttribute(contentStr, "version")
	cf.user = extractPythonAttribute(contentStr, "user")
	if cf.user == "" {
		cf.user = "_"
	}
	cf.channel = extractPythonAttribute(contentStr, "channel")
	if cf.channel == "" {
		cf.channel = "_"
	}
	return nil
}

// extractPythonAttribute extracts a string attribute value from Python source code.
// Supports both single and double quoted strings.
// Uses strings.Index which returns the FIRST occurrence, so if there are duplicate
// definitions like name="mylib1" and name="mylib", the first one is returned.
// This matches Python's behavior where the first class attribute definition takes precedence.
// Example: For 'name = "mylib"' with attr="name", returns "mylib"
func extractPythonAttribute(content, attr string) string {
	// Try double quotes: attr = "value"
	pattern := attr + ` = "`
	if idx := strings.Index(content, pattern); idx != -1 {
		start := idx + len(pattern)
		if end := strings.Index(content[start:], `"`); end != -1 {
			return content[start : start+end]
		}
	}
	// Try single quotes: attr = 'value'
	pattern = attr + ` = '`
	if idx := strings.Index(content, pattern); idx != -1 {
		start := idx + len(pattern)
		if end := strings.Index(content[start:], `'`); end != -1 {
			return content[start : start+end]
		}
	}
	return ""
}

// getProjectRootId returns the project identifier for the root project.
// Format depends on whether user/channel and version are specified:
//   - With user/channel and version: "name/version@user/channel"
//   - Without user/channel but with version: "name:version"
//   - Without version: just "name" (for consumer-only recipes)
func (cf *ConanFlexPack) getProjectRootId() string {
	// Handle empty version case - Conan allows consumer-only recipes without version
	if cf.projectName == "" {
		return "unknown"
	}
	if cf.projectVersion == "" {
		return cf.projectName
	}
	if cf.user != "_" && cf.channel != "_" {
		return fmt.Sprintf("%s/%s@%s/%s", cf.projectName, cf.projectVersion, cf.user, cf.channel)
	}
	return fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion)
}

// findConanExecutable finds the Conan executable in PATH.
// Returns error if conan is not found, allowing caller to decide fallback behavior.
func findConanExecutable() (string, error) {
	path, err := exec.LookPath("conan")
	if err != nil {
		return "", fmt.Errorf("conan executable not found in PATH: %w", err)
	}
	return path, nil
}

// getConanVersion gets the Conan version for build info.
// Parses output from "conan --version" which returns: "Conan version X.Y.Z"
func (cf *ConanFlexPack) getConanVersion() string {
	cmd := exec.Command(cf.config.ConanExecutable, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	version := strings.TrimSpace(string(output))
	lines := strings.Split(version, "\n")
	if len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 3 {
			return fields[2]
		}
	}
	return "unknown"
}
