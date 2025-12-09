package flexpack

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jfrog/gofrog/log"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"gopkg.in/yaml.v3"
)

const (
	ChartYaml = "Chart.yaml"
	ChartLock = "Chart.lock"
)

// HelmFlexPack implements the FlexPackManager interface for Helm package manager
type HelmFlexPack struct {
	config           HelmConfig
	dependencies     []DependencyInfo
	dependencyGraph  map[string][]string
	chartData        *HelmChartYAML
	lockData         *HelmChartLock
	cliPath          string
	cacheIndex       map[string]string
	repoAliases      map[string]string
	cacheDirectories []string
}

// HelmConfig represents configuration for Helm FlexPack
type HelmConfig struct {
	WorkingDirectory string
	HelmExecutable   string
}

// HelmChartYAML represents Chart.yaml structure
type HelmChartYAML struct {
	APIVersion   string                `yaml:"apiVersion"`
	Name         string                `yaml:"name"`
	Version      string                `yaml:"version"`
	Description  string                `yaml:"description,omitempty"`
	Type         string                `yaml:"type,omitempty"`
	Keywords     []string              `yaml:"keywords,omitempty"`
	Home         string                `yaml:"home,omitempty"`
	Sources      []string              `yaml:"sources,omitempty"`
	Dependencies []HelmChartDependency `yaml:"dependencies,omitempty"`
	Maintainers  []map[string]string   `yaml:"maintainers,omitempty"`
	Icon         string                `yaml:"icon,omitempty"`
	AppVersion   string                `yaml:"appVersion,omitempty"`
	Deprecated   bool                  `yaml:"deprecated,omitempty"`
	Annotations  map[string]string     `yaml:"annotations,omitempty"`
	KubeVersion  string                `yaml:"kubeVersion,omitempty"`
}

// HelmChartDependency represents a dependency in Chart.yaml
type HelmChartDependency struct {
	Name         string        `yaml:"name"`
	Version      string        `yaml:"version"`
	Repository   string        `yaml:"repository"`
	Condition    string        `yaml:"condition,omitempty"`
	Tags         []string      `yaml:"tags,omitempty"`
	Enabled      bool          `yaml:"enabled,omitempty"`
	ImportValues []interface{} `yaml:"import-values,omitempty"`
	Alias        string        `yaml:"alias,omitempty"`
}

// HelmChartLock represents Chart.lock structure
type HelmChartLock struct {
	Generated    string               `yaml:"generated"`
	Digest       string               `yaml:"digest"`
	Dependencies []HelmLockDependency `yaml:"dependencies"`
}

// HelmLockDependency represents a dependency in Chart.lock
type HelmLockDependency struct {
	Name       string   `yaml:"name"`
	Version    string   `yaml:"version"`
	Repository string   `yaml:"repository"`
	Digest     string   `yaml:"digest"`
	Condition  string   `yaml:"condition,omitempty"`
	Tags       []string `yaml:"tags,omitempty"`
}

// NewHelmFlexPack creates a new Helm FlexPack instance
func NewHelmFlexPack(config HelmConfig) (*HelmFlexPack, error) {
	hf := &HelmFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		cacheIndex:      make(map[string]string),
		repoAliases:     make(map[string]string),
	}

	// Auto-detect helm CLI path if not provided
	if config.HelmExecutable == "" {
		var err error
		hf.cliPath, err = hf.findHelmExecutable()
		if err != nil {
			return nil, fmt.Errorf("helm CLI not found: %w", err)
		}
	} else {
		hf.cliPath = config.HelmExecutable
	}

	// Validate working directory
	if config.WorkingDirectory == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		hf.config.WorkingDirectory = wd
	}

	// Load Chart.yaml
	if err := hf.loadChartYAML(); err != nil {
		return nil, fmt.Errorf("failed to load Chart.yaml: %w", err)
	}

	// Load Chart.lock
	if err := hf.loadChartLock(); err != nil {
		return nil, fmt.Errorf("failed to load Chart.lock: %w", err)
	}

	// Load cache directories and build cache index during initialization
	hf.cacheDirectories = hf.loadCacheDirectories()
	hf.buildCacheIndex()

	return hf, nil
}

// findHelmExecutable locates the helm CLI executable
func (hf *HelmFlexPack) findHelmExecutable() (string, error) {
	if runtime.GOOS == "windows" {
		path, err := exec.LookPath("helm.exe")
		if err == nil {
			return path, nil
		}
		// Fallback to helm (in case it's a symlink or wrapper)
		path, err = exec.LookPath("helm")
		if err == nil {
			return path, nil
		}
	} else {
		// On Unix-like systems, try helm
		path, err := exec.LookPath("helm")
		if err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("helm executable not found in PATH")
}

// loadChartYAML loads and parses Chart.yaml
func (hf *HelmFlexPack) loadChartYAML() error {
	chartPath := filepath.Join(hf.config.WorkingDirectory, ChartYaml)

	data, err := os.ReadFile(chartPath)
	if err != nil {
		return fmt.Errorf("failed to read Chart.yaml: %w", err)
	}

	hf.chartData = &HelmChartYAML{}
	if err := yaml.Unmarshal(data, hf.chartData); err != nil {
		return fmt.Errorf("failed to parse Chart.yaml: %w", err)
	}

	return nil
}

// loadChartLock loads and parses Chart.lock if it exists
func (hf *HelmFlexPack) loadChartLock() error {
	lockPath := filepath.Join(hf.config.WorkingDirectory, ChartLock)

	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read Chart.lock: %w", err)
	}

	hf.lockData = &HelmChartLock{}
	if err := yaml.Unmarshal(data, hf.lockData); err != nil {
		return fmt.Errorf("failed to parse Chart.lock: %w", err)
	}

	return nil
}

// getDependencies returns all dependencies (lazy loading)
func (hf *HelmFlexPack) getDependencies() []DependencyInfo {
	if len(hf.dependencies) > 0 {
		deps := hf.dependencies
		return deps
	}
	// Parse dependencies directly from Chart.yaml and Chart.lock
	hf.parseDependenciesFromChartYamlAndLockfile()
	return hf.dependencies
}

// getHelmRepoList gets and parses the output of 'helm repo list' command
func (hf *HelmFlexPack) getHelmRepoList() (map[string]string, error) {
	cmd := exec.Command(hf.cliPath, "repo", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("helm repo list failed: %w", err)
	}

	repoMap := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	headerSkipped := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Skip header line
		if !headerSkipped && strings.Contains(strings.ToUpper(line), "NAME") {
			headerSkipped = true
			continue
		}
		headerSkipped = true

		// Parse repo line: NAME    URL
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			alias := fields[0]
			url := fields[1]
			repoMap[alias] = url
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse helm repo list: %w", err)
	}

	return repoMap, nil
}

// resolveRepositoryAlias resolves repository alias (e.g., @redisLocation) to actual URL
func (hf *HelmFlexPack) resolveRepositoryAlias(dep *DependencyInfo) {
	if dep.Repository == "" {
		return
	}

	// Check if repository is an alias (starts with @)
	if !strings.HasPrefix(dep.Repository, "@") {
		return // Not an alias, already a URL
	}

	// Extract alias name (remove @ prefix)
	alias := strings.TrimPrefix(dep.Repository, "@")

	if len(hf.repoAliases) == 0 {
		repoMap, err := hf.getHelmRepoList()
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to get helm repo list: %v", err))
			return
		}
		hf.repoAliases = repoMap
	}

	// Resolve alias to URL
	if url, found := hf.repoAliases[alias]; found {
		dep.Repository = url
	} else {
		log.Warn(fmt.Sprintf("Repository alias '%s' not found in helm repo list", alias))
	}
}

// parseDependenciesFromFiles parses dependencies from Chart.yaml and Chart.lock (fallback)
func (hf *HelmFlexPack) parseDependenciesFromChartYamlAndLockfile() {
	if hf.chartData == nil {
		return
	}

	// Build maps of resolved versions and repositories from Chart.lock
	lockVersions := make(map[string]string)
	lockRepositories := make(map[string]string)
	if hf.lockData != nil {
		for _, lockDep := range hf.lockData.Dependencies {
			lockVersions[lockDep.Name] = lockDep.Version
			if lockDep.Repository != "" {
				lockRepositories[lockDep.Name] = lockDep.Repository
			}
		}
	}

	// Process dependencies from Chart.yaml
	for _, chartDep := range hf.chartData.Dependencies {
		dep := DependencyInfo{
			Name:       chartDep.Name,
			Version:    chartDep.Version,
			Type:       "helm",
			Repository: chartDep.Repository,
		}

		// Use resolved version from Chart.lock if available
		if resolvedVersion, found := lockVersions[chartDep.Name]; found {
			dep.Version = resolvedVersion
		}

		// Use resolved repository from Chart.lock if available (more accurate)
		if resolvedRepo, found := lockRepositories[chartDep.Name]; found {
			dep.Repository = resolvedRepo
		}

		dep.ID = fmt.Sprintf("%s:%s", dep.Name, dep.Version)

		// Resolve repository alias if needed
		hf.resolveRepositoryAlias(&dep)

		hf.dependencies = append(hf.dependencies, dep)
	}
}

// calculateChecksumWithFallback calculates checksums with multiple fallback strategies
func (hf *HelmFlexPack) calculateChecksumWithFallback(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
	}

	// Strategy 1: Try to find chart archive in cache
	if chartFile := hf.findChartFile(dep.Name, dep.Version); chartFile != "" {
		if Sha1, Sha256, Md5, err := hf.calculateFileChecksum(chartFile); err == nil {
			checksumMap["sha1"] = Sha1
			checksumMap["sha256"] = Sha256
			checksumMap["md5"] = Md5
			checksumMap["path"] = chartFile
			checksumMap["source"] = "cache-file"
			return checksumMap
		}
	}

	// Strategy 2: Try to find dependency chart in charts/ directory
	if chartFile := hf.findDependencyInChartsDir(dep.Name, dep.Version); chartFile != "" {
		if Sha1, Sha256, Md5, err := hf.calculateFileChecksum(chartFile); err == nil {
			checksumMap["sha1"] = Sha1
			checksumMap["sha256"] = Sha256
			checksumMap["md5"] = Md5
			checksumMap["path"] = chartFile
			checksumMap["source"] = "charts-directory"
			return checksumMap
		}
	}

	log.Warn(fmt.Sprintf("Could not find dependency: %s, version: %s, either in charts directory or cache", dep.Name, dep.Version))
	return checksumMap

}

// findChartFile searches for chart archive in Helm cache
func (hf *HelmFlexPack) findChartFile(name, version string) string {
	key := fmt.Sprintf("%s-%s", name, version)
	if path, found := hf.cacheIndex[key]; found {
		return path
	}
	return hf.searchCache(name, version)
}

// findDependencyInChartsDir searches for a dependency chart in the charts/ subdirectory
// Dependencies are stored as chart-name-version.tgz in the charts/ directory
func (hf *HelmFlexPack) findDependencyInChartsDir(name, version string) string {
	chartsDir := filepath.Join(hf.config.WorkingDirectory, "charts")
	if _, err := os.Stat(chartsDir); err != nil {
		return "" // charts/ directory doesn't exist
	}

	pattern := fmt.Sprintf("%s-%s.tgz", name, version)
	if foundPath := hf.findFileInDirectory(chartsDir, pattern); foundPath != "" {
		return foundPath
	}

	return ""
}

// buildCacheIndex builds an index of cached chart files
func (hf *HelmFlexPack) buildCacheIndex() {
	for _, cacheDir := range hf.cacheDirectories {
		cleanDir := filepath.Clean(cacheDir)
		if cleanDir == "" || cleanDir == "." || cleanDir == ".." {
			continue
		}

		// Ensure directory is absolute
		absDir, err := filepath.Abs(cleanDir)
		if err != nil {
			continue
		}

		// Validate that the absolute path doesn't contain traversal sequences
		if strings.Contains(absDir, "..") {
			continue
		}

		// Validate that the directory exists and is actually a directory
		dirInfo, err := os.Stat(absDir)
		if err != nil || !dirInfo.IsDir() {
			continue
		}

		err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err // Stop walking on error
			}
			if !d.IsDir() && strings.HasSuffix(path, ".tgz") {
				// Validate that the found path is within the cache directory
				absPath, err := filepath.Abs(path)
				if err != nil {
					return err
				}
				// Ensure the path is within the directory (prevent symlink attacks)
				relPath, err := filepath.Rel(absDir, absPath)
				if err != nil {
					return err
				}
				// Check for path traversal in relative path
				if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
					return fmt.Errorf("path traversal detected: %s", relPath)
				}

				// Format: chart-name-version.tgz
				baseName := filepath.Base(path)
				baseName = strings.TrimSuffix(baseName, ".tgz")

				// Try to extract version (last part after last dash)
				parts := strings.Split(baseName, "-")
				if len(parts) >= 2 {
					// Assume last segment is version
					version := parts[len(parts)-1]
					name := strings.Join(parts[:len(parts)-1], "-")

					key := fmt.Sprintf("%s-%s", name, version)
					hf.cacheIndex[key] = absPath
				}
			}
			return nil
		})
		if err != nil {
			return
		}
	}
}

// recursiveSearchCache performs recursive search for chart files
func (hf *HelmFlexPack) searchCache(name, version string) string {
	for _, cacheDir := range hf.cacheDirectories {
		if foundPath := hf.searchCacheDirectory(cacheDir, name, version); foundPath != "" {
			return foundPath
		}
	}
	return ""
}

// searchCacheDirectory searches for a chart file in a specific cache directory using version candidates
func (hf *HelmFlexPack) searchCacheDirectory(cacheDir, name string, version string) string {
	pattern := fmt.Sprintf("%s-%s.tgz", name, version)
	if foundPath := hf.findFileInDirectory(cacheDir, pattern); foundPath != "" {
		return foundPath
	}
	return ""
}

// findFileInDirectory searches for a file matching the pattern in a directory
// Validates inputs to prevent path traversal attacks
func (hf *HelmFlexPack) findFileInDirectory(dir, pattern string) string {
	// Sanitize and validate directory path to prevent path traversal
	cleanDir := filepath.Clean(dir)
	if cleanDir == "" || cleanDir == "." {
		return ""
	}

	// Ensure directory is absolute to prevent relative path issues
	absDir, err := filepath.Abs(cleanDir)
	if err != nil {
		return ""
	}

	// Validate that the directory exists and is actually a directory
	dirInfo, err := os.Stat(absDir)
	if err != nil || !dirInfo.IsDir() {
		return ""
	}

	// Validate pattern to prevent path traversal
	// Pattern should only be a filename, not a path
	cleanPattern := filepath.Clean(pattern)
	if cleanPattern == "" || cleanPattern == "." || cleanPattern == ".." {
		return ""
	}

	// Ensure pattern doesn't contain path separators (should be filename only)
	if strings.Contains(cleanPattern, string(os.PathSeparator)) {
		return ""
	}

	// Prevent patterns that could be used for traversal
	if strings.HasPrefix(cleanPattern, "..") {
		return ""
	}

	var foundPath string
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) == cleanPattern {
			// Validate that the found path is within the original directory
			absPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			// Ensure the path is within the directory (prevent symlink attacks)
			relPath, err := filepath.Rel(absDir, absPath)
			if err != nil {
				return err
			}
			// Check for path traversal in relative path
			if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
				return fmt.Errorf("path traversal detected: %s", relPath)
			}
			foundPath = absPath
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return ""
	}
	return foundPath
}

// loadCacheDirectories loads and returns Helm cache directory paths
func (hf *HelmFlexPack) loadCacheDirectories() []string {
	var paths []string

	// Environment variable override (highest priority)
	if envPath := os.Getenv("HELM_REPOSITORY_CACHE"); envPath != "" {
		// Sanitize the path
		cleanPath := filepath.Clean(envPath)
		if cleanPath != "" && cleanPath != "." && cleanPath != ".." {
			// Convert to absolute path to prevent relative path issues
			absPath, err := filepath.Abs(cleanPath)
			if err == nil && absPath != "" {
				// Additional validation: ensure path doesn't contain traversal sequences
				// Even after Clean(), check for any remaining ".." components
				if !strings.Contains(absPath, "..") {
					paths = append(paths, absPath)
				}
			}
		}
	}

	// Platform-specific paths (these are safe as they're constructed from trusted sources)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		switch runtime.GOOS {
		case "windows":
			paths = append(paths,
				filepath.Join(homeDir, "AppData", "Local", "helm", "repository", "cache"),
			)
		case "darwin": // macOS
			paths = append(paths,
				filepath.Join(homeDir, "Library", "Caches", "helm", "repository"),
				filepath.Join(homeDir, ".cache", "helm", "repository"), // XDG fallback
			)
		default: // Linux and others
			paths = append(paths,
				filepath.Join(homeDir, ".cache", "helm", "repository"),
				filepath.Join(homeDir, ".local", "share", "helm", "repository"),
			)
		}
	}

	// Filter to only existing paths and validate each path
	var existingPaths []string
	for _, path := range paths {
		// Sanitize each path before checking
		cleanPath := filepath.Clean(path)
		if cleanPath == "" || cleanPath == "." || cleanPath == ".." {
			continue
		}

		// Convert to absolute path
		absPath, err := filepath.Abs(cleanPath)
		if err != nil {
			continue
		}

		// Validate that the absolute path doesn't contain traversal sequences
		if strings.Contains(absPath, "..") {
			continue
		}

		// Check if path exists and is a directory
		pathInfo, err := os.Stat(absPath)
		if err == nil && pathInfo.IsDir() {
			existingPaths = append(existingPaths, absPath)
		}
	}

	return existingPaths
}

// calculateFileChecksum calculates checksums for a file
func (hf *HelmFlexPack) calculateFileChecksum(filePath string) (string, string, string, error) {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return "", "", "", err
	}
	if fileDetails == nil {
		return "", "", "", fmt.Errorf("fileDetails is nil for file: %s", filePath)
	}
	return fileDetails.Checksum.Sha1,
		fileDetails.Checksum.Sha256,
		fileDetails.Checksum.Md5,
		nil
}

// CollectBuildInfo collects complete build information for Helm chart
func (hf *HelmFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if len(hf.dependencies) == 0 {
		hf.getDependencies()
	}
	properties := make(map[string]string)
	if hf.chartData != nil {
		if hf.chartData.Type != "" {
			properties["helm.chart.type"] = hf.chartData.Type
		}
		if hf.chartData.AppVersion != "" {
			properties["helm.chart.appVersion"] = hf.chartData.AppVersion
		}
		if hf.chartData.Description != "" {
			properties["helm.chart.description"] = hf.chartData.Description
		}
	}
	dependencies := make([]entities.Dependency, 0, len(hf.dependencies))
	for _, dep := range hf.dependencies {
		dependency := hf.createDependencyChecksum(dep)
		dependencies = append(dependencies, dependency)
	}
	module := entities.Module{
		Id:           fmt.Sprintf("%s:%s", hf.chartData.Name, hf.chartData.Version),
		Type:         "helm",
		Properties:   properties,
		Dependencies: dependencies,
	}
	return &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "build-info-go",
			Version: "1.0.0",
		},
		BuildAgent: &entities.Agent{
			Name:    "Helm",
			Version: hf.getHelmVersion(),
		},
		Modules: []entities.Module{module},
	}, nil
}

// createDependencyEntity creates an entities.Dependency from DependencyInfo
func (hf *HelmFlexPack) createDependencyChecksum(dep DependencyInfo) entities.Dependency {
	checksumMap := hf.calculateChecksumWithFallback(dep)
	checksum := entities.Checksum{}
	dependency := entities.Dependency{}
	if checksumMap == nil {
		// Even if checksum is nil, we should still set ID and Repository
		dependency.Id = dep.ID
		dependency.Repository = dep.Repository
		return dependency
	}
	if Sha1, ok := checksumMap["sha1"].(string); ok && Sha1 != "" {
		checksum.Sha1 = Sha1
	}
	if Sha256, ok := checksumMap["sha256"].(string); ok && Sha256 != "" {
		checksum.Sha256 = Sha256
	}
	if Md5, ok := checksumMap["md5"].(string); ok && Md5 != "" {
		checksum.Md5 = Md5
	}
	dependency.Id = dep.ID
	dependency.Repository = dep.Repository
	dependency.Checksum = checksum
	return dependency
}

// getHelmVersion gets the Helm version
func (hf *HelmFlexPack) getHelmVersion() string {
	cmd := exec.Command(hf.cliPath, "version", "--short")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown"
	}
	versionStr := strings.TrimSpace(string(output))
	// Remove 'v' prefix if present
	versionStr = strings.TrimPrefix(versionStr, "v")
	// Remove any build metadata after '+'
	if idx := strings.Index(versionStr, "+"); idx != -1 {
		versionStr = versionStr[:idx]
	}
	return versionStr
}
