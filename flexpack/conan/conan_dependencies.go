package conan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// Conan graph info command arguments
const (
	conanGraphCmd    = "graph"
	conanInfoSubCmd  = "info"
	conanFormatFlag  = "--format=json"
	conanProfileFlag = "-pr"
	conanSettingFlag = "-s"
	conanOptionFlag  = "-o"
)

// parseDependencies parses dependencies using conan graph info with fallback to lock file.
// Returns error if both methods fail.
func (cf *ConanFlexPack) parseDependencies() error {
	if err := cf.parseWithConanGraphInfo(); err == nil {
		log.Debug("Successfully parsed dependencies using 'conan graph info'")
		return nil
	} else {
		log.Debug("Conan graph info parsing failed: " + err.Error())
	}
	if err := cf.parseDependenciesFromLockFile(); err != nil {
		return fmt.Errorf("failed to parse dependencies from both graph info and lock file: %w", err)
	}
	log.Debug("Successfully parsed dependencies from conan.lock")
	return nil
}

// parseWithConanGraphInfo uses 'conan graph info --format=json' to get dependency information.
// This is the preferred method as it provides complete dependency graph with metadata.
func (cf *ConanFlexPack) parseWithConanGraphInfo() error {
	args := cf.buildGraphInfoArgs()
	cmd := exec.Command(cf.config.ConanExecutable, args...)
	cmd.Dir = cf.config.WorkingDirectory
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := stdout.String()
	var graphData ConanGraphOutput
	if jsonErr := json.Unmarshal([]byte(output), &graphData); jsonErr != nil {
		if err != nil {
			return fmt.Errorf("conan graph info failed: %w (stderr: %s)", err, stderr.String())
		}
		return fmt.Errorf("failed to parse graph info JSON: %w (output: %s)", jsonErr, output)
	}
	cf.graphData = &graphData
	cf.extractDependenciesFromGraph()
	log.Debug(fmt.Sprintf("Collected %d dependencies from conan graph info", len(cf.dependencies)))
	return nil
}

// buildGraphInfoArgs builds command arguments for conan graph info.
// Returns: ["graph", "info", "<conanfile>", "--format", "json", ...]
func (cf *ConanFlexPack) buildGraphInfoArgs() []string {
	args := []string{conanGraphCmd, conanInfoSubCmd, cf.conanfilePath, conanFormatFlag}
	if cf.config.Profile != "" {
		args = append(args, conanProfileFlag, cf.config.Profile)
	}
	for key, value := range cf.config.Settings {
		args = append(args, conanSettingFlag, fmt.Sprintf("%s=%s", key, value))
	}
	for key, value := range cf.config.Options {
		args = append(args, conanOptionFlag, fmt.Sprintf("%s=%s", key, value))
	}
	return args
}

// extractDependenciesFromGraph extracts dependencies from the graph data stored in cf.graphData.
// The graph structure is:
//
//	{
//	  "graph": {
//	    "nodes": {
//	      "0": { ... root node ... },
//	      "1": { "ref": "zlib/1.2.13", "dependencies": {...} },
//	      ...
//	    }
//	  }
//	}
//
// Node "0" is always the root project. Other nodes are dependencies.
func (cf *ConanFlexPack) extractDependenciesFromGraph() {
	cf.dependencies = []entities.Dependency{}
	cf.requestedByMap = make(map[string][]string)
	processedDeps := make(map[string]bool)
	rootNode, exists := cf.graphData.Graph.Nodes["0"]
	if !exists {
		log.Warn("No root node found in Conan graph")
		return
	}
	rootID := cf.getProjectRootId()
	// Process all direct dependencies of the root node
	// Example: rootNode.Dependencies = {"1": {ref: "zlib/1.2.13", direct: true}, ...}
	for childNodeID, depEdge := range rootNode.Dependencies {
		if childNode, exists := cf.graphData.Graph.Nodes[childNodeID]; exists {
			cf.processDependencyNode(childNode, depEdge, rootID, processedDeps)
		}
	}
}

// processDependencyNode processes a single dependency node and its transitive dependencies.
// It builds the dependency entity with checksums and tracks requestedBy relationships.
// Parameters:
//   - node: The graph node representing this dependency
//   - edge: The edge properties (build, test, direct flags)
//   - parentID: The ID of the parent dependency (for requestedBy tracking)
//   - processedDeps: Map to track already processed dependencies (avoids duplicates)
func (cf *ConanFlexPack) processDependencyNode(node ConanGraphNode, edge ConanDependencyEdge, parentID string, processedDeps map[string]bool) {
	var name, version string
	if node.Ref != "" {
		name, version = cf.parseConanReference(node.Ref)
	}
	// Fallback to Name/Version fields if Ref is empty or couldn't be parsed
	if name == "" {
		name, version = node.Name, node.Version
	}
	if name == "" {
		return
	}
	dependencyID := fmt.Sprintf("%s:%s", name, version)
	if parentID != "" {
		cf.addRequestedBy(dependencyID, parentID)
	}
	if processedDeps[dependencyID] {
		return
	}
	processedDeps[dependencyID] = true
	dep := cf.createDependencyEntity(name, version, node, edge)
	cf.dependencies = append(cf.dependencies, dep)
	cf.processChildDependencies(node, dependencyID, processedDeps)
}

// createDependencyEntity creates an entities.Dependency from node information.
// Calculates checksums from the local Conan cache.
func (cf *ConanFlexPack) createDependencyEntity(name, version string, node ConanGraphNode, edge ConanDependencyEdge) entities.Dependency {
	dependencyID := fmt.Sprintf("%s:%s", name, version)
	scopes := cf.determineScopesFromEdge(edge, node.Context)
	dep := entities.Dependency{
		Id:     dependencyID,
		Scopes: scopes,
	}
	checksum, err := cf.calculateDependencyChecksum(name, version)
	if err != nil {
		log.Debug(fmt.Sprintf("Could not calculate checksum for %s: %v", dependencyID, err))
	} else {
		dep.Checksum = checksum
	}
	if requesters, exists := cf.requestedByMap[dependencyID]; exists && len(requesters) > 0 {
		dep.RequestedBy = [][]string{requesters}
	}
	return dep
}

// calculateDependencyChecksum calculates checksums for a dependency from Conan cache.
// Returns the checksum entity or error if the dependency is not in cache.
func (cf *ConanFlexPack) calculateDependencyChecksum(name, version string) (entities.Checksum, error) {
	ref := fmt.Sprintf("%s/%s", name, version)
	cmd := exec.Command(cf.config.ConanExecutable, "cache", "path", ref)
	cmd.Dir = cf.config.WorkingDirectory
	output, err := cmd.Output()
	if err != nil {
		return entities.Checksum{}, fmt.Errorf("dependency not in cache: %w", err)
	}
	packagePath := strings.TrimSpace(string(output))
	if _, err := os.Stat(packagePath); err != nil {
		return entities.Checksum{}, fmt.Errorf("cache path does not exist: %w", err)
	}
	filePath, err := cf.findConanPackageFile(packagePath)
	if err != nil {
		return entities.Checksum{}, err
	}
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return entities.Checksum{}, fmt.Errorf("failed to calculate checksum: %w", err)
	}
	return entities.Checksum{
		Sha1:   fileDetails.Checksum.Sha1,
		Sha256: fileDetails.Checksum.Sha256,
		Md5:    fileDetails.Checksum.Md5,
	}, nil
}

// findConanPackageFile finds a file suitable for checksum calculation in the Conan package directory.
// Priority: archive files (.tgz, .tar.gz, .zip) > conanmanifest.txt > conanfile.py
// Returns error if no suitable file is found in the package directory.
func (cf *ConanFlexPack) findConanPackageFile(packagePath string) (string, error) {
	archivePatterns := []string{"*.tgz", "*.tar.gz", "*.zip"}
	for _, pattern := range archivePatterns {
		matches, err := filepath.Glob(filepath.Join(packagePath, pattern))
		if err == nil && len(matches) > 0 {
			return matches[0], nil
		}
	}
	manifestPath := filepath.Join(packagePath, "conanmanifest.txt")
	if _, err := os.Stat(manifestPath); err == nil {
		return manifestPath, nil
	}
	conanfilePath := filepath.Join(packagePath, "conanfile.py")
	if _, err := os.Stat(conanfilePath); err == nil {
		return conanfilePath, nil
	}
	return "", fmt.Errorf("no checksummable file found in package path: %s", packagePath)
}

// processChildDependencies recursively processes transitive dependencies of a node.
func (cf *ConanFlexPack) processChildDependencies(node ConanGraphNode, parentID string, processedDeps map[string]bool) {
	if cf.graphData == nil {
		return
	}
	for childNodeID, childEdge := range node.Dependencies {
		if childNode, exists := cf.graphData.Graph.Nodes[childNodeID]; exists {
			cf.processDependencyNode(childNode, childEdge, parentID, processedDeps)
		}
	}
}

// addRequestedBy adds a requestedBy relationship, avoiding duplicates.
func (cf *ConanFlexPack) addRequestedBy(dependencyID, parentID string) {
	for _, existing := range cf.requestedByMap[dependencyID] {
		if existing == parentID {
			return
		}
	}
	cf.requestedByMap[dependencyID] = append(cf.requestedByMap[dependencyID], parentID)
}

// parseDependenciesFromLockFile parses dependencies from conan.lock file (fallback method).
// Lock file contains frozen dependency versions but less metadata than graph info.
func (cf *ConanFlexPack) parseDependenciesFromLockFile() error {
	lockPath := filepath.Join(cf.config.WorkingDirectory, "conan.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read conan.lock: %w", err)
	}
	var lockFile ConanLockFile
	if err := json.Unmarshal(data, &lockFile); err != nil {
		return fmt.Errorf("failed to parse conan.lock: %w", err)
	}
	cf.extractDependenciesFromLockRequires(lockFile.Requires, "runtime")
	cf.extractDependenciesFromLockRequires(lockFile.BuildRequires, "build")
	cf.extractDependenciesFromLockRequires(lockFile.PythonRequires, "python")
	log.Debug("Parsed dependencies from conan.lock:", len(cf.dependencies))
	return nil
}

// extractDependenciesFromLockRequires parses a list of requirements from lock file.
func (cf *ConanFlexPack) extractDependenciesFromLockRequires(requires []string, scope string) {
	for _, req := range requires {
		name, version := cf.parseConanReference(req)
		if name != "" {
			dep := entities.Dependency{
				Id:     fmt.Sprintf("%s:%s", name, version),
				Scopes: []string{scope},
			}
			checksum, err := cf.calculateDependencyChecksum(name, version)
			if err == nil {
				dep.Checksum = checksum
			}
			cf.dependencies = append(cf.dependencies, dep)
		}
	}
}

// parseConanReference parses a Conan reference string to extract name and version.
// Conan reference format: name/version[@user/channel][#revision][:package_id]
// Examples:
//   - "zlib/1.2.13" -> name="zlib", version="1.2.13"
//   - "zlib/1.2.13@_/_#abc123" -> name="zlib", version="1.2.13"
//   - "zlib/1.2.13:pkg123" -> name="zlib", version="1.2.13"
func (cf *ConanFlexPack) parseConanReference(ref string) (name, version string) {
	if idx := strings.Index(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}
	if idx := strings.Index(ref, "#"); idx != -1 {
		ref = ref[:idx]
	}
	if idx := strings.Index(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	parts := strings.Split(ref, "/")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return ref, ""
}

// determineScopesFromEdge determines scopes based on dependency edge properties.
// Edge properties indicate the dependency type in the Conan graph.
func (cf *ConanFlexPack) determineScopesFromEdge(edge ConanDependencyEdge, context string) []string {
	if edge.Build {
		return []string{"build"}
	}
	if edge.Test {
		return []string{"test"}
	}
	return cf.mapConanContextToScopes(context)
}

// mapConanContextToScopes maps Conan context to build-info scopes.
// Returns a slice because build-info spec supports multiple scopes per dependency,
// though Conan typically assigns a single scope based on context.
// Conan contexts:
//   - "host": Runtime dependencies (the target platform)
//   - "build": Build-time dependencies (tools for building)
//   - "test": Test dependencies
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
