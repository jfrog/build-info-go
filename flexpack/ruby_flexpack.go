package flexpack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// RubyFlexPack implements the FlexPackManager interface for Ruby Bundler/Gems
type RubyFlexPack struct {
	config          PackageManagerConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	gemfileData     *RubyGemfile
	lockfileData    *RubyGemfileLock
}

// RubyGemfile represents the structure of Gemfile
type RubyGemfile struct {
	Source       string              `json:"source"`
	Dependencies []RubyGemDependency `json:"dependencies"`
	Groups       map[string][]string `json:"groups"`
}

// RubyGemDependency represents a gem dependency in Gemfile
type RubyGemDependency struct {
	Name    string   `json:"name"`
	Version string   `json:"version,omitempty"`
	Groups  []string `json:"groups,omitempty"`
	Source  string   `json:"source,omitempty"`
	Git     string   `json:"git,omitempty"`
	Path    string   `json:"path,omitempty"`
}

// RubyGemfileLock represents the structure of Gemfile.lock
type RubyGemfileLock struct {
	GEM          RubyGemSection             `json:"GEM"`
	PATH         RubyGemSection             `json:"PATH,omitempty"`
	GIT          RubyGemSection             `json:"GIT,omitempty"`
	Platforms    []string                   `json:"PLATFORMS"`
	Dependencies []string                   `json:"DEPENDENCIES"`
	BundledWith  string                     `json:"BUNDLED WITH"`
	Specs        map[string]RubyGemLockSpec `json:"specs"`
}

// RubyGemSection represents a section in Gemfile.lock (GEM, PATH, GIT)
type RubyGemSection struct {
	Remote string                     `json:"remote,omitempty"`
	Specs  map[string]RubyGemLockSpec `json:"specs"`
}

// RubyGemLockSpec represents a gem specification in Gemfile.lock
type RubyGemLockSpec struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
	Platform     string            `json:"platform,omitempty"`
}

// BundleShowOutput represents the output of 'bundle show' command
type BundleShowOutput struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Path     string `json:"path"`
	Summary  string `json:"summary"`
	Homepage string `json:"homepage"`
}

// NewRubyFlexPack creates a new Ruby FlexPack instance
func NewRubyFlexPack(config PackageManagerConfig) (*RubyFlexPack, error) {
	rf := &RubyFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
	}

	// Load Gemfile
	if err := rf.loadGemfile(); err != nil {
		return nil, fmt.Errorf("failed to load Gemfile: %w", err)
	}

	// Load Gemfile.lock
	if err := rf.loadGemfileLock(); err != nil {
		return nil, fmt.Errorf("failed to load Gemfile.lock: %w", err)
	}

	return rf, nil
}

// GetDependency returns dependency information along with name and version
func (rf *RubyFlexPack) GetDependency() string {
	if len(rf.dependencies) == 0 {
		rf.parseDependencies()
	}
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Project: %s:%s\n", rf.projectName, rf.projectVersion))
	result.WriteString("Dependencies:\n")
	for _, dep := range rf.dependencies {
		result.WriteString(fmt.Sprintf("  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type))
	}
	return result.String()
}

// ParseDependencyToList parses and returns a list of dependencies with their name and version
func (rf *RubyFlexPack) ParseDependencyToList() []string {
	if len(rf.dependencies) == 0 {
		rf.parseDependencies()
	}
	var depList []string
	for _, dep := range rf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum calculates checksums for dependencies in the provided list
func (rf *RubyFlexPack) CalculateChecksum() []map[string]interface{} {
	if len(rf.dependencies) == 0 {
		rf.parseDependencies()
	}
	var checksums []map[string]interface{}
	for _, dep := range rf.dependencies {
		checksumMap := make(map[string]interface{})
		// Find the gem file path
		filePath := rf.findGemFilePath(dep.Name, dep.Version)
		if filePath != "" {
			// Calculate checksums for the gem file
			sha1Hash, sha256Hash, md5Hash := rf.calculateFileChecksums(filePath)
			checksumMap["type"] = dep.Type
			checksumMap["sha1"] = sha1Hash
			checksumMap["sha256"] = sha256Hash
			checksumMap["md5"] = md5Hash
			checksumMap["id"] = dep.ID
			checksumMap["scopes"] = dep.Scopes
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
			checksumMap["scopes"] = dep.Scopes
			checksumMap["name"] = dep.Name
			checksumMap["version"] = dep.Version
			checksumMap["path"] = ""
		}
		checksums = append(checksums, checksumMap)
	}
	return checksums
}

// CalculateScopes calculates and returns the scopes for dependencies
func (rf *RubyFlexPack) CalculateScopes() []string {
	scopesMap := make(map[string]bool)
	if len(rf.dependencies) == 0 {
		rf.parseDependencies()
	}
	for _, dep := range rf.dependencies {
		for _, scope := range dep.Scopes {
			scopesMap[scope] = true
		}
	}
	var scopes []string
	for scope := range scopesMap {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

// CalculateRequestedBy determines which dependencies requested a particular package
func (rf *RubyFlexPack) CalculateRequestedBy() map[string][]string {
	if len(rf.dependencyGraph) == 0 {
		rf.buildDependencyGraph()
	}
	requestedBy := make(map[string][]string)
	// Invert the dependency graph to show what packages are requested by which packages
	for parent, children := range rf.dependencyGraph {
		for _, child := range children {
			if requestedBy[child] == nil {
				requestedBy[child] = []string{}
			}
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	return requestedBy
}

// loadGemfile loads and parses the Gemfile
func (rf *RubyFlexPack) loadGemfile() error {
	gemfilePath := filepath.Join(rf.config.WorkingDirectory, "Gemfile")
	data, err := os.ReadFile(gemfilePath)
	if err != nil {
		return fmt.Errorf("failed to read Gemfile: %w", err)
	}

	// Parse Gemfile content
	rf.gemfileData = &RubyGemfile{
		Dependencies: []RubyGemDependency{},
		Groups:       make(map[string][]string),
	}

	// Simple regex-based parsing for Gemfile
	content := string(data)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "source") {
			// Extract source
			re := regexp.MustCompile(`source\s+['"]([^'"]+)['"]`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				rf.gemfileData.Source = matches[1]
			}
		} else if strings.HasPrefix(line, "gem") {
			// Extract gem dependency
			re := regexp.MustCompile(`gem\s+['"]([^'"]+)['"](?:\s*,\s*['"]([^'"]+)['"])?`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				dep := RubyGemDependency{
					Name:   matches[1],
					Groups: []string{"default"},
				}
				if len(matches) > 2 && matches[2] != "" {
					dep.Version = matches[2]
				}
				rf.gemfileData.Dependencies = append(rf.gemfileData.Dependencies, dep)
			}
		}
	}

	// Extract project name from working directory
	rf.projectName = filepath.Base(rf.config.WorkingDirectory)
	rf.projectVersion = "1.0.0" // Default version

	return nil
}

// loadGemfileLock loads and parses the Gemfile.lock
func (rf *RubyFlexPack) loadGemfileLock() error {
	lockPath := filepath.Join(rf.config.WorkingDirectory, "Gemfile.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read Gemfile.lock: %w", err)
	}

	// Parse Gemfile.lock content
	rf.lockfileData = &RubyGemfileLock{
		Specs: make(map[string]RubyGemLockSpec),
	}

	content := string(data)
	sections := strings.Split(content, "\n\n")

	for _, section := range sections {
		lines := strings.Split(section, "\n")
		if len(lines) == 0 {
			continue
		}

		sectionHeader := strings.TrimSpace(lines[0])

		switch {
		case strings.HasPrefix(sectionHeader, "GEM"):
			rf.parseGemSection(lines)
		case strings.HasPrefix(sectionHeader, "PLATFORMS"):
			rf.parsePlatformsSection(lines)
		case strings.HasPrefix(sectionHeader, "DEPENDENCIES"):
			rf.parseDependenciesSection(lines)
		case strings.HasPrefix(sectionHeader, "BUNDLED WITH"):
			rf.parseBundledWithSection(lines)
		}
	}

	return nil
}

// parseGemSection parses the GEM section of Gemfile.lock
func (rf *RubyFlexPack) parseGemSection(lines []string) {
	rf.lockfileData.GEM.Specs = make(map[string]RubyGemLockSpec)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if i == 0 && strings.HasPrefix(line, "GEM") {
			continue
		}
		if strings.HasPrefix(line, "remote:") {
			rf.lockfileData.GEM.Remote = strings.TrimSpace(strings.TrimPrefix(line, "remote:"))
			continue
		}
		if strings.HasPrefix(line, "specs:") {
			continue
		}

		// Parse gem specs
		if strings.HasPrefix(line, "    ") { // 4 spaces = gem name and version
			// Extract gem name and version
			re := regexp.MustCompile(`^    ([a-zA-Z0-9_-]+)\s+\(([^)]+)\)(?:\s+(.+))?$`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 2 {
				spec := RubyGemLockSpec{
					Name:         matches[1],
					Version:      matches[2],
					Dependencies: make(map[string]string),
				}
				if len(matches) > 3 && matches[3] != "" {
					spec.Platform = matches[3]
				}
				rf.lockfileData.Specs[spec.Name] = spec
				rf.lockfileData.GEM.Specs[spec.Name] = spec
			}
		} else if strings.HasPrefix(line, "      ") { // 6 spaces = dependency
			// Parse dependencies of the current gem
			depLine := strings.TrimSpace(line)
			// Dependencies in format: "gem_name (>= version)"
			re := regexp.MustCompile(`([a-zA-Z0-9_-]+)\s*\(([^)]+)\)`)
			matches := re.FindStringSubmatch(depLine)
			if len(matches) > 2 {
				// Find the last added gem and add this dependency
				for name, spec := range rf.lockfileData.GEM.Specs {
					if spec.Dependencies == nil {
						spec.Dependencies = make(map[string]string)
					}
					spec.Dependencies[matches[1]] = matches[2]
					rf.lockfileData.GEM.Specs[name] = spec
					rf.lockfileData.Specs[name] = spec
					break // Add to the most recently parsed gem
				}
			}
		}
	}
}

// parsePlatformsSection parses the PLATFORMS section
func (rf *RubyFlexPack) parsePlatformsSection(lines []string) {
	for i, line := range lines {
		if i == 0 {
			continue // Skip "PLATFORMS" header
		}
		platform := strings.TrimSpace(line)
		if platform != "" {
			rf.lockfileData.Platforms = append(rf.lockfileData.Platforms, platform)
		}
	}
}

// parseDependenciesSection parses the DEPENDENCIES section
func (rf *RubyFlexPack) parseDependenciesSection(lines []string) {
	for i, line := range lines {
		if i == 0 {
			continue // Skip "DEPENDENCIES" header
		}
		dep := strings.TrimSpace(line)
		if dep != "" {
			rf.lockfileData.Dependencies = append(rf.lockfileData.Dependencies, dep)
		}
	}
}

// parseBundledWithSection parses the BUNDLED WITH section
func (rf *RubyFlexPack) parseBundledWithSection(lines []string) {
	if len(lines) > 1 {
		rf.lockfileData.BundledWith = strings.TrimSpace(lines[1])
	}
}

// parseDependencies parses dependencies from Gemfile.lock and Gemfile
func (rf *RubyFlexPack) parseDependencies() {
	if rf.lockfileData == nil || rf.gemfileData == nil {
		log.Warn("Ruby lock file or Gemfile not loaded")
		return
	}

	directDeps := make(map[string]bool)
	directDevDeps := make(map[string]bool)

	// Identify direct dependencies from Gemfile
	for _, dep := range rf.gemfileData.Dependencies {
		if contains(dep.Groups, "development") || contains(dep.Groups, "test") {
			directDevDeps[dep.Name] = true
		} else {
			directDeps[dep.Name] = true
		}
	}

	// Process all gems from Gemfile.lock
	for _, spec := range rf.lockfileData.Specs {
		dep := DependencyInfo{
			Type:    "gem",
			ID:      fmt.Sprintf("%s:%s", spec.Name, spec.Version),
			Name:    spec.Name,
			Version: spec.Version,
			Scopes:  rf.determineScopes(spec.Name, directDeps, directDevDeps),
		}
		rf.dependencies = append(rf.dependencies, dep)
	}
}

// determineScopes determines the scopes for a dependency
func (rf *RubyFlexPack) determineScopes(depName string, directDeps, directDevDeps map[string]bool) []string {
	var scopes []string
	if directDeps[depName] {
		scopes = append(scopes, "runtime")
		scopes = append(scopes, "compile")
	}
	if directDevDeps[depName] {
		scopes = append(scopes, "development")
		scopes = append(scopes, "test")
	}
	// If not a direct dependency, it's a transitive dependency
	if !directDeps[depName] && !directDevDeps[depName] {
		scopes = append(scopes, "transitive")
	}
	if len(scopes) == 0 {
		scopes = append(scopes, "runtime")
	}
	return scopes
}

// buildDependencyGraph builds the complete dependency graph
func (rf *RubyFlexPack) buildDependencyGraph() {
	if rf.lockfileData == nil {
		return
	}

	// Add project as root
	projectKey := fmt.Sprintf("%s:%s", rf.projectName, rf.projectVersion)
	rf.dependencyGraph[projectKey] = []string{}

	// Add direct dependencies to project
	for _, dep := range rf.gemfileData.Dependencies {
		if spec, exists := rf.lockfileData.Specs[dep.Name]; exists {
			depKey := fmt.Sprintf("%s:%s", spec.Name, spec.Version)
			rf.dependencyGraph[projectKey] = append(rf.dependencyGraph[projectKey], depKey)
		}
	}

	// Build dependency relationships
	for _, spec := range rf.lockfileData.Specs {
		specKey := fmt.Sprintf("%s:%s", spec.Name, spec.Version)
		rf.dependencyGraph[specKey] = []string{}

		for depName := range spec.Dependencies {
			if depSpec, exists := rf.lockfileData.Specs[depName]; exists {
				depKey := fmt.Sprintf("%s:%s", depSpec.Name, depSpec.Version)
				rf.dependencyGraph[specKey] = append(rf.dependencyGraph[specKey], depKey)
			}
		}
	}
}

// findGemFilePath attempts to find the gem file path for checksum calculation
func (rf *RubyFlexPack) findGemFilePath(name, version string) string {
	// Try to find in Bundler cache directory
	cacheDir, err := rf.getBundlerCacheDirectory()
	if err != nil {
		log.Debug("Failed to get Bundler cache directory:", err)
		return ""
	}

	// Look for .gem files in cache
	gemFileName := fmt.Sprintf("%s-%s.gem", name, version)
	gemPath := filepath.Join(cacheDir, gemFileName)

	if _, err := os.Stat(gemPath); err == nil {
		return gemPath
	}

	// Try alternative gem installation directories
	return rf.findGemInInstallPath(name, version)
}

// getBundlerCacheDirectory returns the Bundler cache directory
func (rf *RubyFlexPack) getBundlerCacheDirectory() (string, error) {
	// Check if bundle config has a cache path set
	cmd := exec.Command("bundle", "config", "cache_path")
	output, err := cmd.Output()
	if err == nil {
		cachePath := strings.TrimSpace(string(output))
		if cachePath != "" && !strings.Contains(cachePath, "not set") {
			return cachePath, nil
		}
	}

	// Default Bundler cache locations
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	possiblePaths := []string{
		filepath.Join(rf.config.WorkingDirectory, "vendor", "cache"),
		filepath.Join(homeDir, ".bundle", "cache"),
		filepath.Join(homeDir, ".local", "share", "gem", "cache"),
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("bundler cache directory not found. Try running 'bundle install' or 'bundle cache'")
}

// findGemInInstallPath finds gem in Ruby installation paths
func (rf *RubyFlexPack) findGemInInstallPath(name, version string) string {
	// Get gem environment paths
	cmd := exec.Command("gem", "environment", "gemdir")
	output, err := cmd.Output()
	if err != nil {
		log.Debug("Failed to get gem directory:", err)
		return ""
	}

	gemDir := strings.TrimSpace(string(output))
	cachePath := filepath.Join(gemDir, "cache", fmt.Sprintf("%s-%s.gem", name, version))

	if _, err := os.Stat(cachePath); err == nil {
		return cachePath
	}

	// Try with gem show command to get exact path
	cmd = exec.Command("gem", "show", name, "-v", version, "--quiet")
	if output, err := cmd.Output(); err == nil {
		installPath := strings.TrimSpace(string(output))
		// Check for .gem file in parent cache directory
		if strings.Contains(installPath, "gems") {
			baseDir := filepath.Dir(filepath.Dir(installPath))
			cachePath := filepath.Join(baseDir, "cache", fmt.Sprintf("%s-%s.gem", name, version))
			if _, err := os.Stat(cachePath); err == nil {
				return cachePath
			}
		}
	}

	log.Debug("No gem file found for", name, version)
	return ""
}

// calculateFileChecksums calculates SHA1, SHA256, and MD5 checksums for a file using crypto.GetFileDetails
func (rf *RubyFlexPack) calculateFileChecksums(filePath string) (sha1Hash, sha256Hash, md5Hash string) {
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

// contains checks if a slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// RunBundleInstallWithBuildInfo runs bundle install and collects build information
func RunBundleInstallWithBuildInfo(workingDir string, buildName, buildNumber string, includeDevDeps bool, extraArgs []string) error {
	log.Info("Running Bundle install with build info collection...")

	// Create configuration
	config := PackageManagerConfig{
		WorkingDirectory:       workingDir,
		IncludeDevDependencies: includeDevDeps,
		ExtraArgs:              extraArgs,
	}

	// Create Ruby FlexPack instance
	rubyFlex, err := NewRubyFlexPack(config)
	if err != nil {
		return fmt.Errorf("failed to create Ruby FlexPack: %w", err)
	}

	// Run bundle install command
	args := append([]string{"install"}, extraArgs...)
	cmd := exec.Command("bundle", args...)
	cmd.Dir = workingDir
	log.Debug("Executing command:", cmd.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bundle install failed: %w\nOutput: %s", err, string(output))
	}
	log.Info("Bundle install completed successfully")

	// Collect build information if build name and number are provided
	if buildName != "" && buildNumber != "" {
		log.Info("Collecting build information...")
		// Parse dependencies
		depList := rubyFlex.ParseDependencyToList()
		log.Debug(fmt.Sprintf("Found %d dependencies", len(depList)))

		// Calculate checksums
		checksums := rubyFlex.CalculateChecksum()
		log.Debug(fmt.Sprintf("Calculated checksums for %d packages", len(checksums)))

		// Get dependency graph
		requestedBy := rubyFlex.CalculateRequestedBy()
		log.Debug(fmt.Sprintf("Built dependency graph with %d relationships", len(requestedBy)))

		log.Info("Build information collection completed")
		log.Debug("Build info collected successfully")
	}

	return nil
}
