package conan

import (
	"encoding/json"
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
	cf.parseDependencies()
	cf.initialized = true
	log.Debug(fmt.Sprintf("Initialized with project %s, %d dependencies", cf.projectName, len(cf.dependencies)))
	return nil
}

// loadConanfile loads either conanfile.py or conanfile.txt and extracts project metadata.
// Conanfile.py is preferred as it contains more metadata (name, version, user, channel).
// It searches in RecipeFilePath first (if set), then falls back to WorkingDirectory.
// If no conanfile is found (e.g. when using --requires), it sets defaults gracefully
// instead of returning an error, so that dependency parsing can still proceed.
func (cf *ConanFlexPack) loadConanfile() error {
	searchDirs := cf.getConanfileSearchDirs()

	for _, dir := range searchDirs {
		conanfilePy := filepath.Join(dir, "conanfile.py")
		if _, err := os.Stat(conanfilePy); err == nil {
			cf.conanfilePath = conanfilePy
			if err := cf.extractProjectInfoFromConanfilePy(); err != nil {
				return err
			}
			cf.applyOverrides()
			return nil
		}
		conanfileTxt := filepath.Join(dir, "conanfile.txt")
		if _, err := os.Stat(conanfileTxt); err == nil {
			cf.conanfilePath = conanfileTxt
			cf.projectName = filepath.Base(dir)
			cf.projectVersion = ""
			cf.user = "_"
			cf.channel = "_"
			cf.applyOverrides()
			return nil
		}
	}

	// No conanfile found. This is valid for Conan 2.x commands using --requires
	// (e.g. conan install --requires zlib/1.2.11). Set defaults and continue.
	log.Debug("No conanfile.py or conanfile.txt found, using defaults (--requires mode)")
	cf.projectName = filepath.Base(cf.config.WorkingDirectory)
	cf.projectVersion = ""
	cf.user = "_"
	cf.channel = "_"
	cf.applyOverrides()
	return nil
}

// getConanfileSearchDirs returns the directories to search for conanfile, in priority order.
// RecipeFilePath takes precedence over WorkingDirectory.
func (cf *ConanFlexPack) getConanfileSearchDirs() []string {
	var dirs []string
	if cf.config.RecipeFilePath != "" {
		recipeDir := cf.config.RecipeFilePath
		if info, err := os.Stat(recipeDir); err == nil && !info.IsDir() {
			recipeDir = filepath.Dir(recipeDir)
		}
		dirs = append(dirs, recipeDir)
	}
	if cf.config.WorkingDirectory != "" {
		dirs = append(dirs, cf.config.WorkingDirectory)
	}
	return dirs
}

// getRecipeDir returns the directory containing the conanfile (used for running Conan commands).
// If conanfilePath is already resolved, returns its parent directory.
// Otherwise falls back to RecipeFilePath, then WorkingDirectory.
func (cf *ConanFlexPack) getRecipeDir() string {
	if cf.conanfilePath != "" {
		return filepath.Dir(cf.conanfilePath)
	}
	target := cf.config.RecipeFilePath
	if target == "" {
		return cf.config.WorkingDirectory
	}

	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		return filepath.Dir(target)
	}
	return target
}

// applyOverrides applies CLI-provided overrides (from --name, --version, --user, --channel flags).
func (cf *ConanFlexPack) applyOverrides() {
	if cf.config.ProjectNameOverride != "" {
		cf.projectName = cf.config.ProjectNameOverride
	}
	if cf.config.ProjectVersionOverride != "" {
		cf.projectVersion = cf.config.ProjectVersionOverride
	}
	if cf.config.UserOverride != "" {
		cf.user = cf.config.UserOverride
	}
	if cf.config.ChannelOverride != "" {
		cf.channel = cf.config.ChannelOverride
	}
}

// extractProjectInfoFromConanfilePy extracts project metadata from conanfile.py.
// Uses 'conan inspect . --format=json' which is more reliable than parsing Python source.
// Falls back to regex parsing if the inspect command fails.
func (cf *ConanFlexPack) extractProjectInfoFromConanfilePy() error {
	// Try using conan inspect first (preferred method)
	if err := cf.extractProjectInfoUsingConanInspect(); err == nil {
		log.Debug("Successfully extracted project info using 'conan inspect'")
		return nil
	} else {
		log.Debug("Conan inspect failed, falling back to regex parsing: " + err.Error())
	}
	// Fallback to regex parsing for older Conan versions or edge cases
	return cf.extractProjectInfoByParsingPython()
}

// extractProjectInfoUsingConanInspect uses 'conan inspect . --format=json' to get project metadata.
// This is the preferred method as it uses Conan's own parser and handles all edge cases.
func (cf *ConanFlexPack) extractProjectInfoUsingConanInspect() error {
	cmd := exec.Command(cf.config.ConanExecutable, "inspect", cf.config.WorkingDirectory, "--format=json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("conan inspect failed: %w", err)
	}
	var inspectOutput ConanInspectOutput
	if err := json.Unmarshal(output, &inspectOutput); err != nil {
		return fmt.Errorf("failed to parse conan inspect output: %w", err)
	}
	cf.projectName = inspectOutput.Name
	if cf.projectName == "" {
		cf.projectName = filepath.Base(cf.config.WorkingDirectory)
	}
	cf.projectVersion = inspectOutput.Version
	cf.user = inspectOutput.User
	if cf.user == "" {
		cf.user = "_"
	}
	cf.channel = inspectOutput.Channel
	if cf.channel == "" {
		cf.channel = "_"
	}
	log.Debug(fmt.Sprintf("Conan inspect: name=%s, version=%s, user=%s, channel=%s",
		cf.projectName, cf.projectVersion, cf.user, cf.channel))
	return nil
}

// extractProjectInfoByParsingPython extracts project info by parsing Python source code.
// This is a fallback method when 'conan inspect' is not available or fails.
func (cf *ConanFlexPack) extractProjectInfoByParsingPython() error {
	content, err := os.ReadFile(cf.conanfilePath)
	if err != nil {
		return err
	}
	contentStr := string(content)
	cf.projectName = extractPythonAttribute(contentStr, "name")
	if cf.projectName == "" {
		cf.projectName = filepath.Base(cf.config.WorkingDirectory)
	}
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
