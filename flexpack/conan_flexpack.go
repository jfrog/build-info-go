package flexpack

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// ConanFlexPack implements the FlexPackManager interface for Conan package manager
// Following the same pattern as MavenFlexPack for consistency
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

// ConanConfig represents configuration for Conan FlexPack
// Mirrors MavenConfig structure
type ConanConfig struct {
	WorkingDirectory string
	ConanExecutable  string
	Profile          string
	Settings         map[string]string
	Options          map[string]string
}

// ConanGraphOutput represents the output of 'conan graph info --format=json'
type ConanGraphOutput struct {
	Graph struct {
		Nodes map[string]ConanGraphNode `json:"nodes"`
	} `json:"graph"`
	RootRef string `json:"root_ref"`
}

// ConanGraphNode represents a node in the Conan dependency graph
type ConanGraphNode struct {
	Ref          string                         `json:"ref"`
	DisplayName  string                         `json:"display_name"`
	Context      string                         `json:"context"`
	Dependencies map[string]ConanDependencyEdge `json:"dependencies"`
	Settings     map[string]string              `json:"settings"`
	Options      map[string]string              `json:"options"`
	Path         string                         `json:"path"`
	PackageId    string                         `json:"package_id"`
	Revision     string                         `json:"rrev"`
	Binary       string                         `json:"binary"`
	Name         string                         `json:"name"`
	Version      string                         `json:"version"`
}

// ConanDependencyEdge represents an edge in the dependency graph
type ConanDependencyEdge struct {
	Ref     string `json:"ref"`
	Require string `json:"require"`
	Build   bool   `json:"build"`
	Test    bool   `json:"test"`
	Direct  bool   `json:"direct"`
	Run     bool   `json:"run"`
	Visible bool   `json:"visible"`
}

// ConanLockFile represents the structure of conan.lock file
type ConanLockFile struct {
	Version        string                   `json:"version"`
	Requires       []string                 `json:"requires"`
	BuildRequires  []string                 `json:"build_requires"`
	PythonRequires []string                 `json:"python_requires"`
	Graph          map[string]ConanLockNode `json:"graph"`
}

// ConanLockNode represents a node in conan.lock
type ConanLockNode struct {
	Ref           string            `json:"ref"`
	Options       map[string]string `json:"options"`
	Settings      map[string]string `json:"settings"`
	Requires      []string          `json:"requires"`
	BuildRequires []string          `json:"build_requires"`
	Path          string            `json:"path"`
	PackageId     string            `json:"package_id"`
}

// NewConanFlexPack creates a new Conan FlexPack instance
func NewConanFlexPack(config ConanConfig) (*ConanFlexPack, error) {
	cf := &ConanFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}
	// Set default executable if not provided
	if cf.config.ConanExecutable == "" {
		cf.config.ConanExecutable = cf.getConanExecutablePath()
	}
	// Load conanfile
	if err := cf.loadConanfile(); err != nil {
		return nil, fmt.Errorf("failed to load conanfile: %w", err)
	}
	return cf, nil
}

// GetDependency fetches and parses dependencies, then returns dependency information
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
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
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
		checksumMap := cf.calculateChecksumWithFallback(dep)
		if checksumMap != nil {
			checksums = append(checksums, checksumMap)
		}
	}
	if checksums == nil {
		checksums = []map[string]interface{}{}
	}
	return checksums
}

// CalculateScopes calculates and returns the scopes for dependencies
func (cf *ConanFlexPack) CalculateScopes() []string {
	scopesMap := make(map[string]bool)
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}
	// Collect all unique scopes from dependencies
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

// CalculateRequestedBy determines which dependencies requested a particular package
// Returns a map where key is dependency ID and value is list of parent IDs that requested it
func (cf *ConanFlexPack) CalculateRequestedBy() map[string][]string {
	if cf.requestedByMap == nil {
		cf.requestedByMap = make(map[string][]string)
	}
	// If requestedBy wasn't populated during graph parsing, try to build from graph
	if len(cf.requestedByMap) == 0 && cf.graphData != nil {
		cf.buildRequestedByMapFromGraph()
	}
	return cf.requestedByMap
}

// loadConanfile loads either conanfile.py or conanfile.txt
func (cf *ConanFlexPack) loadConanfile() error {
	// Check for conanfile.py first (This file is preferred in conan, like Maven prefers pom.xml)
	conanfilePy := filepath.Join(cf.config.WorkingDirectory, "conanfile.py")
	if _, err := os.Stat(conanfilePy); err == nil {
		cf.conanfilePath = conanfilePy
		return cf.extractProjectInfoFromConanfilePy()
	}
	// Check for conanfile.txt
	conanfileTxt := filepath.Join(cf.config.WorkingDirectory, "conanfile.txt")
	if _, err := os.Stat(conanfileTxt); err == nil {
		cf.conanfilePath = conanfileTxt
		// For conanfile.txt, use directory name as project name
		cf.projectName = filepath.Base(cf.config.WorkingDirectory)
		cf.projectVersion = "1.0.0"
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
	if cf.projectVersion == "" {
		cf.projectVersion = "1.0.0"
	}
	// Extract user and channel if present
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
	pattern := attr + ` = "`
	if idx := strings.Index(content, pattern); idx != -1 {
		start := idx + len(pattern)
		end := strings.Index(content[start:], `"`)
		if end != -1 {
			return content[start : start+end]
		}
	}
	pattern = attr + ` = '`
	if idx := strings.Index(content, pattern); idx != -1 {
		start := idx + len(pattern)
		end := strings.Index(content[start:], `'`)
		if end != -1 {
			return content[start : start+end]
		}
	}
	return ""
}

// parseDependencies parses dependencies primarily using conan graph info and fallbacks to conan lock file
func (cf *ConanFlexPack) parseDependencies() {
	// parse the dependency from the dependency graph
	if err := cf.parseWithConanGraphInfo(); err == nil {
		log.Debug("Successfully parsed dependencies using 'conan graph info'")
		return
	} else {
		log.Warn("Conan graph info parsing failed, falling back to lock file: " + err.Error())
	}
	// Fallback to a conan lock file
	cf.parseFromLockFile()
}

// parseWithConanGraphInfo uses 'conan graph info --format=json' to get complete dependency information
func (cf *ConanFlexPack) parseWithConanGraphInfo() error {
	args := []string{"graph", "info", cf.conanfilePath, "--format=json"}
	// Add profile if specified
	if cf.config.Profile != "" {
		args = append(args, "-pr", cf.config.Profile)
	}
	// Add settings if specified
	for key, value := range cf.config.Settings {
		args = append(args, "-s", fmt.Sprintf("%s=%s", key, value))
	}
	// Add options if specified
	for key, value := range cf.config.Options {
		args = append(args, "-o", fmt.Sprintf("%s=%s", key, value))
	}
	cmd := exec.Command(cf.config.ConanExecutable, args...)
	cmd.Dir = cf.config.WorkingDirectory
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("conan graph info failed: %w", err)
	}
	// Parse JSON output
	var graphData ConanGraphOutput
	if err := json.Unmarshal(output, &graphData); err != nil {
		return fmt.Errorf("failed to parse graph info JSON: %w", err)
	}
	cf.graphData = &graphData
	// Process all dependencies from graph
	cf.parseDependenciesFromGraphInfo(&graphData)
	log.Debug(fmt.Sprintf("Collected %d dependencies from conan graph info", len(cf.dependencies)))
	return nil
}

// parseDependenciesFromGraphInfo extracts dependencies from graph info JSON and tracks the requestedBy relationships for transitive dependencies
func (cf *ConanFlexPack) parseDependenciesFromGraphInfo(graphData *ConanGraphOutput) {
	cf.dependencies = []DependencyInfo{}
	cf.requestedByMap = make(map[string][]string)
	seenDependencies := make(map[string]bool)
	// Find root node
	rootNode, exists := graphData.Graph.Nodes["0"]
	if !exists {
		log.Warn("No root node found in Conan graph")
		return
	}
	// Get root project identifier for requestedBy tracking
	rootId := cf.getProjectRootId()
	// Process all dependencies from root node
	// Root's direct dependencies are "requestedBy" the root project
	for childId, depEdge := range rootNode.Dependencies {
		if childNode, exists := graphData.Graph.Nodes[childId]; exists {
			cf.processDependencyNodeWithRequestedBy(childId, childNode, depEdge, rootId, seenDependencies)
		}
	}
	log.Debug(fmt.Sprintf("Built requestedBy map with %d entries", len(cf.requestedByMap)))
}

// getProjectRootId returns the project identifier for the root project
func (cf *ConanFlexPack) getProjectRootId() string {
	if cf.user != "_" && cf.channel != "_" {
		return fmt.Sprintf("%s/%s@%s/%s", cf.projectName, cf.projectVersion, cf.user, cf.channel)
	}
	return fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion)
}

// processDependencyNodeWithRequestedBy processes a dependency node and tracks requestedBy relationships
func (cf *ConanFlexPack) processDependencyNodeWithRequestedBy(nodeId string, node ConanGraphNode, edge ConanDependencyEdge, parentId string, seen map[string]bool) {
	if node.Ref == "" {
		return
	}
	// Parse the reference to get name and version
	name, version := cf.parseConanReference(node.Ref)
	if name == "" {
		if node.Name != "" {
			name = node.Name
			version = node.Version
		} else {
			return
		}
	}
	dependencyId := fmt.Sprintf("%s:%s", name, version)
	// Track requestedBy relationship (even if we've seen this dependency before)
	// This captures multiple parents requesting the same dependency
	if parentId != "" {
		cf.addRequestedBy(dependencyId, parentId)
	}
	// Skip if already processed for dependency collection
	if seen[dependencyId] {
		return
	}
	seen[dependencyId] = true
	// Determine scope from edge properties
	scopes := cf.determineScopesFromEdge(edge, node.Context)
	// Determine if this is a direct or transitive dependency
	isDirect := edge.Direct
	// Create dependency info
	depInfo := DependencyInfo{
		ID:       dependencyId,
		Name:     name,
		Version:  version,
		Type:     "conan",
		Scopes:   scopes,
		Path:     node.Path,
		IsDirect: isDirect,
	}
	cf.dependencies = append(cf.dependencies, depInfo)
	// Process children recursively - they are requestedBy this dependency
	if cf.graphData != nil {
		for childId, childEdge := range node.Dependencies {
			if childNode, exists := cf.graphData.Graph.Nodes[childId]; exists {
				cf.processDependencyNodeWithRequestedBy(childId, childNode, childEdge, dependencyId, seen)
			}
		}
	}
}

// addRequestedBy adds a requestedBy relationship, avoiding duplicates
func (cf *ConanFlexPack) addRequestedBy(dependencyId, parentId string) {
	// Check if this relationship already exists
	for _, existing := range cf.requestedByMap[dependencyId] {
		if existing == parentId {
			return
		}
	}
	cf.requestedByMap[dependencyId] = append(cf.requestedByMap[dependencyId], parentId)
}

// parseFromLockFile parses dependencies from conan.lock (fallback)
// Includes ALL dependency types: requires, build_requires, python_requires
func (cf *ConanFlexPack) parseFromLockFile() {
	lockPath := filepath.Join(cf.config.WorkingDirectory, "conan.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		log.Debug("No conan.lock file found: " + err.Error())
		return
	}
	var lockFile ConanLockFile
	if err := json.Unmarshal(data, &lockFile); err != nil {
		log.Warn("Failed to parse conan.lock: " + err.Error())
		return
	}
	// Parse runtime dependencies from lock file
	for _, req := range lockFile.Requires {
		name, version := cf.parseConanReference(req)
		if name != "" {
			cf.dependencies = append(cf.dependencies, DependencyInfo{
				Type:    "conan",
				ID:      fmt.Sprintf("%s:%s", name, version),
				Name:    name,
				Version: version,
				Scopes:  []string{"runtime"},
			})
		}
	}
	// Parse build requires (always include all dependencies)
	for _, req := range lockFile.BuildRequires {
		name, version := cf.parseConanReference(req)
		if name != "" {
			cf.dependencies = append(cf.dependencies, DependencyInfo{
				Type:    "conan",
				ID:      fmt.Sprintf("%s:%s", name, version),
				Name:    name,
				Version: version,
				Scopes:  []string{"build"},
			})
		}
	}
	// Parse python requires (always include all dependencies)
	for _, req := range lockFile.PythonRequires {
		name, version := cf.parseConanReference(req)
		if name != "" {
			cf.dependencies = append(cf.dependencies, DependencyInfo{
				Type:    "conan",
				ID:      fmt.Sprintf("%s:%s", name, version),
				Name:    name,
				Version: version,
				Scopes:  []string{"python"},
			})
		}
	}
	log.Debug(fmt.Sprintf("Parsed %d dependencies from conan.lock", len(cf.dependencies)))
}

// parseConanReference parses a Conan reference string to extract name and version
// Format: name/version[@user/channel][#revision][:package_id]
func (cf *ConanFlexPack) parseConanReference(ref string) (name, version string) {
	// Remove package_id if present
	if idx := strings.Index(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}
	// Remove revision if present
	if idx := strings.Index(ref, "#"); idx != -1 {
		ref = ref[:idx]
	}
	// Remove user/channel if present
	if idx := strings.Index(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	// Split name/version
	parts := strings.Split(ref, "/")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return ref, ""
}

// determineScopesFromEdge determines scopes based on dependency edge properties
func (cf *ConanFlexPack) determineScopesFromEdge(edge ConanDependencyEdge, context string) []string {
	// Check edge properties first
	if edge.Build {
		return []string{"build"}
	}
	if edge.Test {
		return []string{"test"}
	}
	return cf.mapConanContextToScopes(context)
}

// mapConanContextToScopes maps Conan context to build-info scopes
func (cf *ConanFlexPack) mapConanContextToScopes(context string) []string {
	switch strings.ToLower(context) {
	case "host":
		return []string{"runtime"}
	case "build":
		return []string{"build"}
	case "test":
		return []string{"test"}
	default:
		return []string{"runtime"}
	}
}

// calculateChecksumWithFallback calculates checksums
func (cf *ConanFlexPack) calculateChecksumWithFallback(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  cf.validateAndNormalizeScopes(dep.Scopes),
	}
	// Try to find artifact in Conan cache
	if artifactPath := cf.findConanArtifact(dep); artifactPath != "" {
		if sha1, sha256, md5, err := cf.calculateFileChecksum(artifactPath); err == nil {
			checksumMap["sha1"] = sha1
			checksumMap["sha256"] = sha256
			checksumMap["md5"] = md5
			checksumMap["path"] = artifactPath
			return checksumMap
		}
		log.Warn(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
	}
	// Handle missing checksums gracefully
	isBuildDependency := false
	for _, scope := range dep.Scopes {
		if strings.ToLower(scope) == "build" {
			isBuildDependency = true
			break
		}
	}
	if isBuildDependency {
		log.Debug(fmt.Sprintf("Skipping checksum calculation for build dependency: %s:%s", dep.Name, dep.Version))
	} else {
		log.Warn(fmt.Sprintf("Failed to calculate checksums for dependency: %s:%s", dep.Name, dep.Version))
	}
	return nil
}

// findConanArtifact locates a Conan artifact in the cache
func (cf *ConanFlexPack) findConanArtifact(dep DependencyInfo) string {
	// Try to find package using 'conan cache path' command
	ref := fmt.Sprintf("%s/%s", dep.Name, dep.Version)
	cmd := exec.Command(cf.config.ConanExecutable, "cache", "path", ref)
	cmd.Dir = cf.config.WorkingDirectory
	output, err := cmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get cache path for %s: %v", dep.ID, err))
		return ""
	}
	packagePath := strings.TrimSpace(string(output))
	if _, err := os.Stat(packagePath); err == nil {
		// Look for package files in the cache path
		return cf.findPackageFile(packagePath)
	}
	return ""
}

// findPackageFile finds a check summable file in the package directory
func (cf *ConanFlexPack) findPackageFile(packagePath string) string {
	patterns := []string{"*.tgz", "*.tar.gz", "*.zip"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(packagePath, pattern))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	manifestPath := filepath.Join(packagePath, "conanmanifest.txt")
	if _, err := os.Stat(manifestPath); err == nil {
		return manifestPath
	}
	conanfilePath := filepath.Join(packagePath, "conanfile.py")
	if _, err := os.Stat(conanfilePath); err == nil {
		return conanfilePath
	}
	return ""
}

// calculateFileChecksum calculates checksums for a file
func (cf *ConanFlexPack) calculateFileChecksum(filePath string) (string, string, string, error) {
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

// validateAndNormalizeScopes ensures scopes are valid and normalized
func (cf *ConanFlexPack) validateAndNormalizeScopes(scopes []string) []string {
	validScopes := map[string]bool{
		"runtime": true,
		"build":   true,
		"test":    true,
		"python":  true,
	}
	var normalized []string
	for _, scope := range scopes {
		if validScopes[scope] {
			normalized = append(normalized, scope)
		}
	}
	if len(normalized) == 0 {
		normalized = []string{"runtime"} // Default Conan scope
	}
	return normalized
}

// buildRequestedByMapFromGraph builds the requested-by relationship map from graph data
func (cf *ConanFlexPack) buildRequestedByMapFromGraph() {
	if cf.graphData == nil {
		return
	}
	if cf.requestedByMap == nil {
		cf.requestedByMap = make(map[string][]string)
	}
	// Build from dependency graph
	for parent, children := range cf.dependencyGraph {
		for _, child := range children {
			cf.addRequestedBy(child, parent)
		}
	}
}

// CollectBuildInfo collects complete build information for Conan project
func (cf *ConanFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}
	requestedByMap := cf.CalculateRequestedBy()
	var dependencies []entities.Dependency
	for _, dep := range cf.dependencies {
		// Try to calculate checksum for this specific dependency
		checksumMap := cf.calculateChecksumWithFallback(dep)
		// Convert checksum map to entities.Checksum struct
		checksum := entities.Checksum{}
		if checksumMap != nil {
			if sha1, ok := checksumMap["sha1"].(string); ok {
				checksum.Sha1 = sha1
			}
			if sha256, ok := checksumMap["sha256"].(string); ok {
				checksum.Sha256 = sha256
			}
			if md5, ok := checksumMap["md5"].(string); ok {
				checksum.Md5 = md5
			}
		}
		entity := entities.Dependency{
			Id:       dep.ID,
			Type:     dep.Type,
			Scopes:   dep.Scopes,
			Checksum: checksum,
		}
		// Add requested-by relationships
		if requesters, exists := requestedByMap[dep.ID]; exists && len(requesters) > 0 {
			entity.RequestedBy = [][]string{requesters}
		}
		dependencies = append(dependencies, entity)
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
	// Add Conan module
	moduleId := fmt.Sprintf("%s:%s", cf.projectName, cf.projectVersion)
	if cf.user != "_" && cf.channel != "_" {
		moduleId = fmt.Sprintf("%s/%s@%s/%s", cf.projectName, cf.projectVersion, cf.user, cf.channel)
	}
	module := entities.Module{
		Id:           moduleId,
		Type:         entities.Conan,
		Dependencies: dependencies,
	}
	buildInfo.Modules = append(buildInfo.Modules, module)
	// Collect VCS (Git) information if available
	vcsInfo := cf.collectVcsInfo()
	if vcsInfo != nil {
		buildInfo.VcsList = append(buildInfo.VcsList, *vcsInfo)
	}
	return buildInfo, nil
}

// collectVcsInfo collects Git VCS information from the project directory
func (cf *ConanFlexPack) collectVcsInfo() *entities.Vcs {
	workDir := cf.config.WorkingDirectory
	// Check if this is a git repository
	gitDir := filepath.Join(workDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Try parent directories up to 5 levels
		currentDir := workDir
		for i := 0; i < 5; i++ {
			parentDir := filepath.Dir(currentDir)
			if parentDir == currentDir {
				break
			}
			gitDir = filepath.Join(parentDir, ".git")
			if _, err := os.Stat(gitDir); err == nil {
				workDir = parentDir
				break
			}
			currentDir = parentDir
		}
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			log.Debug("Not a git repository, skipping VCS info collection")
			return nil
		}
	}
	vcs := &entities.Vcs{}
	// Get remote URL
	urlCmd := exec.Command("git", "config", "--get", "remote.origin.url")
	urlCmd.Dir = workDir
	if urlOutput, err := urlCmd.Output(); err == nil {
		vcs.Url = strings.TrimSpace(string(urlOutput))
	}
	// Get current revision (commit hash)
	revCmd := exec.Command("git", "rev-parse", "HEAD")
	revCmd.Dir = workDir
	if revOutput, err := revCmd.Output(); err == nil {
		vcs.Revision = strings.TrimSpace(string(revOutput))
	}
	// Get current branch
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = workDir
	if branchOutput, err := branchCmd.Output(); err == nil {
		vcs.Branch = strings.TrimSpace(string(branchOutput))
	}
	// Get last commit message
	msgCmd := exec.Command("git", "log", "-1", "--pretty=%B")
	msgCmd.Dir = workDir
	if msgOutput, err := msgCmd.Output(); err == nil {
		vcs.Message = strings.TrimSpace(string(msgOutput))
	}
	// Only return VCS info if we have at least the revision
	if vcs.Revision != "" {
		log.Debug(fmt.Sprintf("Collected VCS info: url=%s, branch=%s, revision=%s", vcs.Url, vcs.Branch, vcs.Revision))
		return vcs
	}
	return nil
}

// CollectArtifacts collects Conan artifacts from the local cache
func (cf *ConanFlexPack) CollectArtifacts() []entities.Artifact {
	var artifacts []entities.Artifact
	// Get the package reference for the current project
	packageRef := fmt.Sprintf("%s/%s", cf.projectName, cf.projectVersion)
	// Try to find the package in the local cache using 'conan cache path'
	// First get the recipe path
	recipeCmd := exec.Command(cf.config.ConanExecutable, "cache", "path", packageRef)
	recipeCmd.Dir = cf.config.WorkingDirectory
	recipeOutput, err := recipeCmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Could not find recipe in cache for %s: %v", packageRef, err))
		return artifacts
	}
	recipePath := strings.TrimSpace(string(recipeOutput))
	if recipePath != "" {
		// Collect recipe artifacts
		recipeArtifacts := cf.collectRecipeArtifacts(recipePath, packageRef)
		artifacts = append(artifacts, recipeArtifacts...)
	}
	// Try to find the package binary path
	// List packages for this recipe
	listCmd := exec.Command(cf.config.ConanExecutable, "list", packageRef+":*", "--format=json")
	listCmd.Dir = cf.config.WorkingDirectory
	listOutput, err := listCmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Could not list packages for %s: %v", packageRef, err))
		return artifacts
	}
	// Parse the package IDs from the list output
	packageIds := cf.extractPackageIdsFromList(listOutput)
	for _, pkgId := range packageIds {
		// Get the package binary path
		pkgRef := fmt.Sprintf("%s:%s", packageRef, pkgId)
		pkgCmd := exec.Command(cf.config.ConanExecutable, "cache", "path", pkgRef)
		pkgCmd.Dir = cf.config.WorkingDirectory
		pkgOutput, err := pkgCmd.Output()
		if err != nil {
			log.Debug(fmt.Sprintf("Could not find package in cache for %s: %v", pkgRef, err))
			continue
		}
		pkgPath := strings.TrimSpace(string(pkgOutput))
		if pkgPath != "" {
			// Collect package artifacts
			pkgArtifacts := cf.collectPackageArtifacts(pkgPath, packageRef, pkgId)
			artifacts = append(artifacts, pkgArtifacts...)
		}
	}
	log.Info(fmt.Sprintf("Collected %d Conan artifacts from local cache", len(artifacts)))
	return artifacts
}

// collectRecipeArtifacts collects artifacts from the recipe export folder
func (cf *ConanFlexPack) collectRecipeArtifacts(recipePath, packageRef string) []entities.Artifact {
	var artifacts []entities.Artifact
	// Recipe files to look for
	recipeFiles := []string{"conanfile.py", "conandata.yml", "conanmanifest.txt"}
	for _, filename := range recipeFiles {
		filePath := filepath.Join(recipePath, filename)
		if _, err := os.Stat(filePath); err == nil {
			artifact := cf.createArtifactFromFile(filePath, filename, packageRef, "recipe")
			if artifact != nil {
				artifacts = append(artifacts, *artifact)
			}
		}
	}
	// Check for conan_sources.tgz in the download folder
	downloadPath := filepath.Join(filepath.Dir(recipePath), "d")
	sourceTgz := filepath.Join(downloadPath, "conan_sources.tgz")
	if _, err := os.Stat(sourceTgz); err == nil {
		artifact := cf.createArtifactFromFile(sourceTgz, "conan_sources.tgz", packageRef, "sources")
		if artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}
	return artifacts
}

// collectPackageArtifacts collects artifacts from the package binary folder
func (cf *ConanFlexPack) collectPackageArtifacts(pkgPath, packageRef, packageId string) []entities.Artifact {
	var artifacts []entities.Artifact
	// Package files to look for
	packageFiles := []string{"conaninfo.txt", "conanmanifest.txt"}
	for _, filename := range packageFiles {
		filePath := filepath.Join(pkgPath, filename)
		if _, err := os.Stat(filePath); err == nil {
			artifact := cf.createArtifactFromFile(filePath, filename, packageRef+":"+packageId, "package")
			if artifact != nil {
				artifacts = append(artifacts, *artifact)
			}
		}
	}
	// Check for conan_package.tgz in the build folder (parent of package folder)
	buildPath := filepath.Dir(pkgPath)
	packageTgz := filepath.Join(buildPath, "conan_package.tgz")
	if _, err := os.Stat(packageTgz); err == nil {
		artifact := cf.createArtifactFromFile(packageTgz, "conan_package.tgz", packageRef+":"+packageId, "package")
		if artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}
	return artifacts
}

// createArtifactFromFile creates an artifact entry with checksums
func (cf *ConanFlexPack) createArtifactFromFile(filePath, filename, packageRef, artifactType string) *entities.Artifact {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get file details for %s: %v", filePath, err))
		return nil
	}
	return &entities.Artifact{
		Name: filename,
		Path: packageRef,
		Type: fmt.Sprintf("conan-%s", artifactType),
		Checksum: entities.Checksum{
			Sha1:   fileDetails.Checksum.Sha1,
			Sha256: fileDetails.Checksum.Sha256,
			Md5:    fileDetails.Checksum.Md5,
		},
	}
}

// extractPackageIdsFromList extracts package IDs from 'conan list' JSON output
func (cf *ConanFlexPack) extractPackageIdsFromList(listOutput []byte) []string {
	var packageIds []string
	var listData map[string]interface{}
	if err := json.Unmarshal(listOutput, &listData); err != nil {
		log.Debug("Failed to parse conan list output: " + err.Error())
		return packageIds
	}
	// Navigate the nested structure to find package IDs
	// Structure: {"Local Cache": {"<name>/<version>": {"revisions": {"<rrev>": {"packages": {"<pkg_id>": ...}}}}}}
	for _, cache := range listData {
		cacheMap, ok := cache.(map[string]interface{})
		if !ok {
			continue
		}
		for _, pkg := range cacheMap {
			pkgMap, ok := pkg.(map[string]interface{})
			if !ok {
				continue
			}
			revisions, ok := pkgMap["revisions"].(map[string]interface{})
			if !ok {
				continue
			}
			for _, rev := range revisions {
				revMap, ok := rev.(map[string]interface{})
				if !ok {
					continue
				}
				packages, ok := revMap["packages"].(map[string]interface{})
				if !ok {
					continue
				}
				for pkgId := range packages {
					packageIds = append(packageIds, pkgId)
				}
			}
		}
	}
	return packageIds
}

// GetProjectDependencies returns all project dependencies with full details
func (cf *ConanFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	if len(cf.dependencies) == 0 {
		cf.parseDependencies()
	}
	// Calculate RequestedBy relationships
	requestedBy := cf.CalculateRequestedBy()
	// Add RequestedBy information to dependencies
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

// getConanExecutablePath gets the Conan executable path
func (cf *ConanFlexPack) getConanExecutablePath() string {
	// Check for conan in PATH
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
	// Parse "Conan version 2.0.13" format
	lines := strings.Split(version, "\n")
	if len(lines) > 0 {
		firstLine := lines[0]
		if parts := strings.Fields(firstLine); len(parts) >= 3 {
			return parts[2]
		}
	}
	return "unknown"
}

// RunConanInstallWithBuildInfo runs conan install and collects build information
// Collects ALL dependency types: requires, build_requires, tool_requires, python_requires
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
	// Collect build info if build name and number provided
	if buildName != "" && buildNumber != "" {
		buildInfo, err := conanFlex.CollectBuildInfo(buildName, buildNumber)
		if err != nil {
			return fmt.Errorf("failed to collect build info: %w", err)
		}
		err = SaveConanBuildInfoForJfrogCli(buildInfo)
		if err != nil {
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

	err = buildInstance.SaveBuildInfo(buildInfo)
	if err != nil {
		return fmt.Errorf("failed to save build info: %w", err)
	}
	log.Debug("Successfully saved Conan build info for jfrog-cli")
	return nil
}

const conanCacheLatestVersion = 1

// ConanDependenciesCache represents cached dependency information for Conan projects
type ConanDependenciesCache struct {
	Version     int                            `json:"version,omitempty"`
	DepsMap     map[string]entities.Dependency `json:"dependencies,omitempty"`
	LastUpdated time.Time                      `json:"lastUpdated,omitempty"`
	ProjectPath string                         `json:"projectPath,omitempty"`
}

// GetConanDependenciesCache reads the JSON cache file of recent used project's dependencies
func GetConanDependenciesCache(projectPath string) (cache *ConanDependenciesCache, err error) {
	cache = new(ConanDependenciesCache)
	cacheFilePath, exists := getConanDependenciesCacheFilePath(projectPath)
	if !exists {
		log.Debug("Conan dependencies cache not found: " + cacheFilePath)
		return nil, nil
	}
	data, err := os.ReadFile(cacheFilePath)
	if err != nil {
		log.Debug("Failed to read Conan cache file: " + err.Error())
		return nil, err
	}
	err = json.Unmarshal(data, cache)
	if err != nil {
		log.Debug("Failed to parse Conan cache file: " + err.Error())
		return nil, err
	}
	log.Debug(fmt.Sprintf("Loaded Conan dependencies cache with %d entries", len(cache.DepsMap)))
	return cache, nil
}

// UpdateConanDependenciesCache writes the updated project's dependencies cache
func UpdateConanDependenciesCache(dependenciesMap map[string]entities.Dependency, projectPath string) error {
	updatedCache := ConanDependenciesCache{
		Version:     conanCacheLatestVersion,
		DepsMap:     dependenciesMap,
		LastUpdated: time.Now(),
		ProjectPath: projectPath,
	}
	content, err := json.MarshalIndent(&updatedCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Conan cache: %w", err)
	}
	cacheFilePath, _ := getConanDependenciesCacheFilePath(projectPath)
	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}
	err = os.WriteFile(cacheFilePath, content, 0644)
	if err != nil {
		return fmt.Errorf("failed to write Conan cache file: %w", err)
	}
	log.Debug(fmt.Sprintf("Updated Conan dependencies cache with %d entries at %s", len(dependenciesMap), cacheFilePath))
	return nil
}

// GetDependency returns required dependency from cache
func (cache *ConanDependenciesCache) GetDependency(dependencyName string) (dependency entities.Dependency, found bool) {
	if cache == nil || cache.DepsMap == nil {
		return entities.Dependency{}, false
	}
	dependency, found = cache.DepsMap[dependencyName]
	if found {
		log.Debug("Found cached dependency: " + dependencyName)
	}
	return dependency, found
}

// IsValid checks if the cache is valid and not expired
func (cache *ConanDependenciesCache) IsValid(maxAge time.Duration) bool {
	if cache == nil {
		return false
	}
	// Check version compatibility
	if cache.Version != conanCacheLatestVersion {
		log.Debug(fmt.Sprintf("Conan cache version mismatch: expected %d, got %d", conanCacheLatestVersion, cache.Version))
		return false
	}
	// Check if cache is too old
	if maxAge > 0 && time.Since(cache.LastUpdated) > maxAge {
		log.Debug(fmt.Sprintf("Conan cache expired: last updated %v ago", time.Since(cache.LastUpdated)))
		return false
	}
	return true
}

// getConanDependenciesCacheFilePath returns the path to Conan dependencies cache file
func getConanDependenciesCacheFilePath(projectPath string) (cacheFilePath string, exists bool) {
	projectsDirPath := filepath.Join(projectPath, ".jfrog", "projects")
	cacheFilePath = filepath.Join(projectsDirPath, "conan-deps.cache.json")
	_, err := os.Stat(cacheFilePath)
	exists = !os.IsNotExist(err)
	return cacheFilePath, exists
}

// UpdateDependenciesWithCache enhances dependencies with cached information
func (cf *ConanFlexPack) UpdateDependenciesWithCache() error {
	// Load existing cache
	cache, err := GetConanDependenciesCache(cf.config.WorkingDirectory)
	if err != nil {
		log.Debug("No existing Conan cache found, will create new one")
		cache = nil
	}
	// Check if cache is valid
	maxCacheAge := 24 * time.Hour
	if cache != nil && !cache.IsValid(maxCacheAge) {
		log.Debug("Conan cache is invalid or expired, ignoring")
		cache = nil
	}
	dependenciesMap := make(map[string]entities.Dependency)
	var missingDeps []string
	// Process each dependency
	for i, dep := range cf.dependencies {
		depKey := fmt.Sprintf("%s:%s", dep.Name, dep.Version)
		// Try to get from cache first
		var cachedDep entities.Dependency
		var found bool
		if cache != nil {
			cachedDep, found = cache.GetDependency(depKey)
		}
		if found && !cachedDep.IsEmpty() {
			// Use cached dependency info
			entityDep := entities.Dependency{
				Id:       depKey,
				Type:     "conan",
				Scopes:   dep.Scopes,
				Checksum: cachedDep.Checksum,
			}
			dependenciesMap[depKey] = entityDep
			// Update our internal dependency with cached checksums
			cf.dependencies[i].SHA1 = cachedDep.Sha1
			cf.dependencies[i].SHA256 = cachedDep.Sha256
			cf.dependencies[i].MD5 = cachedDep.Md5
			log.Debug("Using cached checksums for " + depKey)
		} else {
			// Need to calculate checksums for this dependency
			checksumMap := cf.calculateChecksumWithFallback(dep)
			if checksumMap != nil {
				if sha1, ok := checksumMap["sha1"].(string); ok && sha1 != "" {
					sha256, _ := checksumMap["sha256"].(string)
					md5, _ := checksumMap["md5"].(string)
					entityDep := entities.Dependency{
						Id:     depKey,
						Type:   "conan",
						Scopes: dep.Scopes,
						Checksum: entities.Checksum{
							Sha1:   sha1,
							Sha256: sha256,
							Md5:    md5,
						},
					}
					dependenciesMap[depKey] = entityDep
					// Update our internal dependency
					cf.dependencies[i].SHA1 = sha1
					cf.dependencies[i].SHA256 = sha256
					cf.dependencies[i].MD5 = md5
					log.Debug("Calculated new checksums for " + depKey)
				} else {
					missingDeps = append(missingDeps, depKey)
				}
			} else {
				missingDeps = append(missingDeps, depKey)
			}
		}
	}
	// Report missing dependencies
	if len(missingDeps) > 0 {
		log.Warn("The following Conan packages could not be found or checksums calculated:")
		for _, dep := range missingDeps {
			log.Warn("  - " + dep)
		}
		log.Warn("This may happen if packages are not in Conan cache. Run 'conan install' to populate the cache.")
	}
	// Update cache with new information
	if len(dependenciesMap) > 0 {
		err = UpdateConanDependenciesCache(dependenciesMap, cf.config.WorkingDirectory)
		if err != nil {
			log.Warn("Failed to update Conan dependencies cache: " + err.Error())
		}
	}
	return nil
}
