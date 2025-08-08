package flexpack

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// CargoFlexPack implements the FlexPackManager interface for Cargo package manager
type CargoFlexPack struct {
	config          PackageManagerConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	manifestData    *CargoManifest
	lockData        *CargoLock
}

// CargoManifest represents the structure of Cargo.toml file
type CargoManifest struct {
	Package struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
	} `toml:"package"`
	Dependencies      map[string]interface{} `toml:"dependencies"`
	DevDependencies   map[string]interface{} `toml:"dev-dependencies"`
	BuildDependencies map[string]interface{} `toml:"build-dependencies"`
	Workspace         *CargoWorkspace        `toml:"workspace"`
}

// CargoWorkspace represents workspace configuration
type CargoWorkspace struct {
	Members []string `toml:"members"`
}

// CargoLock represents the structure of Cargo.lock file
type CargoLock struct {
	Version  int            `toml:"version"`
	Packages []CargoPackage `toml:"package"`
}

// CargoPackage represents a package in Cargo.lock
type CargoPackage struct {
	Name         string   `toml:"name"`
	Version      string   `toml:"version"`
	Source       string   `toml:"source"`
	Checksum     string   `toml:"checksum"`
	Dependencies []string `toml:"dependencies"`
}

// NewCargoFlexPack creates a new Cargo FlexPack instance
func NewCargoFlexPack(config PackageManagerConfig) (*CargoFlexPack, error) {
	cf := &CargoFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
	}

	// Load Cargo.toml
	if err := cf.loadCargoManifest(); err != nil {
		return nil, fmt.Errorf("failed to load Cargo.toml: %w", err)
	}

	// Load Cargo.lock
	if err := cf.loadCargoLock(); err != nil {
		return nil, fmt.Errorf("failed to load Cargo.lock: %w", err)
	}

	return cf, nil
}

// loadCargoManifest loads and parses Cargo.toml file
func (cf *CargoFlexPack) loadCargoManifest() error {
	manifestPath := filepath.Join(cf.config.WorkingDirectory, "Cargo.toml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	cf.manifestData = &CargoManifest{}
	if err := toml.Unmarshal(data, cf.manifestData); err != nil {
		return err
	}

	cf.projectName = cf.manifestData.Package.Name
	cf.projectVersion = cf.manifestData.Package.Version

	return nil
}

// loadCargoLock loads and parses Cargo.lock file
func (cf *CargoFlexPack) loadCargoLock() error {
	lockPath := filepath.Join(cf.config.WorkingDirectory, "Cargo.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		// Cargo.lock might not exist yet, try to generate it
		log.Debug("Cargo.lock not found, attempting to generate it")
		if err := cf.runCargoBuild(); err != nil {
			return fmt.Errorf("failed to generate Cargo.lock: %w", err)
		}
		// Try reading again
		data, err = os.ReadFile(lockPath)
		if err != nil {
			return err
		}
	}

	cf.lockData = &CargoLock{}
	if err := toml.Unmarshal(data, cf.lockData); err != nil {
		return err
	}

	return nil
}

// runCargoBuild runs cargo build to generate Cargo.lock
func (cf *CargoFlexPack) runCargoBuild() error {
	cmd := exec.Command("cargo", "build")
	cmd.Dir = cf.config.WorkingDirectory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cargo build failed: %w\nOutput: %s", err, output)
	}
	return nil
}

// GetDependency returns dependency information along with name and version
func (cf *CargoFlexPack) GetDependency() string {
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

// ParseDependencyToList parses and returns a list of dependencies with their name and version
func (cf *CargoFlexPack) ParseDependencyToList() []string {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}
	var result []string
	for _, dep := range cf.dependencies {
		result = append(result, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return result
}

// parseDependencies parses dependencies using cargo tree command with fallback to lock file
func (cf *CargoFlexPack) parseDependencies() {
	// Try using cargo tree first (most accurate)
	if err := cf.parseWithCargoTree(); err == nil {
		return
	}

	// Fallback to parsing lock file
	log.Debug("Falling back to lock file parsing")
	cf.parseFromLockFile()
}

// parseWithCargoTree uses cargo tree command to get dependency information
func (cf *CargoFlexPack) parseWithCargoTree() error {
	cmd := exec.Command("cargo", "tree", "--all-features", "--no-dedupe", "--prefix=none")
	cmd.Dir = cf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("cargo tree failed: %w", err)
	}

	// Parse cargo tree output
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	projectKey := fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and status messages
		if line == "" || strings.Contains(line, "Downloading:") || strings.Contains(line, "Downloaded:") {
			continue
		}

		// Parse dependency line
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := parts[0]
			version := strings.TrimPrefix(parts[1], "v")

			// Skip the project itself
			if name == cf.projectName && version == cf.projectVersion {
				continue
			}

			// Determine scope
			scopes := cf.determineScopes(name)

			dep := DependencyInfo{
				Type:    "cargo",
				ID:      fmt.Sprintf("%s:%s", name, version),
				Name:    name,
				Version: version,
				Scopes:  scopes,
			}

			cf.dependencies = append(cf.dependencies, dep)

			// Build dependency graph
			cf.dependencyGraph[projectKey] = append(cf.dependencyGraph[projectKey], dep.ID)
		}
	}

	return scanner.Err()
}

// parseFromLockFile fallback parser using Cargo.lock
func (cf *CargoFlexPack) parseFromLockFile() {
	projectKey := fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion)

	for _, pkg := range cf.lockData.Packages {
		// Skip the project itself
		if pkg.Name == cf.projectName && pkg.Version == cf.projectVersion {
			continue
		}

		scopes := cf.determineScopes(pkg.Name)

		dep := DependencyInfo{
			Type:    "cargo",
			ID:      fmt.Sprintf("%s:%s", pkg.Name, pkg.Version),
			Name:    pkg.Name,
			Version: pkg.Version,
			Scopes:  scopes,
		}

		cf.dependencies = append(cf.dependencies, dep)

		// Build basic dependency graph
		cf.dependencyGraph[projectKey] = append(cf.dependencyGraph[projectKey], dep.ID)
	}
}

// determineScopes determines the scopes for a dependency
func (cf *CargoFlexPack) determineScopes(depName string) []string {
	var scopes []string

	// Check if it's in regular dependencies
	if _, exists := cf.manifestData.Dependencies[depName]; exists {
		scopes = append(scopes, "runtime")
	}

	// Check if it's in dev dependencies
	if cf.config.IncludeDevDependencies {
		if _, exists := cf.manifestData.DevDependencies[depName]; exists {
			scopes = append(scopes, "dev")
		}
	}

	// Check if it's in build dependencies
	if _, exists := cf.manifestData.BuildDependencies[depName]; exists {
		scopes = append(scopes, "build")
	}

	// If not found in manifest, it's likely transitive
	if len(scopes) == 0 {
		scopes = append(scopes, "transitive")
	}

	return scopes
}

// CalculateChecksum calculates checksums for dependencies
func (cf *CargoFlexPack) CalculateChecksum() []map[string]interface{} {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	var result []map[string]interface{}
	for _, dep := range cf.dependencies {
		checksumMap := cf.calculateChecksumForDependency(dep)
		result = append(result, checksumMap)
	}

	return result
}

// calculateChecksumForDependency calculates checksum for a single dependency
func (cf *CargoFlexPack) calculateChecksumForDependency(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  dep.Scopes,
	}

	// Try to find .crate file in cache
	cratePath := cf.findCrateFile(dep.Name, dep.Version)
	if cratePath != "" {
		if sha1, sha256, md5, err := cf.calculateFileChecksum(cratePath); err == nil {
			checksumMap["sha1"] = sha1
			checksumMap["sha256"] = sha256
			checksumMap["md5"] = md5
			checksumMap["path"] = cratePath
			return checksumMap
		}
	}

	// Fallback: use checksum from lock file if available
	for _, pkg := range cf.lockData.Packages {
		if pkg.Name == dep.Name && pkg.Version == dep.Version && pkg.Checksum != "" {
			checksumMap["sha1"] = ""
			checksumMap["sha256"] = pkg.Checksum
			checksumMap["md5"] = ""
			checksumMap["path"] = "lock-file"
			return checksumMap
		}
	}

	// Final fallback: manifest-based checksum
	sha1, sha256, md5 := cf.calculateManifestChecksum(dep)
	checksumMap["sha1"] = sha1
	checksumMap["sha256"] = sha256
	checksumMap["md5"] = md5
	checksumMap["path"] = "manifest"

	return checksumMap
}

// findCrateFile finds the .crate file in Cargo cache
func (cf *CargoFlexPack) findCrateFile(name, version string) string {
	cacheDirs := cf.getCacheDirs()

	// Try multiple version formats
	versionVariants := []string{
		strings.TrimPrefix(version, "v"),
		version,
		"v" + strings.TrimPrefix(version, "v"),
	}

	for _, cacheDir := range cacheDirs {
		registryCache := filepath.Join(cacheDir, "registry", "cache")

		// Walk through registry cache subdirectories
		entries, err := os.ReadDir(registryCache)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			subDir := filepath.Join(registryCache, entry.Name())
			for _, v := range versionVariants {
				filename := fmt.Sprintf("%s-%s.crate", name, v)
				cratePath := filepath.Join(subDir, filename)

				if _, err := os.Stat(cratePath); err == nil {
					return cratePath
				}
			}
		}
	}

	return ""
}

// getCacheDirs returns possible Cargo cache directories
func (cf *CargoFlexPack) getCacheDirs() []string {
	var paths []string

	// Check CARGO_HOME environment variable
	if cargoHome := os.Getenv("CARGO_HOME"); cargoHome != "" {
		paths = append(paths, cargoHome)
	}

	// Add platform-specific default paths
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		switch runtime.GOOS {
		case "windows":
			paths = append(paths, filepath.Join(homeDir, ".cargo"))
		case "darwin", "linux":
			paths = append(paths, filepath.Join(homeDir, ".cargo"))
		}
	}

	// Filter existing paths
	var existingPaths []string
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			existingPaths = append(existingPaths, path)
		}
	}

	return existingPaths
}

// calculateFileChecksum calculates checksums for a file
func (cf *CargoFlexPack) calculateFileChecksum(filePath string) (string, string, string, error) {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return "", "", "", err
	}

	return fileDetails.Checksum.Sha1,
		fileDetails.Checksum.Sha256,
		fileDetails.Checksum.Md5,
		nil
}

// calculateManifestChecksum creates deterministic checksums from manifest content
func (cf *CargoFlexPack) calculateManifestChecksum(dep DependencyInfo) (string, string, string) {
	manifest := fmt.Sprintf("name:%s\nversion:%s\ntype:%s\n",
		dep.Name, dep.Version, dep.Type)

	// Create temporary file for checksum calculation
	tempFile, err := os.CreateTemp("", "cargo-checksum-*.txt")
	if err != nil {
		return "", "", ""
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	tempFile.WriteString(manifest)
	tempFile.Close()

	sha1, sha256, md5, err := cf.calculateFileChecksum(tempFile.Name())
	if err != nil {
		return "", "", ""
	}

	return sha1, sha256, md5
}

// CalculateScopes returns all unique scopes found in dependencies
func (cf *CargoFlexPack) CalculateScopes() []string {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	scopeSet := make(map[string]bool)
	for _, dep := range cf.dependencies {
		for _, scope := range dep.Scopes {
			scopeSet[scope] = true
		}
	}

	var scopes []string
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}

	return scopes
}

// CalculateRequestedBy determines which dependencies requested each package
func (cf *CargoFlexPack) CalculateRequestedBy() map[string][]string {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	requestedBy := make(map[string][]string)

	// Invert the dependency graph
	for parent, children := range cf.dependencyGraph {
		for _, child := range children {
			if requestedBy[child] == nil {
				requestedBy[child] = []string{}
			}
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}

	// Deduplicate
	for child, parents := range requestedBy {
		uniqueParents := make(map[string]bool)
		for _, parent := range parents {
			uniqueParents[parent] = true
		}

		var deduped []string
		for parent := range uniqueParents {
			deduped = append(deduped, parent)
		}
		requestedBy[child] = deduped
	}

	return requestedBy
}

// CollectBuildInfo collects complete build information including dependencies
func (cf *CargoFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	buildInfo := &entities.BuildInfo{
		Name:   buildName,
		Number: buildNumber,
		Agent: &entities.Agent{
			Name:    "cargo",
			Version: cf.getCargoVersion(),
		},
		BuildAgent: &entities.Agent{
			Name:    "Generic",
			Version: "1.0",
		},
		Modules: []entities.Module{},
	}

	// Create module for the project
	module := entities.Module{
		Id:   fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion),
		Type: "cargo",
	}

	// Get all dependencies
	deps, err := cf.GetProjectDependencies()
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
func (cf *CargoFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	// Calculate checksums
	checksums := cf.CalculateChecksum()

	// Calculate RequestedBy
	requestedBy := cf.CalculateRequestedBy()

	// Merge checksum data into dependencies
	for i, dep := range cf.dependencies {
		if i < len(checksums) {
			checksum := checksums[i]
			cf.dependencies[i].SHA1 = checksum["sha1"].(string)
			cf.dependencies[i].SHA256 = checksum["sha256"].(string)
			cf.dependencies[i].MD5 = checksum["md5"].(string)
			if path, ok := checksum["path"].(string); ok {
				cf.dependencies[i].Path = path
			}
		}

		// Add RequestedBy information
		if parents, exists := requestedBy[dep.ID]; exists {
			cf.dependencies[i].RequestedBy = parents
		}
	}

	return cf.dependencies, nil
}

// GetDependencyGraph returns the complete dependency graph
func (cf *CargoFlexPack) GetDependencyGraph() (map[string][]string, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}

	return cf.dependencyGraph, nil
}

// getCargoVersion gets the installed cargo version
func (cf *CargoFlexPack) getCargoVersion() string {
	cmd := exec.Command("cargo", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	// Parse version from output like "cargo 1.75.0 (1d8b05cdd 2023-11-20)"
	parts := strings.Fields(string(output))
	if len(parts) >= 2 {
		return parts[1]
	}

	return "unknown"
}
