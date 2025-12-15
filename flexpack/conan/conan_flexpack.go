package conan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

// DependencyInfo is an alias for flexpack.DependencyInfo
type DependencyInfo = flexpack.DependencyInfo

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
// Note: Conanfile loading is deferred to first dependency parse for lazy initialization.
func NewConanFlexPack(config ConanConfig) (*ConanFlexPack, error) {
	cf := &ConanFlexPack{
		config:          config,
		dependencies:    []entities.Dependency{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}

	// Set default executable if not provided
	if cf.config.ConanExecutable == "" {
		cf.config.ConanExecutable = cf.getConanExecutablePath()
	}

	return cf, nil
}

// GetDependency returns dependency information as a formatted string
func (cf *ConanFlexPack) GetDependency() string {
	if err := cf.ensureInitialized(); err != nil {
		log.Warn("Failed to initialize: " + err.Error())
		return ""
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Project: %s:%s\n", cf.projectName, cf.projectVersion))
	result.WriteString("Dependencies:\n")

	for _, dep := range cf.dependencies {
		result.WriteString(fmt.Sprintf("  - %s [conan]\n", dep.Id))
	}

	return result.String()
}

// ParseDependencyToList converts parsed dependencies to a list format
func (cf *ConanFlexPack) ParseDependencyToList() []string {
	if err := cf.ensureInitialized(); err != nil {
		log.Warn("Failed to initialize: " + err.Error())
		return nil
	}

	var depList []string
	for _, dep := range cf.dependencies {
		depList = append(depList, dep.Id)
	}

	return depList
}

// CalculateScopes returns unique scopes from dependencies in standard order.
// Conan supports the following scopes:
//   - runtime: Regular dependencies (requires)
//   - build: Build-time dependencies (build_requires, tool_requires)
//   - test: Test dependencies (test_requires)
//   - python: Python extension dependencies (python_requires)
func (cf *ConanFlexPack) CalculateScopes() []string {
	if err := cf.ensureInitialized(); err != nil {
		return nil
	}

	scopesMap := make(map[string]bool)
	for _, dep := range cf.dependencies {
		for _, scope := range dep.Scopes {
			scopesMap[scope] = true
		}
	}

	// Return scopes in Conan standard order
	var orderedScopes []string
	conanScopeOrder := []string{"runtime", "build", "test", "python"}
	for _, scope := range conanScopeOrder {
		if scopesMap[scope] {
			orderedScopes = append(orderedScopes, scope)
		}
	}

	return orderedScopes
}

// CollectBuildInfo collects complete build information for Conan project.
// Note: Artifacts are NOT collected here - they are collected during upload
// by jfrog-cli-artifactory when artifacts are uploaded to Artifactory.
// This method only collects dependencies from the local Conan cache.
func (cf *ConanFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	log.Debug("Starting Conan build info collection...")

	if err := cf.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}

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
			Version: cf.getConanVersion(),
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

// GetProjectDependencies returns all project dependencies with full details
func (cf *ConanFlexPack) GetProjectDependencies() ([]entities.Dependency, error) {
	if err := cf.ensureInitialized(); err != nil {
		return nil, err
	}
	return cf.dependencies, nil
}

// GetDependencyGraph returns the complete dependency graph
func (cf *ConanFlexPack) GetDependencyGraph() (map[string][]string, error) {
	if err := cf.ensureInitialized(); err != nil {
		return nil, err
	}
	return cf.dependencyGraph, nil
}

// ensureInitialized loads conanfile and parses dependencies if not already done.
// This implements lazy initialization - conanfile is only loaded when needed.
func (cf *ConanFlexPack) ensureInitialized() error {
	if cf.initialized {
		return nil
	}

	log.Debug("Initializing Conan FlexPack...")

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
	// Check for conanfile.py first (preferred in Conan)
	conanfilePy := filepath.Join(cf.config.WorkingDirectory, "conanfile.py")
	if _, err := os.Stat(conanfilePy); err == nil {
		cf.conanfilePath = conanfilePy
		return cf.extractProjectInfoFromConanfilePy()
	}

	// Fallback to conanfile.txt
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

	cf.projectName = cf.extractPythonAttribute(contentStr, "name")
	if cf.projectName == "" {
		cf.projectName = filepath.Base(cf.config.WorkingDirectory)
	}

	cf.projectVersion = cf.extractPythonAttribute(contentStr, "version")

	cf.user = cf.extractPythonAttribute(contentStr, "user")
	if cf.user == "" {
		cf.user = "_"
	}

	cf.channel = cf.extractPythonAttribute(contentStr, "channel")
	if cf.channel == "" {
		cf.channel = "_"
	}

	return nil
}

// extractPythonAttribute extracts a string attribute value from Python source code.
// Supports both single and double quoted strings.
// Example: For 'name = "mylib"' with attr="name", returns "mylib"
func (cf *ConanFlexPack) extractPythonAttribute(content, attr string) string {
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
// Format depends on whether user/channel are specified:
//   - With user/channel: "name/version@user/channel"
//   - Without: "name:version"
func (cf *ConanFlexPack) getProjectRootId() string {
	if cf.projectName == "" || cf.projectVersion == "" {
		return cf.projectName
	}

	if cf.user != "_" && cf.channel != "_" {
		return fmt.Sprintf("%s/%s@%s/%s", cf.projectName, cf.projectVersion, cf.user, cf.channel)
	}

	return fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion)
}

// getConanExecutablePath finds the Conan executable in PATH
func (cf *ConanFlexPack) getConanExecutablePath() string {
	if path, err := exec.LookPath("conan"); err == nil {
		return path
	}
	return "conan"
}

// getConanVersion gets the Conan version for build info.
// Parses output from "conan --version" which returns: "Conan version X.Y.Z"
func (cf *ConanFlexPack) getConanVersion() string {
	cmd := exec.Command(cf.config.ConanExecutable, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	// Parse "Conan version X.Y.Z" format
	// Split by whitespace and take the version number (3rd field)
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

// SaveConanBuildInfoForJfrogCli saves build info in a format compatible with jfrog-cli.
// This allows 'jf rt bp' command to publish the collected build info.
func SaveConanBuildInfoForJfrogCli(buildInfo *entities.BuildInfo) error {
	log.Debug(fmt.Sprintf("Saving Conan build info: %s/%s", buildInfo.Name, buildInfo.Number))

	buildInfoService := build.NewBuildInfoService()

	buildInstance, err := buildInfoService.GetOrCreateBuildWithProject(
		buildInfo.Name,
		buildInfo.Number,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to get or create build: %w", err)
	}

	if err := buildInstance.SaveBuildInfo(buildInfo); err != nil {
		return fmt.Errorf("failed to save build info: %w", err)
	}

	log.Debug("Successfully saved Conan build info for jfrog-cli")
	return nil
}
