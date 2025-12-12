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

// ConanFlexPack implements the FlexPackManager interface for Conan package manager
type ConanFlexPack struct {
	config          ConanConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	user            string
	channel         string
	conanfilePath   string
	graphData       *ConanGraphOutput
	requestedByMap  map[string][]string
}

// NewConanFlexPack creates a new Conan FlexPack instance
func NewConanFlexPack(config ConanConfig) (*ConanFlexPack, error) {
	cf := &ConanFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}

	if cf.config.ConanExecutable == "" {
		cf.config.ConanExecutable = cf.getConanExecutablePath()
	}

	if err := cf.loadConanfile(); err != nil {
		return nil, fmt.Errorf("failed to load conanfile: %w", err)
	}

	return cf, nil
}

// GetDependency returns dependency information as a formatted string
func (cf *ConanFlexPack) GetDependency() string {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Project: %s:%s\n", cf.projectName, cf.projectVersion))
	result.WriteString("Dependencies:\n")

	for _, dep := range cf.dependencies {
		result.WriteString(fmt.Sprintf("  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type))
	}

	return result.String()
}

// ParseDependencyToList converts parsed dependencies to a list format
func (cf *ConanFlexPack) ParseDependencyToList() []string {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	var depList []string
	for _, dep := range cf.dependencies {
		depList = append(depList, formatDependencyKey(dep.Name, dep.Version))
	}

	return depList
}

// CalculateChecksum calculates checksums for dependencies
func (cf *ConanFlexPack) CalculateChecksum() []map[string]interface{} {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	var checksums []map[string]interface{}
	for _, dep := range cf.dependencies {
		if checksumMap := cf.calculateChecksumWithFallback(dep); checksumMap != nil {
			checksums = append(checksums, checksumMap)
		}
	}

	if checksums == nil {
		checksums = []map[string]interface{}{}
	}

	return checksums
}

// CalculateScopes returns unique scopes from dependencies in standard order
func (cf *ConanFlexPack) CalculateScopes() []string {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	scopesMap := make(map[string]bool)
	for _, dep := range cf.dependencies {
		for _, scope := range dep.Scopes {
			scopesMap[scope] = true
		}
	}

	var orderedScopes []string
	conanScopeOrder := []string{"runtime", "build", "test", "python"}
	for _, scope := range conanScopeOrder {
		if scopesMap[scope] {
			orderedScopes = append(orderedScopes, scope)
		}
	}

	return orderedScopes
}

// CalculateRequestedBy determines which dependencies requested a particular package
func (cf *ConanFlexPack) CalculateRequestedBy() map[string][]string {
	if cf.requestedByMap == nil {
		cf.requestedByMap = make(map[string][]string)
	}

	if len(cf.requestedByMap) == 0 && cf.graphData != nil {
		cf.buildRequestedByMapFromGraph()
	}

	return cf.requestedByMap
}

// CollectBuildInfo collects complete build information for Conan project
func (cf *ConanFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	dependencies := cf.buildDependencyEntities()

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
		Dependencies: dependencies,
	}
	buildInfo.Modules = append(buildInfo.Modules, module)

	if vcsInfo := cf.collectVcsInfo(); vcsInfo != nil {
		buildInfo.VcsList = append(buildInfo.VcsList, *vcsInfo)
	}

	return buildInfo, nil
}

// buildDependencyEntities converts internal dependencies to entities
func (cf *ConanFlexPack) buildDependencyEntities() []entities.Dependency {
	requestedByMap := cf.CalculateRequestedBy()
	var dependencies []entities.Dependency

	for _, dep := range cf.dependencies {
		checksum := cf.getChecksumForDependency(dep)

		entity := entities.Dependency{
			Id:       dep.ID,
			Type:     dep.Type,
			Scopes:   dep.Scopes,
			Checksum: checksum,
		}

		if requesters, exists := requestedByMap[dep.ID]; exists && len(requesters) > 0 {
			entity.RequestedBy = [][]string{requesters}
		}

		dependencies = append(dependencies, entity)
	}

	return dependencies
}

// getChecksumForDependency calculates checksum for a single dependency
func (cf *ConanFlexPack) getChecksumForDependency(dep DependencyInfo) entities.Checksum {
	checksumMap := cf.calculateChecksumWithFallback(dep)
	if checksumMap == nil {
		return entities.Checksum{}
	}

	checksum := entities.Checksum{}
	if sha1, ok := checksumMap["sha1"].(string); ok {
		checksum.Sha1 = sha1
	}
	if sha256, ok := checksumMap["sha256"].(string); ok {
		checksum.Sha256 = sha256
	}
	if md5, ok := checksumMap["md5"].(string); ok {
		checksum.Md5 = md5
	}

	return checksum
}

// GetProjectDependencies returns all project dependencies with full details
func (cf *ConanFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	requestedBy := cf.CalculateRequestedBy()
	for i, dep := range cf.dependencies {
		if parents, exists := requestedBy[dep.ID]; exists {
			cf.dependencies[i].RequestedBy = parents
		}
	}

	return cf.dependencies, nil
}

// GetDependencyGraph returns the complete dependency graph
func (cf *ConanFlexPack) GetDependencyGraph() (map[string][]string, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}
	return cf.dependencyGraph, nil
}

// loadConanfile loads either conanfile.py or conanfile.txt
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

// extractProjectInfoFromConanfilePy extracts project name and version from conanfile.py
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

// extractPythonAttribute extracts a string attribute from Python source code
func (cf *ConanFlexPack) extractPythonAttribute(content, attr string) string {
	// Try double quotes
	pattern := attr + ` = "`
	if idx := strings.Index(content, pattern); idx != -1 {
		start := idx + len(pattern)
		if end := strings.Index(content[start:], `"`); end != -1 {
			return content[start : start+end]
		}
	}

	// Try single quotes
	pattern = attr + ` = '`
	if idx := strings.Index(content, pattern); idx != -1 {
		start := idx + len(pattern)
		if end := strings.Index(content[start:], `'`); end != -1 {
			return content[start : start+end]
		}
	}

	return ""
}

// getProjectRootId returns the project identifier for the root project
func (cf *ConanFlexPack) getProjectRootId() string {
	if cf.projectName == "" || cf.projectVersion == "" {
		return cf.projectName
	}

	if cf.user != "_" && cf.channel != "_" {
		return fmt.Sprintf("%s/%s@%s/%s", cf.projectName, cf.projectVersion, cf.user, cf.channel)
	}

	return formatDependencyKey(cf.projectName, cf.projectVersion)
}

// getConanExecutablePath gets the Conan executable path
func (cf *ConanFlexPack) getConanExecutablePath() string {
	if path, err := exec.LookPath("conan"); err == nil {
		return path
	}
	return "conan"
}

// getConanVersion gets the Conan version for build info
func (cf *ConanFlexPack) getConanVersion() string {
	cmd := exec.Command(cf.config.ConanExecutable, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	version := strings.TrimSpace(string(output))
	lines := strings.Split(version, "\n")

	if len(lines) > 0 {
		if parts := strings.Fields(lines[0]); len(parts) >= 3 {
			return parts[2]
		}
	}

	return "unknown"
}

// RunConanInstallWithBuildInfo runs conan install and collects build information
func RunConanInstallWithBuildInfo(workingDir string, buildName, buildNumber string, extraArgs []string) error {
	config := ConanConfig{
		WorkingDirectory: workingDir,
	}

	conanFlex, err := NewConanFlexPack(config)
	if err != nil {
		return fmt.Errorf("failed to create Conan instance: %w", err)
	}

	args := append([]string{"install", "."}, extraArgs...)
	cmd := exec.Command(config.ConanExecutable, args...)
	cmd.Dir = workingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("conan install failed: %w", err)
	}

	log.Info("Conan install completed successfully")

	if buildName != "" && buildNumber != "" {
		buildInfo, err := conanFlex.CollectBuildInfo(buildName, buildNumber)
		if err != nil {
			return fmt.Errorf("failed to collect build info: %w", err)
		}

		if err := SaveConanBuildInfoForJfrogCli(buildInfo); err != nil {
			log.Warn("Failed to save build info for jfrog-cli compatibility: " + err.Error())
		} else {
			log.Debug("Build info saved for jfrog-cli compatibility")
		}
	}

	return nil
}

// SaveConanBuildInfoForJfrogCli saves build info in a format compatible with jfrog-cli
func SaveConanBuildInfoForJfrogCli(buildInfo *entities.BuildInfo) error {
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
