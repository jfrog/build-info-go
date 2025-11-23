package flexpack

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/md5"  // #nosec G501 // MD5 required for Artifactory build info compatibility
	"crypto/sha1" // #nosec G505 // SHA1 required for Artifactory build info compatibility
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

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
	config          HelmConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	chartData       *HelmChartYAML
	lockData        *HelmChartLock
	cliPath         string
	cacheIndex      map[string]string
	cacheIndexBuilt bool
	mu              sync.RWMutex
}

// HelmConfig represents configuration for Helm FlexPack
type HelmConfig struct {
	WorkingDirectory        string
	IncludeTestDependencies bool // Helm doesn't have test deps, but kept for interface consistency
	HelmExecutable          string
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

	// Load Chart.lock if it exists (optional)
	_ = hf.loadChartLock() // Ignore error if lock file doesn't exist

	return hf, nil
}

// findHelmExecutable locates the helm CLI executable
func (hf *HelmFlexPack) findHelmExecutable() (string, error) {
	candidates := []string{"helm"}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "helm.exe")
	}

	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
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
		if os.IsNotExist(err) {
			return nil // Chart.lock is optional
		}
		return fmt.Errorf("failed to read Chart.lock: %w", err)
	}

	hf.lockData = &HelmChartLock{}
	if err := yaml.Unmarshal(data, hf.lockData); err != nil {
		return fmt.Errorf("failed to parse Chart.lock: %w", err)
	}

	return nil
}

// GetDependency returns a human-readable summary of dependencies
func (hf *HelmFlexPack) GetDependency() string {
	deps := hf.getDependencies()
	if len(deps) == 0 {
		return "No dependencies"
	}

	var parts []string
	for _, dep := range deps {
		parts = append(parts, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return strings.Join(parts, ", ")
}

// ParseDependencyToList returns a simple list of dependency IDs
func (hf *HelmFlexPack) ParseDependencyToList() []string {
	deps := hf.getDependencies()
	result := make([]string, 0, len(deps))
	for _, dep := range deps {
		result = append(result, dep.ID)
	}
	return result
}

// CalculateChecksum calculates checksums for all dependencies
func (hf *HelmFlexPack) CalculateChecksum() []map[string]interface{} {
	deps := hf.getDependencies()
	result := make([]map[string]interface{}, 0, len(deps))

	for _, dep := range deps {
		checksumMap := hf.calculateChecksumWithFallback(dep)
		if checksumMap != nil {
			result = append(result, checksumMap)
		}
	}

	return result
}

// CalculateScopes returns available scopes for dependencies
func (hf *HelmFlexPack) CalculateScopes() []string {
	scopes := make(map[string]bool)

	deps := hf.getDependencies()
	for _, dep := range deps {
		for _, scope := range dep.Scopes {
			scopes[scope] = true
		}
	}

	result := make([]string, 0, len(scopes))
	for scope := range scopes {
		result = append(result, scope)
	}

	// Default to runtime if no scopes found
	if len(result) == 0 {
		result = append(result, "runtime")
	}

	return result
}

// CalculateRequestedBy calculates the dependency graph (which dependencies request which)
// It also populates the RequestedBy field directly on DependencyInfo structs
func (hf *HelmFlexPack) CalculateRequestedBy() map[string][]string {
	hf.buildDependencyGraph()
	requestedBy := make(map[string][]string)
	// Invert the dependency graph
	for parent, children := range hf.dependencyGraph {
		for _, child := range children {
			// Prevent self-references (A->A is not allowed)
			if parent == child {
				continue
			}
			if requestedBy[child] == nil {
				requestedBy[child] = []string{}
			}
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	// Deduplicate and sort
	for child, requesters := range requestedBy {
		requestedBy[child] = hf.deduplicateAndSort(requesters)
	}
	// Populate RequestedBy field directly on DependencyInfo structs
	hf.populateRequestedByOnDependencies(requestedBy)
	return requestedBy
}

// populateRequestedByOnDependencies populates the RequestedBy field on DependencyInfo structs
func (hf *HelmFlexPack) populateRequestedByOnDependencies(requestedByMap map[string][]string) {
	for i := range hf.dependencies {
		if requesters, exists := requestedByMap[hf.dependencies[i].ID]; exists && len(requesters) > 0 {
			hf.dependencies[i].RequestedBy = requesters
		}
	}
}

// getDependencies returns all dependencies (lazy loading)
func (hf *HelmFlexPack) getDependencies() []DependencyInfo {
	hf.mu.RLock()
	if len(hf.dependencies) > 0 {
		deps := hf.dependencies
		hf.mu.RUnlock()
		return deps
	}
	hf.mu.RUnlock()
	hf.mu.Lock()
	defer hf.mu.Unlock()
	// Double-check after acquiring write lock
	if len(hf.dependencies) > 0 {
		return hf.dependencies
	}
	// Resolve dependencies using hybrid approach
	if err := hf.resolveDependencies(); err != nil {
		// Fallback to file-based parsing if helm dependency list fails
		hf.parseDependenciesFromFiles()
	}
	return hf.dependencies
}

// resolveDependencies uses CLI to resolve dependencies (primary strategy)
func (hf *HelmFlexPack) resolveDependencies() error {
	// Try using helm dependency list command
	cmd := exec.Command(hf.cliPath, "dependency", "list")
	cmd.Dir = hf.config.WorkingDirectory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm dependency list failed: %w", err)
	}
	return hf.parseHelmDependencyList(string(output))
}

// parseHelmDependencyList parses output from `helm dependency list`
// Only includes dependencies that are declared in the current chart's Chart.yaml
func (hf *HelmFlexPack) parseHelmDependencyList(output string) error {
	scanner := bufio.NewScanner(strings.NewReader(output))
	headerSkipped := false
	// Build a set of valid dependency names from Chart.yaml to filter out dependencies from other directories
	validDependencyNames := hf.getValidDependencyNames()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !headerSkipped && hf.isHeaderLine(line) {
			headerSkipped = true
			continue
		}
		headerSkipped = true
		if dep := hf.parseDependencyLine(line); dep != nil {
			// Only include dependencies that are declared in the current chart's Chart.yaml
			if !hf.isValidDependency(dep.Name, validDependencyNames) {
				continue
			}
			hf.dependencies = append(hf.dependencies, *dep)
		}
	}
	return scanner.Err()
}

// getValidDependencyNames returns a set of dependency names declared in Chart.yaml
func (hf *HelmFlexPack) getValidDependencyNames() map[string]bool {
	validNames := make(map[string]bool)
	if hf.chartData == nil {
		return validNames
	}
	for _, dep := range hf.chartData.Dependencies {
		validNames[dep.Name] = true
	}
	return validNames
}

// isValidDependency checks if a dependency name is declared in the current chart's Chart.yaml
func (hf *HelmFlexPack) isValidDependency(depName string, validNames map[string]bool) bool {
	// Only include dependencies that are explicitly declared in Chart.yaml
	// This filters out dependencies from other directories that helm dependency list might return
	return validNames[depName]
}

// isHeaderLine checks if a line is the header line
func (hf *HelmFlexPack) isHeaderLine(line string) bool {
	return strings.Contains(line, "NAME")
}

// parseDependencyLine parses a single dependency line from helm dependency list output
// Format: NAME    VERSION    REPOSITORY    STATUS
func (hf *HelmFlexPack) parseDependencyLine(line string) *DependencyInfo {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil
	}
	return &DependencyInfo{
		Name:    fields[0],
		Version: fields[1],
		Type:    "helm",
		ID:      fmt.Sprintf("%s:%s", fields[0], fields[1]),
	}
}

// parseDependenciesFromFiles parses dependencies from Chart.yaml and Chart.lock (fallback)
func (hf *HelmFlexPack) parseDependenciesFromFiles() {
	if hf.chartData == nil {
		return
	}

	// Build a map of resolved versions from Chart.lock
	lockVersions := make(map[string]string)
	if hf.lockData != nil {
		for _, lockDep := range hf.lockData.Dependencies {
			lockVersions[lockDep.Name] = lockDep.Version
		}
	}

	// Process dependencies from Chart.yaml
	for _, chartDep := range hf.chartData.Dependencies {
		dep := DependencyInfo{
			Name:    chartDep.Name,
			Version: chartDep.Version,
			Type:    "helm",
		}

		// Use resolved version from Chart.lock if available
		if resolvedVersion, found := lockVersions[chartDep.Name]; found {
			dep.Version = resolvedVersion
		}

		dep.ID = fmt.Sprintf("%s:%s", dep.Name, dep.Version)

		hf.dependencies = append(hf.dependencies, dep)
	}
}

// buildDependencyGraph constructs the dependency graph
func (hf *HelmFlexPack) buildDependencyGraph() {
	if len(hf.dependencyGraph) > 0 {
		return
	}
	// Try to recursively build graph from cached charts first
	// This captures transitive dependencies by reading Chart.yaml/Chart.lock from cached .tgz files
	hf.buildDependencyGraphRecursively()
	// Final fallback: build flat graph from Chart.yaml (only direct dependencies)
	if len(hf.dependencyGraph) == 0 {
		chartName := hf.chartData.Name
		if chartName == "" {
			chartName = "root"
		}
		for _, dep := range hf.dependencies {
			hf.dependencyGraph[chartName] = append(hf.dependencyGraph[chartName], dep.ID)
		}
	}
}

// buildDependencyGraphRecursively builds a dependency graph by recursively parsing Chart.yaml/Chart.lock
// from cached charts. This captures transitive dependencies.
func (hf *HelmFlexPack) buildDependencyGraphRecursively() {
	if hf.chartData == nil {
		return
	}
	parentChartID := fmt.Sprintf("%s:%s", hf.chartData.Name, hf.chartData.Version)
	visited := make(map[string]bool)
	visited[parentChartID] = true
	// For each direct dependency, recursively build its dependency graph
	for _, dep := range hf.dependencies {
		// Add as direct dependency of parent chart
		hf.dependencyGraph[parentChartID] = append(hf.dependencyGraph[parentChartID], dep.ID)
		// Recursively build graph for this dependency
		hf.buildDependencyGraphForDep(dep.Name, dep.Version, visited, 10)
	}
}

// buildDependencyGraphForDep recursively builds dependency graph for a specific dependency
func (hf *HelmFlexPack) buildDependencyGraphForDep(depName, depVersion string, visited map[string]bool, maxDepth int) {
	if maxDepth <= 0 {
		return
	}
	depID := fmt.Sprintf("%s:%s", depName, depVersion)
	if visited[depID] {
		return // Already visited, avoid infinite loops
	}
	visited[depID] = true
	// Find cached chart
	cachedChartPath := hf.findChartFile(depName, depVersion)
	if cachedChartPath == "" {
		return // Chart not in cache, can't build transitive graph
	}
	// Read dependencies from cached chart
		childDeps, err := hf.getDependenciesFromCachedChart(cachedChartPath, depName)
	if err != nil {
		return // Failed to read dependencies
	}
	// Add child dependencies to graph
	for _, childDep := range childDeps {
		hf.dependencyGraph[depID] = append(hf.dependencyGraph[depID], childDep.ID)
		// Recursively build graph for child dependencies
		hf.buildDependencyGraphForDep(childDep.Name, childDep.Version, visited, maxDepth-1)
	}
}

// extractResult holds the result of chart extraction including the chart directory and cleanup error
type extractResult struct {
	chartDir  string
	removeErr error
}

// getDependenciesFromCachedChart reads dependencies from a cached chart file by extracting and parsing Chart.yaml/Chart.lock
func (hf *HelmFlexPack) getDependenciesFromCachedChart(chartPath, chartName string) ([]DependencyInfo, error) {
	result, err := hf.extractChartToTemp(chartPath)
	if err != nil {
		return nil, errors.Join(err, result.removeErr)
	}
	chartYAML, err := hf.readChartYAMLFromDir(result.chartDir)
	if err != nil {
		return nil, errors.Join(err, result.removeErr)
	}

	lockVersions := hf.readLockVersionsFromDir(result.chartDir)
	deps := hf.buildDependenciesFromChartYAML(chartYAML, lockVersions)

	if result.removeErr != nil {
		return deps, result.removeErr
	}
	return deps, nil
}

// extractChartToTemp extracts a chart archive to a temporary directory and returns the chart directory path
func (hf *HelmFlexPack) extractChartToTemp(chartPath string) (*extractResult, error) {
	tempDir, err := os.MkdirTemp("", "helm-chart-*")
	if err != nil {
		return &extractResult{}, fmt.Errorf("failed to create temp directory: %w", err)
	}
	result := &extractResult{}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			result.removeErr = fmt.Errorf("failed to remove temp directory: %w", err)
		}
	}()
	if err := hf.extractTgz(chartPath, tempDir); err != nil {
		return result, fmt.Errorf("failed to extract chart archive: %w", err)
	}
	chartDir := hf.findChartDirectoryInExtracted(tempDir)
	if chartDir == "" {
		return result, fmt.Errorf("could not find chart directory in extracted archive")
	}
	result.chartDir = chartDir
	return result, nil
}

// chartYAMLData represents the structure of Chart.yaml dependencies
type chartYAMLData struct {
	Dependencies []struct {
		Name       string   `yaml:"name"`
		Version    string   `yaml:"version"`
		Repository string   `yaml:"repository"`
		Condition  string   `yaml:"condition,omitempty"`
		Tags       []string `yaml:"tags,omitempty"`
	} `yaml:"dependencies"`
}

// readChartYAMLFromDir reads and parses Chart.yaml from a directory
func (hf *HelmFlexPack) readChartYAMLFromDir(chartDir string) (*chartYAMLData, error) {
	chartYamlPath := filepath.Join(chartDir, ChartYaml)
	chartYamlData, err := os.ReadFile(chartYamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Chart.yaml: %w", err)
	}
	var chartYAML chartYAMLData
	if err := yaml.Unmarshal(chartYamlData, &chartYAML); err != nil {
		return nil, fmt.Errorf("failed to parse Chart.yaml: %w", err)
	}
	return &chartYAML, nil
}

// readLockVersionsFromDir reads Chart.lock and returns a map of dependency names to resolved versions
func (hf *HelmFlexPack) readLockVersionsFromDir(chartDir string) map[string]string {
	lockVersions := make(map[string]string)
	chartLockPath := filepath.Join(chartDir, ChartLock)
	lockData, err := os.ReadFile(chartLockPath)
	if err != nil {
		return lockVersions // Chart.lock is optional
	}
	var chartLock struct {
		Dependencies []struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(lockData, &chartLock); err != nil {
		return lockVersions // Ignore parse errors for Chart.lock
	}
	for _, lockDep := range chartLock.Dependencies {
		lockVersions[lockDep.Name] = lockDep.Version
	}
	return lockVersions
}

// buildDependenciesFromChartYAML builds a list of DependencyInfo from Chart.yaml data and lock versions
func (hf *HelmFlexPack) buildDependenciesFromChartYAML(chartYAML *chartYAMLData, lockVersions map[string]string) []DependencyInfo {
	deps := make([]DependencyInfo, 0, len(chartYAML.Dependencies))
	for _, dep := range chartYAML.Dependencies {
		version := dep.Version
		// Use resolved version from Chart.lock if available
		if resolvedVersion, found := lockVersions[dep.Name]; found {
			version = resolvedVersion
		}
		deps = append(deps, DependencyInfo{
			ID:      fmt.Sprintf("%s:%s", dep.Name, version),
			Name:    dep.Name,
			Version: version,
			Type:    "helm",
		})
	}
	return deps
}

// extractTgz extracts a .tgz file to a destination directory
func (hf *HelmFlexPack) extractTgz(tgzPath, destDir string) (err error) {
	file, err := os.Open(tgzPath)
	if err != nil {
		return fmt.Errorf("failed to open .tgz file: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		err = errors.Join(err, gzReader.Close())
	}()
	tarReader := tar.NewReader(gzReader)
	return hf.extractTarEntries(tarReader, destDir)
}

// extractTarEntries extracts all entries from a tar reader to the destination directory
func (hf *HelmFlexPack) extractTarEntries(tarReader *tar.Reader, destDir string) error {
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}
		if err := hf.extractTarEntry(header, tarReader, destDir); err != nil {
			return err
		}
	}
	return nil
}

// extractTarEntry extracts a single entry from a tar archive
func (hf *HelmFlexPack) extractTarEntry(header *tar.Header, tarReader *tar.Reader, destDir string) error {
	// Validate path to prevent directory traversal attacks
	cleanName := filepath.Clean(header.Name)
	if cleanName == ".." || len(cleanName) >= 3 && cleanName[0:3] == "../" {
		return fmt.Errorf("invalid path in archive: %s", header.Name)
	}
	// Ensure the cleaned path is still within destDir
	targetPath := filepath.Join(destDir, cleanName)
	destDirClean := filepath.Clean(destDir)
	if !strings.HasPrefix(targetPath, destDirClean+string(os.PathSeparator)) && targetPath != destDirClean {
		return fmt.Errorf("path traversal detected: %s", header.Name)
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return hf.createDirectory(targetPath, header.Mode)
	case tar.TypeReg:
		return hf.extractRegularFile(targetPath, header, tarReader)
	default:
		// Skip unsupported entry types (symlinks, etc.)
		return nil
	}
}

// createDirectory creates a directory with the specified mode
func (hf *HelmFlexPack) createDirectory(path string, mode int64) error {
	// Validate mode to prevent integer overflow (os.FileMode is uint32)
	const maxFileMode = 0777
	var fileMode os.FileMode
	if mode < 0 || mode > maxFileMode {
		fileMode = 0755 // Use safe default
	} else {
		fileMode = os.FileMode(mode) // #nosec G115 // Mode is validated to be within uint32 range
	}
	if err := os.MkdirAll(path, fileMode); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

// extractRegularFile extracts a regular file from tar archive
func (hf *HelmFlexPack) extractRegularFile(targetPath string, header *tar.Header, tarReader *tar.Reader) (err error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	// Validate mode to prevent integer overflow (os.FileMode is uint32)
	mode := header.Mode
	const maxFileMode = 0777
	var fileMode os.FileMode
	if mode < 0 || mode > maxFileMode {
		fileMode = 0644 // Use safe default
	} else {
		fileMode = os.FileMode(mode) // #nosec G115 // Mode is validated to be within uint32 range
	}
	outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		err = errors.Join(err, outFile.Close())
	}()
	if _, err := io.Copy(outFile, tarReader); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// findChartDirectoryInExtracted finds the chart directory inside an extracted archive
func (hf *HelmFlexPack) findChartDirectoryInExtracted(extractedDir string) string {
	var chartDir string
	err := filepath.WalkDir(extractedDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err // Stop walking on error
		}
		if d.IsDir() {
			// Check if this directory contains Chart.yaml
			chartYamlPath := filepath.Join(path, ChartYaml)
			if _, err := os.Stat(chartYamlPath); err == nil {
				chartDir = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return ""
	}
	return chartDir
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
	// When helm dependency build/update is run, dependencies are stored in charts/ subdirectory
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

	// Strategy 3: Generate manifest-based checksum
	Sha1, Sha256, Md5 := hf.calculateManifestChecksum(dep)
	checksumMap["sha1"] = Sha1
	checksumMap["sha256"] = Sha256
	checksumMap["md5"] = Md5
	checksumMap["path"] = "manifest"
	checksumMap["source"] = "manifest"
	return checksumMap

	// Strategy 4: Empty checksums (graceful degradation)
	checksumMap["sha1"] = ""
	checksumMap["sha256"] = ""
	checksumMap["md5"] = ""
	checksumMap["path"] = ""
	checksumMap["source"] = "unavailable"

	return checksumMap
}

// findChartFile searches for chart archive in Helm cache
func (hf *HelmFlexPack) findChartFile(name, version string) string {
	// Build cache index if not already built
	if !hf.cacheIndexBuilt {
		hf.buildCacheIndex()
		hf.cacheIndexBuilt = true
	}

	// Try multiple version formats
	versionCandidates := hf.generateVersionCandidates(version)

	for _, v := range versionCandidates {
		key := fmt.Sprintf("%s-%s", name, v)
		if path, found := hf.cacheIndex[key]; found {
			return path
		}
	}

	// Fallback: recursive search
	return hf.recursiveSearchCache(name, version)
}

// findDependencyInChartsDir searches for a dependency chart in the charts/ subdirectory
// Dependencies are stored as chart-name-version.tgz in the charts/ directory
func (hf *HelmFlexPack) findDependencyInChartsDir(name, version string) string {
	chartsDir := filepath.Join(hf.config.WorkingDirectory, "charts")
	if _, err := os.Stat(chartsDir); err != nil {
		return "" // charts/ directory doesn't exist
	}

	// Try multiple version formats
	versionCandidates := hf.generateVersionCandidates(version)
	for _, v := range versionCandidates {
		pattern := fmt.Sprintf("%s-%s.tgz", name, v)
		if foundPath := hf.findFileInDirectory(chartsDir, pattern); foundPath != "" {
			return foundPath
		}
	}

	return ""
}

// buildCacheIndex builds an index of cached chart files
func (hf *HelmFlexPack) buildCacheIndex() {
	cacheDirs := hf.getCacheDirectories()
	for _, cacheDir := range cacheDirs {
		err := filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err // Stop walking on error
			}
			if !d.IsDir() && strings.HasSuffix(path, ".tgz") {
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
					hf.cacheIndex[key] = path
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
func (hf *HelmFlexPack) recursiveSearchCache(name, version string) string {
	cacheDirs := hf.getCacheDirectories()
	versionCandidates := hf.generateVersionCandidates(version)
	for _, cacheDir := range cacheDirs {
		if foundPath := hf.searchCacheDirectory(cacheDir, name, versionCandidates); foundPath != "" {
			return foundPath
		}
	}
	return ""
}

// generateVersionCandidates generates all possible version format variations
func (hf *HelmFlexPack) generateVersionCandidates(version string) []string {
	return []string{
		strings.TrimPrefix(version, "v"),       // "1.2.3" from "v1.2.3"
		version,                                // "v1.2.3" as-is
		"v" + strings.TrimPrefix(version, "v"), // Ensure "v" prefix
	}
}

// searchCacheDirectory searches for a chart file in a specific cache directory using version candidates
func (hf *HelmFlexPack) searchCacheDirectory(cacheDir, name string, versionCandidates []string) string {
	for _, v := range versionCandidates {
		pattern := fmt.Sprintf("%s-%s.tgz", name, v)
		if foundPath := hf.findFileInDirectory(cacheDir, pattern); foundPath != "" {
			return foundPath
		}
	}
	return ""
}

// findFileInDirectory searches for a file matching the pattern in a directory
func (hf *HelmFlexPack) findFileInDirectory(dir, pattern string) string {
	var foundPath string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err // Stop walking on error
		}
		if !d.IsDir() && filepath.Base(path) == pattern {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return ""
	}
	return foundPath
}

// getCacheDirectories returns Helm cache directory paths
func (hf *HelmFlexPack) getCacheDirectories() []string {
	var paths []string
	// Environment variable override (highest priority)
	if envPath := os.Getenv("HELM_REPOSITORY_CACHE"); envPath != "" {
		paths = append(paths, envPath)
	}
	// Platform-specific paths
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
	// Filter to only existing paths
	var existingPaths []string
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			existingPaths = append(existingPaths, path)
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

// calculateManifestChecksum generates checksum from dependency metadata
func (hf *HelmFlexPack) calculateManifestChecksum(dep DependencyInfo) (string, string, string) {
	// Create deterministic manifest content using only basic dependency info
	// Metadata (repository, condition, tags) is not included in build info output,
	// so we don't need it for checksum calculation either
	manifest := fmt.Sprintf("name:%s\nversion:%s\ntype:%s\n",
		dep.Name, dep.Version, dep.Type)
	// Calculate checksums
	// Note: MD5 and SHA1 are weak cryptographic primitives but required for Artifactory build info compatibility
	sha1Sum := fmt.Sprintf("%x", sha1.Sum([]byte(manifest)))   // #nosec G401 // Required for Artifactory compatibility
	sha256Sum := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	md5Sum := fmt.Sprintf("%x", md5.Sum([]byte(manifest))) // #nosec G401 // Required for Artifactory compatibility
	return sha1Sum, sha256Sum, md5Sum
}

// deduplicateAndSort removes duplicates and sorts a string slice
func (hf *HelmFlexPack) deduplicateAndSort(items []string) []string {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

// CollectBuildInfo collects complete build information for Helm chart
func (hf *HelmFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if len(hf.dependencies) == 0 {
		hf.getDependencies()
	}

	// Calculate and populate RequestedBy on dependencies
	hf.CalculateRequestedBy()

	dependencies := hf.convertDependenciesToEntities()
	buildInfo := hf.createBuildInfo(buildName, buildNumber, dependencies)

	return buildInfo, nil
}

// convertDependenciesToEntities converts DependencyInfo slice to entities.Dependency slice
func (hf *HelmFlexPack) convertDependenciesToEntities() []entities.Dependency {
	dependencies := make([]entities.Dependency, 0, len(hf.dependencies))
	for _, dep := range hf.dependencies {
		entity := hf.createDependencyEntity(dep)
		dependencies = append(dependencies, entity)
	}
	return dependencies
}

// createDependencyEntity creates an entities.Dependency from DependencyInfo
func (hf *HelmFlexPack) createDependencyEntity(dep DependencyInfo) entities.Dependency {
	checksum := hf.convertChecksumMapToEntity(hf.calculateChecksumWithFallback(dep))

	entity := entities.Dependency{
		Id:       dep.ID,
		Type:     dep.Type,
		Checksum: checksum,
	}

	// Add requested-by relationships from DependencyInfo.RequestedBy field
	if len(dep.RequestedBy) > 0 {
		entity.RequestedBy = [][]string{dep.RequestedBy}
	}

	return entity
}

// convertChecksumMapToEntity converts checksum map to entities.Checksum struct
func (hf *HelmFlexPack) convertChecksumMapToEntity(checksumMap map[string]interface{}) entities.Checksum {
	checksum := entities.Checksum{}
	if checksumMap == nil {
		return checksum
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
	return checksum
}

// createBuildInfo creates the build info structure with module
func (hf *HelmFlexPack) createBuildInfo(buildName, buildNumber string, dependencies []entities.Dependency) *entities.BuildInfo {
	properties := make(map[string]string)
	// Add chart metadata from Chart.yaml if available
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
	}
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
