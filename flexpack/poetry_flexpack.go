package flexpack

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// PoetryFlexPack implements the FlexPackManager interface for Poetry package manager
type PoetryFlexPack struct {
	config          PoetryConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	lockFileData    *PoetryLockFile
	pyprojectData   *PoetryPyProjectToml
}

// PoetryPyProjectToml represents the structure of pyproject.toml file for Poetry
type PoetryPyProjectToml struct {
	Tool struct {
		Poetry struct {
			Name            string                 `toml:"name"`
			Version         string                 `toml:"version"`
			Dependencies    map[string]interface{} `toml:"dependencies"`
			DevDependencies map[string]interface{} `toml:"dev-dependencies"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// PoetryLockFile represents the structure of poetry.lock file
type PoetryLockFile struct {
	Package []PoetryPackage `toml:"package"`
}

// PoetryPackage represents a package in poetry.lock
type PoetryPackage struct {
	Name         string                 `toml:"name"`
	Version      string                 `toml:"version"`
	Description  string                 `toml:"description"`
	Category     string                 `toml:"category"`
	Optional     bool                   `toml:"optional"`
	Dependencies map[string]interface{} `toml:"dependencies"`
	Source       *PoetrySource          `toml:"source"`
}

// PoetrySource represents package source information
type PoetrySource struct {
	Type string `toml:"type"`
	URL  string `toml:"url"`
	Name string `toml:"name"`
}

// PoetryShowOutput represents the output of 'poetry show' command
type PoetryShowOutput struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
}

// NewPoetryFlexPack creates a new Poetry FlexPack instance
func NewPoetryFlexPack(config PoetryConfig) (*PoetryFlexPack, error) {
	pf := &PoetryFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
	}
	// Load pyproject.toml
	if err := pf.loadPyProjectToml(); err != nil {
		return nil, fmt.Errorf("failed to load pyproject.toml: %w", err)
	}
	// Load poetry.lock (optional - can continue without it)
	if err := pf.loadPoetryLock(); err != nil {
		log.Debug("Failed to load poetry.lock, will use CLI-based dependency resolution: " + err.Error())
		// Don't return error - we can still collect dependencies via CLI
	}
	return pf, nil
}

// GetDependency returns dependency information along with name and version
func (pf *PoetryFlexPack) GetDependency() string {
	if len(pf.dependencies) == 0 {
		pf.parseDependencies()
	}
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Project: %s:%s\n", pf.projectName, pf.projectVersion))
	result.WriteString("Dependencies:\n")
	for _, dep := range pf.dependencies {
		result.WriteString(fmt.Sprintf("  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type))
	}
	return result.String()
}

// ParseDependencyToList parses and returns a list of dependencies with their name and version
func (pf *PoetryFlexPack) ParseDependencyToList() []string {
	if len(pf.dependencies) == 0 {
		pf.parseDependencies()
	}
	var depList []string
	for _, dep := range pf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum calculates checksums for dependencies in the provided list
func (pf *PoetryFlexPack) CalculateChecksum() []map[string]interface{} {
	if len(pf.dependencies) == 0 {
		pf.parseDependencies()
	}
	var checksums []map[string]interface{}
	for _, dep := range pf.dependencies {
		checksumMap := make(map[string]interface{})
		// Find the package file path
		filePath := pf.findPackageFilePath(dep.Name, dep.Version)
		if filePath != "" {
			// Calculate checksums for the package file
			sha1Hash, sha256Hash, md5Hash := pf.calculateFileChecksums(filePath)
			checksumMap["type"] = dep.Type
			checksumMap["sha1"] = sha1Hash
			checksumMap["sha256"] = sha256Hash
			checksumMap["md5"] = md5Hash
			checksumMap["id"] = dep.ID
			// Check if scopes are in sync with package manager scopes
			checksumMap["scopes"] = pf.validateAndNormalizeScopes(dep.Scopes)
			checksumMap["name"] = dep.Name
			checksumMap["version"] = dep.Version
			checksumMap["path"] = filePath
		} else {
			// If file not found, create entry with empty checksums
			checksumMap["type"] = dep.Type
			checksumMap["sha1"] = ""
			checksumMap["sha256"] = ""
			checksumMap["md5"] = ""
			checksumMap["id"] = dep.ID
			// Check if scopes are in sync with package manager scopes
			checksumMap["scopes"] = pf.validateAndNormalizeScopes(dep.Scopes)
			checksumMap["name"] = dep.Name
			checksumMap["version"] = dep.Version
			checksumMap["path"] = ""
		}
		checksums = append(checksums, checksumMap)
	}
	return checksums
}

// CalculateScopes calculates and returns the scopes for dependencies
func (pf *PoetryFlexPack) CalculateScopes() []string {
	scopesMap := make(map[string]bool)
	if len(pf.dependencies) == 0 {
		pf.parseDependencies()
	}
	for _, dep := range pf.dependencies {
		for _, scope := range dep.Scopes {
			scopesMap[scope] = true
		}
	}
	var scopes []string
	for scope := range scopesMap {
		scopes = append(scopes, scope)
	}
	return scopes
}

// CalculateRequestedBy determines which dependencies requested a particular package
func (pf *PoetryFlexPack) CalculateRequestedBy() map[string][]string {
	if len(pf.dependencyGraph) == 0 {
		pf.buildDependencyGraph()
	}
	requestedBy := make(map[string][]string)
	// Invert the dependency graph to show what packages are requested by which packages
	for parent, children := range pf.dependencyGraph {
		for _, child := range children {
			if requestedBy[child] == nil {
				requestedBy[child] = []string{}
			}
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	return requestedBy
}

// loadPyProjectToml loads and parses the pyproject.toml file
func (pf *PoetryFlexPack) loadPyProjectToml() error {
	pyprojectPath := filepath.Join(pf.config.WorkingDirectory, "pyproject.toml")
	data, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return fmt.Errorf("failed to read pyproject.toml: %w", err)
	}
	pf.pyprojectData = &PoetryPyProjectToml{}
	if err := toml.Unmarshal(data, pf.pyprojectData); err != nil {
		return fmt.Errorf("failed to parse pyproject.toml: %w", err)
	}
	pf.projectName = pf.pyprojectData.Tool.Poetry.Name
	pf.projectVersion = pf.pyprojectData.Tool.Poetry.Version
	return nil
}

// loadPoetryLock loads and parses the poetry.lock file
func (pf *PoetryFlexPack) loadPoetryLock() error {
	lockPath := filepath.Join(pf.config.WorkingDirectory, "poetry.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debug("poetry.lock not found at " + lockPath + ", continuing without lock file")
			return nil
		}
		return fmt.Errorf("failed to read poetry.lock: %w", err)
	}
	pf.lockFileData = &PoetryLockFile{}
	if err := toml.Unmarshal(data, pf.lockFileData); err != nil {
		return fmt.Errorf("failed to parse poetry.lock: %w", err)
	}
	return nil
}

// parseDependencies parses dependencies using hybrid CLI + file parsing approach
func (pf *PoetryFlexPack) parseDependencies() {
	// Strategy 1: Prefer lock file if it was loaded (deterministic)
	if pf.lockFileData != nil && pf.pyprojectData != nil {
		pf.parseFromFiles()
		if len(pf.dependencies) > 0 {
			log.Debug("Successfully parsed dependencies from poetry.lock")
			return
		}
		log.Debug("poetry.lock parsing yielded no dependencies, falling back to CLI")
	} else {
		log.Debug("poetry.lock not loaded, trying CLI")
	}
	// Strategy 2: Try CLI-based resolution
	if err := pf.parseWithPoetryShow(); err == nil {
		log.Debug("Successfully parsed dependencies using 'poetry show'")
		return
	}
	// Strategy 3: Fallback to file parsing (more limited but reliable)
	pf.parseFromFiles()
}

// parseWithPoetryShow uses 'poetry show' command for dependency resolution
func (pf *PoetryFlexPack) parseWithPoetryShow() error {
	cmd := exec.Command("poetry", "show", "--tree")
	cmd.Dir = pf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		log.Debug("Failed to execute 'poetry show --tree': " + err.Error())
		return err
	}

	return pf.parsePoetryShowOutput(string(output))
}

// parsePoetryShowOutput parses the output of 'poetry show --tree'
func (pf *PoetryFlexPack) parsePoetryShowOutput(output string) error {
	lines := strings.Split(output, "\n")
	var currentParent string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this is a top-level dependency (no indentation or tree characters)
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "├") && !strings.HasPrefix(line, "└") && !strings.HasPrefix(line, "│") {
			// Parse top-level dependency
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[0]
				version := parts[1]

				dep := DependencyInfo{
					Type:    "python",
					ID:      fmt.Sprintf("%s:%s", name, version),
					Name:    name,
					Version: version,
					Scopes:  []string{"main"},
				}

				pf.dependencies = append(pf.dependencies, dep)
				currentParent = dep.ID
			}
		} else {
			// Parse sub-dependency (transitive)
			// Remove tree formatting characters: ├, └, ─, │, and spaces
			cleaned := strings.TrimLeft(line, " ├└─│")
			cleaned = strings.TrimSpace(cleaned)

			// Skip lines that are only tree formatting or empty after cleaning
			if cleaned == "" || strings.HasPrefix(cleaned, "└──") || strings.HasPrefix(cleaned, "├──") {
				continue
			}

			parts := strings.Fields(cleaned)
			if len(parts) >= 2 && currentParent != "" {
				name := parts[0]
				version := parts[1]

				childID := fmt.Sprintf("%s:%s", name, version)

				// Add to dependency graph
				if pf.dependencyGraph[currentParent] == nil {
					pf.dependencyGraph[currentParent] = []string{}
				}
				pf.dependencyGraph[currentParent] = append(pf.dependencyGraph[currentParent], childID)

				// Add as transitive dependency if not already exists
				exists := false
				for _, existingDep := range pf.dependencies {
					if existingDep.ID == childID {
						exists = true
						break
					}
				}

				if !exists {
					dep := DependencyInfo{
						Type:    "python",
						ID:      childID,
						Name:    name,
						Version: version,
						Scopes:  []string{"transitive"},
					}
					pf.dependencies = append(pf.dependencies, dep)
				}
			}
		}
	}

	return nil
}

// parseFromFiles parses dependencies from poetry.lock and pyproject.toml (fallback method)
func (pf *PoetryFlexPack) parseFromFiles() {
	if pf.lockFileData == nil || pf.pyprojectData == nil {
		log.Warn("Poetry lock file or pyproject.toml not loaded")
		return
	}
	directDeps := make(map[string]bool)
	directDevDeps := make(map[string]bool)
	// Identify direct dependencies
	for depName := range pf.pyprojectData.Tool.Poetry.Dependencies {
		if depName != "python" { // Exclude python version constraint
			directDeps[depName] = true
		}
	}
	if pf.config.IncludeDevDependencies {
		for depName := range pf.pyprojectData.Tool.Poetry.DevDependencies {
			directDevDeps[depName] = true
		}
	}
	// Process all packages from poetry.lock
	for _, pkg := range pf.lockFileData.Package {
		dep := DependencyInfo{
			Type:    "python",
			ID:      fmt.Sprintf("%s:%s", pkg.Name, pkg.Version),
			Name:    pkg.Name,
			Version: pkg.Version,
			Scopes:  pf.determineScopes(pkg.Name, directDeps, directDevDeps, pkg.Category),
		}
		pf.dependencies = append(pf.dependencies, dep)
	}
}

// determineScopes determines the scopes for a dependency
func (pf *PoetryFlexPack) determineScopes(depName string, directDeps, directDevDeps map[string]bool, category string) []string {
	var scopes []string
	if directDeps[depName] {
		scopes = append(scopes, "main")
	}
	if directDevDeps[depName] {
		scopes = append(scopes, "dev")
	}
	// If not a direct dependency, it's a transitive dependency
	if !directDeps[depName] && !directDevDeps[depName] {
		if category == "dev" {
			scopes = append(scopes, "dev-transitive")
		} else {
			scopes = append(scopes, "transitive")
		}
	}
	if len(scopes) == 0 {
		scopes = append(scopes, "runtime")
	}
	return scopes
}

// validateAndNormalizeScopes validates and normalizes scopes to ensure they are in sync with package manager scopes
func (pf *PoetryFlexPack) validateAndNormalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"runtime"}
	}

	// Normalize scope names to standard package manager scopes
	normalizedScopes := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		switch scope {
		case "main", "runtime":
			normalizedScopes = append(normalizedScopes, "runtime")
		case "dev", "development":
			normalizedScopes = append(normalizedScopes, "dev")
		case "transitive", "dependency":
			normalizedScopes = append(normalizedScopes, "transitive")
		case "dev-transitive", "dev-dependency":
			normalizedScopes = append(normalizedScopes, "dev-transitive")
		default:
			// Keep unknown scopes as-is but log for debugging
			log.Debug("Unknown scope found: " + scope)
			normalizedScopes = append(normalizedScopes, scope)
		}
	}

	// Remove duplicates while preserving order
	seen := make(map[string]bool)
	result := make([]string, 0, len(normalizedScopes))
	for _, scope := range normalizedScopes {
		if !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}

	return result
}

// buildDependencyGraph builds the complete dependency graph
func (pf *PoetryFlexPack) buildDependencyGraph() {
	if pf.lockFileData == nil {
		return
	}
	// Add project as root
	projectKey := fmt.Sprintf("%s:%s", pf.projectName, pf.projectVersion)
	pf.dependencyGraph[projectKey] = []string{}
	// Add direct dependencies to project
	for depName := range pf.pyprojectData.Tool.Poetry.Dependencies {
		if depName != "python" {
			for _, pkg := range pf.lockFileData.Package {
				if strings.EqualFold(pkg.Name, depName) {
					depKey := fmt.Sprintf("%s:%s", pkg.Name, pkg.Version)
					pf.dependencyGraph[projectKey] = append(pf.dependencyGraph[projectKey], depKey)
					break
				}
			}
		}
	}
	if pf.config.IncludeDevDependencies {
		for depName := range pf.pyprojectData.Tool.Poetry.DevDependencies {
			for _, pkg := range pf.lockFileData.Package {
				if strings.EqualFold(pkg.Name, depName) {
					depKey := fmt.Sprintf("%s:%s", pkg.Name, pkg.Version)
					pf.dependencyGraph[projectKey] = append(pf.dependencyGraph[projectKey], depKey)
					break
				}
			}
		}
	}
	// Build dependency relationships
	for _, pkg := range pf.lockFileData.Package {
		pkgKey := fmt.Sprintf("%s:%s", pkg.Name, pkg.Version)
		pf.dependencyGraph[pkgKey] = []string{}
		for depName := range pkg.Dependencies {
			// Find the version of this dependency in lock file
			for _, depPkg := range pf.lockFileData.Package {
				if strings.EqualFold(depPkg.Name, depName) {
					depKey := fmt.Sprintf("%s:%s", depPkg.Name, depPkg.Version)
					pf.dependencyGraph[pkgKey] = append(pf.dependencyGraph[pkgKey], depKey)
					break
				}
			}
		}
	}
}

// findPackageFilePath attempts to find the file path for a package in Poetry cache
func (pf *PoetryFlexPack) findPackageFilePath(name, version string) string {
	// Try to find in Poetry cache directory
	cacheDir, err := pf.getPoetryCacheDirectory()
	log.Debug("getPoetryCacheDirectory returned:", cacheDir)
	if err != nil {
		log.Debug("Failed to get Poetry cache directory:", err)
		return ""
	}

	// Look for wheel files (.whl) or source distributions (.tar.gz) in cache
	// Poetry stores files in hash-based subdirectories, so we need recursive search
	targetPatterns := []string{
		fmt.Sprintf("%s-%s-", name, version),
		fmt.Sprintf("%s-%s.", name, version),
		// Handle name normalization (hyphens vs underscores)
		fmt.Sprintf("%s-%s-", strings.ReplaceAll(name, "-", "_"), version),
		fmt.Sprintf("%s-%s.", strings.ReplaceAll(name, "-", "_"), version),
	}

	var foundPath string
	_ = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Continue walking even if there's an error
			return err
		}

		if !d.IsDir() {
			filename := filepath.Base(path)
			// Check if filename matches any of our target patterns
			for _, pattern := range targetPatterns {
				if strings.HasPrefix(filename, pattern) &&
					(strings.HasSuffix(filename, ".whl") || strings.HasSuffix(filename, ".tar.gz")) {
					log.Debug("Found package file:", path)
					foundPath = path
					return filepath.SkipAll // Stop walking once we find a match
				}
			}
		}
		return nil
	})

	if foundPath != "" {
		return foundPath
	}

	log.Debug("No package files found for", name, version, "in", cacheDir)
	return ""
}

// getPoetryCacheDirectory returns the Poetry cache directory
func (pf *PoetryFlexPack) getPoetryCacheDirectory() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Poetry stores packages in multiple possible locations
	possiblePaths := []string{
		filepath.Join(homeDir, ".cache", "pypoetry", "artifacts"),
		filepath.Join(homeDir, "Library", "Caches", "pypoetry", "artifacts"),
		filepath.Join(homeDir, ".local", "share", "pypoetry", "artifacts"),
		filepath.Join(homeDir, "AppData", "Local", "pypoetry", "artifacts"), // Windows
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// If no cache directory found, provide helpful information
	return "", fmt.Errorf(`poetry artifacts directory not found

Poetry cache locations checked:
%s

To use this tool:
1. Install Poetry: https://python-poetry.org/docs/#installation
2. Run 'poetry install' in a project to populate the cache
3. Or manually download packages to one of the cache directories above

Note: Poetry stores downloaded packages in the artifacts directory. If you haven't used Poetry yet, this directory may not exist`, strings.Join(possiblePaths, "\n"))
}

// calculateFileChecksums calculates SHA1, SHA256, and MD5 checksums for a file using crypto.GetFileDetails
func (pf *PoetryFlexPack) calculateFileChecksums(filePath string) (sha1Hash, sha256Hash, md5Hash string) {
	// Use the existing GetFileDetails method from crypto package
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		log.Debug("Failed to calculate checksums for file:", filePath, err)
		return "", "", ""
	}

	// Extract checksums from the FileDetails struct
	sha1Hash = fileDetails.Checksum.Sha1
	sha256Hash = fileDetails.Checksum.Sha256
	md5Hash = fileDetails.Checksum.Md5

	return sha1Hash, sha256Hash, md5Hash
}

// RunPoetryInstallWithBuildInfo runs poetry install and collects build information
func RunPoetryInstallWithBuildInfo(workingDir string, buildName, buildNumber string, includeDevDeps bool, extraArgs []string) error {
	log.Info("Running Poetry install with build info collection...")
	// Create configuration
	config := PoetryConfig{
		WorkingDirectory:       workingDir,
		IncludeDevDependencies: includeDevDeps,
	}
	// Create Poetry FlexPack instance
	poetryFlex, err := NewPoetryFlexPack(config)
	if err != nil {
		return fmt.Errorf("failed to create Poetry instance: %w", err)
	}
	// Run poetry install command
	args := append([]string{"install"}, extraArgs...)
	cmd := exec.Command("poetry", args...)
	cmd.Dir = workingDir
	log.Debug("Executing command:", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("poetry install failed: %w\nOutput: %s", err, string(output))
	}
	log.Info("Poetry install completed successfully")
	// Collect build information if build name and number are provided
	if buildName != "" && buildNumber != "" {
		log.Info("Collecting build information...")

		// Use the CollectBuildInfo method to get complete build info
		buildInfo, err := poetryFlex.CollectBuildInfo(buildName, buildNumber)
		if err != nil {
			return fmt.Errorf("failed to collect build info: %w", err)
		}

		// Save build info using build-info-go service for jfrog-cli compatibility
		err = savePoetryBuildInfoForJfrogCli(buildInfo)
		if err != nil {
			log.Warn("Failed to save build info for jfrog-cli compatibility: " + err.Error())
			// Don't fail the entire operation, but warn the user
		} else {
			log.Info("Build info saved for jfrog-cli compatibility")
		}

		log.Info("Build information collection completed")
		log.Debug(fmt.Sprintf("Build info: %+v", buildInfo))
	}
	return nil
}

// savePoetryBuildInfoForJfrogCli saves build info in a format compatible with jfrog-cli rt bp
func savePoetryBuildInfoForJfrogCli(buildInfo *entities.BuildInfo) error {
	// Create build-info service
	service := build.NewBuildInfoService()

	// Create or get build
	bld, err := service.GetOrCreateBuildWithProject(buildInfo.Name, buildInfo.Number, "")
	if err != nil {
		return fmt.Errorf("failed to create build: %w", err)
	}

	// Save the complete build info (this will be loaded by rt bp)
	err = bld.SaveBuildInfo(buildInfo)
	if err != nil {
		return fmt.Errorf("failed to save build info: %w", err)
	}

	return nil
}

// ===== BuildInfoCollector Interface Implementation =====

// CollectBuildInfo collects complete build information including dependencies
func (pf *PoetryFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	buildInfo := &entities.BuildInfo{
		Name:   buildName,
		Number: buildNumber,
		Agent: &entities.Agent{
			Name:    "poetry",
			Version: pf.getPoetryVersion(),
		},
		BuildAgent: &entities.Agent{
			Name:    "Generic",
			Version: "1.0",
		},
		Modules: []entities.Module{},
	}

	// Create module for the project
	module := entities.Module{
		Id:   fmt.Sprintf("%s:%s", pf.projectName, pf.projectVersion),
		Type: "pypi",
	}

	// Get all dependencies
	deps, err := pf.GetProjectDependencies()
	if err != nil {
		return nil, err
	}

	// Convert dependencies to entities format
	for _, dep := range deps {
		entityDep := entities.Dependency{
			Id:     dep.ID,
			Type:   dep.Type,
			Scopes: dep.Scopes,
			Checksum: entities.Checksum{
				Sha1:   dep.SHA1,
				Sha256: dep.SHA256,
				Md5:    dep.MD5,
			},
		}

		// Add RequestedBy information
		if len(dep.RequestedBy) > 0 {
			entityDep.RequestedBy = [][]string{dep.RequestedBy}
		}

		module.Dependencies = append(module.Dependencies, entityDep)
	}

	buildInfo.Modules = append(buildInfo.Modules, module)

	return buildInfo, nil
}

// GetProjectDependencies returns all project dependencies with full details
func (pf *PoetryFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	if len(pf.dependencies) == 0 {
		pf.parseDependencies()
	}

	// Use caching to enhance dependencies with checksums and metadata
	err := pf.UpdateDependenciesWithCache()
	if err != nil {
		log.Warn("Failed to update dependencies with cache: " + err.Error())
		// Continue with fallback approach
	}

	// Calculate RequestedBy relationships
	requestedBy := pf.CalculateRequestedBy()

	// Add RequestedBy information to dependencies
	for i, dep := range pf.dependencies {
		if parents, exists := requestedBy[dep.ID]; exists {
			pf.dependencies[i].RequestedBy = parents
		}
	}

	return pf.dependencies, nil
}

// GetDependencyGraph returns the complete dependency graph
func (pf *PoetryFlexPack) GetDependencyGraph() (map[string][]string, error) {
	if len(pf.dependencies) == 0 {
		pf.parseDependencies()
	}

	return pf.dependencyGraph, nil
}

// getPoetryVersion gets the Poetry version for build info
func (pf *PoetryFlexPack) getPoetryVersion() string {
	cmd := exec.Command("poetry", "--version")
	output, err := cmd.Output()
	if err != nil {
		log.Debug("Failed to get Poetry version: " + err.Error())
		return "unknown"
	}

	version := strings.TrimSpace(string(output))
	// Poetry version output format: "Poetry (version 1.6.1)"
	if parts := strings.Fields(version); len(parts) >= 3 {
		return strings.Trim(parts[2], "()")
	}
	return version
}

// ===== Build-Info Dependencies Caching =====

const poetryCacheLatestVersion = 1

// PoetryDependenciesCache represents cached dependency information for Poetry projects
type PoetryDependenciesCache struct {
	Version     int                            `json:"version,omitempty"`
	DepsMap     map[string]entities.Dependency `json:"dependencies,omitempty"`
	LastUpdated time.Time                      `json:"lastUpdated,omitempty"`
	ProjectPath string                         `json:"projectPath,omitempty"`
}

// GetPoetryDependenciesCache reads the JSON cache file of recent used project's dependencies
// Returns cached dependencies map or nil if cache doesn't exist
func GetPoetryDependenciesCache(projectPath string) (cache *PoetryDependenciesCache, err error) {
	cache = new(PoetryDependenciesCache)
	cacheFilePath, exists := getPoetryDependenciesCacheFilePath(projectPath)
	if !exists {
		log.Debug("Poetry dependencies cache not found: " + cacheFilePath)
		return nil, err
	}

	jsonFile, err := os.Open(cacheFilePath)
	if err != nil {
		log.Debug("Failed to open Poetry cache file: " + err.Error())
		return nil, err
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		log.Debug("Failed to read Poetry cache file: " + err.Error())
		return nil, err
	}

	err = json.Unmarshal(byteValue, cache)
	if err != nil {
		log.Debug("Failed to parse Poetry cache file: " + err.Error())
		return nil, err
	}

	log.Debug(fmt.Sprintf("Loaded Poetry dependencies cache with %d entries", len(cache.DepsMap)))
	return cache, nil
}

// UpdatePoetryDependenciesCache writes the updated project's dependencies cache
func UpdatePoetryDependenciesCache(dependenciesMap map[string]entities.Dependency, projectPath string) error {
	updatedCache := PoetryDependenciesCache{
		Version:     poetryCacheLatestVersion,
		DepsMap:     dependenciesMap,
		LastUpdated: time.Now(),
		ProjectPath: projectPath,
	}

	content, err := json.MarshalIndent(&updatedCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Poetry cache: %w", err)
	}

	cacheFilePath, _ := getPoetryDependenciesCacheFilePath(projectPath)

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	err = os.WriteFile(cacheFilePath, content, 0644)
	if err != nil {
		return fmt.Errorf("failed to write Poetry cache file: %w", err)
	}

	log.Debug(fmt.Sprintf("Updated Poetry dependencies cache with %d entries at %s", len(dependenciesMap), cacheFilePath))
	return nil
}

// GetDependency returns required dependency from cache
func (cache *PoetryDependenciesCache) GetDependency(dependencyName string) (dependency entities.Dependency, found bool) {
	if cache == nil || cache.DepsMap == nil {
		return entities.Dependency{}, false
	}

	dependency, found = cache.DepsMap[dependencyName]
	if found {
		log.Debug("Found cached dependency: " + dependencyName)
	}
	return dependency, found
}

// HasDependency checks if a dependency exists in cache
func (cache *PoetryDependenciesCache) HasDependency(dependencyName string) bool {
	_, found := cache.GetDependency(dependencyName)
	return found
}

// IsValid checks if the cache is valid and not expired
func (cache *PoetryDependenciesCache) IsValid(maxAge time.Duration) bool {
	if cache == nil {
		return false
	}

	// Check version compatibility
	if cache.Version != poetryCacheLatestVersion {
		log.Debug(fmt.Sprintf("Poetry cache version mismatch: expected %d, got %d", poetryCacheLatestVersion, cache.Version))
		return false
	}

	// Check if cache is too old
	if maxAge > 0 && time.Since(cache.LastUpdated) > maxAge {
		log.Debug(fmt.Sprintf("Poetry cache expired: last updated %v ago", time.Since(cache.LastUpdated)))
		return false
	}

	return true
}

// getPoetryDependenciesCacheFilePath returns the path to Poetry dependencies cache file
// Cache file location: ./.jfrog/projects/poetry-deps.cache.json
func getPoetryDependenciesCacheFilePath(projectPath string) (cacheFilePath string, exists bool) {
	projectsDirPath := filepath.Join(projectPath, ".jfrog", "projects")
	cacheFilePath = filepath.Join(projectsDirPath, "poetry-deps.cache.json")
	_, err := os.Stat(cacheFilePath)
	exists = !os.IsNotExist(err)
	return cacheFilePath, exists
}

// UpdateDependenciesWithCache enhances dependencies with cached information
func (pf *PoetryFlexPack) UpdateDependenciesWithCache() error {
	// Load existing cache
	cache, err := GetPoetryDependenciesCache(pf.config.WorkingDirectory)
	if err != nil {
		log.Debug("No existing Poetry cache found, will create new one")
		cache = nil
	}

	// Check if cache is valid (max age: 24 hours)
	maxCacheAge := 24 * time.Hour
	if cache != nil && !cache.IsValid(maxCacheAge) {
		log.Debug("Poetry cache is invalid or expired, ignoring")
		cache = nil
	}

	dependenciesMap := make(map[string]entities.Dependency)
	var missingDeps []string

	// Process each dependency
	for i, dep := range pf.dependencies {
		depKey := fmt.Sprintf("%s:%s", dep.Name, dep.Version)

		// Try to get from cache first
		var cachedDep entities.Dependency
		var found bool
		if cache != nil {
			cachedDep, found = cache.GetDependency(depKey)
		}

		if found && !cachedDep.Checksum.IsEmpty() {
			// Use cached dependency info
			entityDep := entities.Dependency{
				Id:       depKey,
				Type:     "pypi",
				Scopes:   dep.Scopes,
				Checksum: cachedDep.Checksum,
			}
			dependenciesMap[depKey] = entityDep

			// Update our internal dependency with cached checksums
			pf.dependencies[i].SHA1 = cachedDep.Checksum.Sha1
			pf.dependencies[i].SHA256 = cachedDep.Checksum.Sha256
			pf.dependencies[i].MD5 = cachedDep.Checksum.Md5

			log.Debug("Using cached checksums for " + depKey)
		} else {
			// Need to calculate checksums for this dependency
			checksums := pf.calculateDependencyChecksum(dep)
			if len(checksums) > 0 && checksums[0] != nil {
				checksum := checksums[0]
				if sha1, ok := checksum["sha1"].(string); ok && sha1 != "" {
					// Check other checksum type assertions
					sha256, sha256Ok := checksum["sha256"].(string)
					if !sha256Ok {
						sha256 = ""
					}
					md5, md5Ok := checksum["md5"].(string)
					if !md5Ok {
						md5 = ""
					}

					entityDep := entities.Dependency{
						Id:     depKey,
						Type:   "pypi",
						Scopes: dep.Scopes,
						Checksum: entities.Checksum{
							Sha1:   sha1,
							Sha256: sha256,
							Md5:    md5,
						},
					}
					dependenciesMap[depKey] = entityDep

					// Update our internal dependency
					pf.dependencies[i].SHA1 = sha1
					pf.dependencies[i].SHA256 = sha256
					pf.dependencies[i].MD5 = md5

					log.Debug("Calculated new checksums for " + depKey)
				} else {
					missingDeps = append(missingDeps, depKey)
					log.Debug("Could not calculate checksums for " + depKey)
				}
			} else {
				missingDeps = append(missingDeps, depKey)
				log.Debug("No checksum data available for " + depKey)
			}
		}
	}

	// Report missing dependencies
	if len(missingDeps) > 0 {
		log.Warn("The following Poetry packages could not be found or checksums calculated:")
		for _, dep := range missingDeps {
			log.Warn("  - " + dep)
		}
		log.Warn("This may happen if packages are not in Poetry cache or are virtual dependencies.")
		log.Warn("Run 'poetry install' to populate the cache, or use 'poetry show --tree' to verify dependencies.")
	}

	// Update cache with new information
	if len(dependenciesMap) > 0 {
		err = UpdatePoetryDependenciesCache(dependenciesMap, pf.config.WorkingDirectory)
		if err != nil {
			log.Warn("Failed to update Poetry dependencies cache: " + err.Error())
		}
	}

	return nil
}

// calculateDependencyChecksum calculates checksum for a single dependency
func (pf *PoetryFlexPack) calculateDependencyChecksum(dep DependencyInfo) []map[string]interface{} {
	// Try to find the package file and calculate its checksum
	packagePath := pf.findPackageFilePath(dep.Name, dep.Version)
	if packagePath != "" {
		fileDetails, err := crypto.GetFileDetails(packagePath, true)
		if err == nil {
			return []map[string]interface{}{{
				"id":      dep.ID,
				"name":    dep.Name,
				"version": dep.Version,
				"type":    dep.Type,
				"scopes":  dep.Scopes,
				"sha1":    fileDetails.Checksum.Sha1,
				"sha256":  fileDetails.Checksum.Sha256,
				"md5":     fileDetails.Checksum.Md5,
				"path":    packagePath,
			}}
		}
	}

	// Fallback: return empty checksum info
	return []map[string]interface{}{{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  dep.Scopes,
		"sha1":    "",
		"sha256":  "",
		"md5":     "",
		"path":    "",
	}}
}

// RunPoetryInstallWithBuildInfoAndCaching runs poetry install with build info and caching support
func RunPoetryInstallWithBuildInfoAndCaching(workingDir string, buildName, buildNumber string, includeDevDeps bool, extraArgs []string) error {
	log.Info("Running Poetry install with build-info caching support")

	// Create Poetry FlexPack configuration
	config := PoetryConfig{
		WorkingDirectory:       workingDir,
		IncludeDevDependencies: includeDevDeps,
	}

	// Create Poetry FlexPack instance
	poetryFlex, err := NewPoetryFlexPack(config)
	if err != nil {
		return fmt.Errorf("failed to create Poetry instance: %w", err)
	}

	// Load existing cache before running install
	existingCache, _ := GetPoetryDependenciesCache(workingDir)
	if existingCache != nil && existingCache.IsValid(24*time.Hour) {
		log.Info(fmt.Sprintf("Found valid Poetry dependencies cache with %d entries", len(existingCache.DepsMap)))
	}

	// Run the standard Poetry install
	err = RunPoetryInstallWithBuildInfo(workingDir, buildName, buildNumber, includeDevDeps, extraArgs)
	if err != nil {
		return fmt.Errorf("poetry install failed: %w", err)
	}

	// After successful install, update cache with new dependency information
	log.Info("Updating Poetry dependencies cache...")
	err = poetryFlex.UpdateDependenciesWithCache()
	if err != nil {
		log.Warn("Failed to update Poetry dependencies cache: " + err.Error())
		// Don't fail the entire operation for caching issues
	} else {
		log.Info("Successfully updated Poetry dependencies cache")
	}

	return nil
}

// ClearPoetryDependenciesCache clears the Poetry dependencies cache
func ClearPoetryDependenciesCache(projectPath string) error {
	cacheFilePath, exists := getPoetryDependenciesCacheFilePath(projectPath)

	if !exists {
		log.Debug("Poetry dependencies cache does not exist: " + cacheFilePath)
		return nil
	}

	err := os.Remove(cacheFilePath)
	if err != nil {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}

	log.Info("Cleared Poetry dependencies cache: " + cacheFilePath)
	return nil
}

// GetPoetryDependenciesCacheInfo returns information about the current cache
func GetPoetryDependenciesCacheInfo(projectPath string) (map[string]interface{}, error) {
	cache, err := GetPoetryDependenciesCache(projectPath)
	if err != nil {
		return map[string]interface{}{
			"exists": false,
			"error":  err.Error(),
		}, err
	}

	if cache == nil {
		return map[string]interface{}{
			"exists": false,
		}, nil
	}

	cacheFilePath, _ := getPoetryDependenciesCacheFilePath(projectPath)

	return map[string]interface{}{
		"exists":       true,
		"version":      cache.Version,
		"dependencies": len(cache.DepsMap),
		"lastUpdated":  cache.LastUpdated.Format(time.RFC3339),
		"projectPath":  cache.ProjectPath,
		"cacheFile":    cacheFilePath,
		"isValid":      cache.IsValid(24 * time.Hour),
		"age":          time.Since(cache.LastUpdated).String(),
	}, nil
}
