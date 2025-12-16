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
	// Try primary method: conan graph info
	if err := cf.parseWithConanGraphInfo(); err == nil {
		log.Debug("Successfully parsed dependencies using 'conan graph info'")
		return nil
	} else {
		log.Debug("Conan graph info parsing failed: " + err.Error())
	}

	// Fallback: parse from lock file
	if err := cf.parseFromLockFile(); err != nil {
		return fmt.Errorf("failed to parse dependencies from both graph info and lock file: %w", err)
	}

	log.Debug("Successfully parsed dependencies from conan.lock")
	return nil
}

// parseWithConanGraphInfo uses 'conan graph info --format=json' to get dependency information.
// This is the preferred method as it provides complete dependency graph with metadata.
func (cf *ConanFlexPack) parseWithConanGraphInfo() error {
	args := cf.buildGraphInfoArgs()
	log.Debug(fmt.Sprintf("Running: conan %s", strings.Join(args, " ")))

	cmd := exec.Command(cf.config.ConanExecutable, args...)
	cmd.Dir = cf.config.WorkingDirectory

	// Capture stdout separately from stderr
	// Conan outputs JSON to stdout and status messages to stderr
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Get stdout content (the JSON output)
	output := stdout.String()

	// Try to parse JSON even if command failed - Conan sometimes exits non-zero
	// but still produces valid JSON output (e.g., when deps are missing from cache)
	var graphData ConanGraphOutput
	if jsonErr := json.Unmarshal([]byte(output), &graphData); jsonErr != nil {
		// If JSON parsing failed and command also failed, return command error
		if err != nil {
			return fmt.Errorf("conan graph info failed: %w (stderr: %s)", err, stderr.String())
		}
		return fmt.Errorf("failed to parse graph info JSON: %w (output: %s)", jsonErr, output)
	}

	cf.graphData = &graphData
	cf.parseDependenciesFromGraphInfo(&graphData)

	log.Debug(fmt.Sprintf("Collected %d dependencies from conan graph info", len(cf.dependencies)))
	return nil
}

// buildGraphInfoArgs builds command arguments for conan graph info.
// Returns: ["graph", "info", "<conanfile>", "--format=json", ...]
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

// parseDependenciesFromGraphInfo extracts dependencies from graph info JSON.
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
func (cf *ConanFlexPack) parseDependenciesFromGraphInfo(graphData *ConanGraphOutput) {
	cf.dependencies = []entities.Dependency{}
	cf.requestedByMap = make(map[string][]string)
	seenDependencies := make(map[string]bool)

	// Node "0" is the root project node
	rootNode, exists := graphData.Graph.Nodes["0"]
	if !exists {
		log.Warn("No root node found in Conan graph")
		return
	}

	rootId := cf.getProjectRootId()

	// Process all direct dependencies of the root node
	// Example: rootNode.Dependencies = {"1": {ref: "zlib/1.2.13", direct: true}, ...}
	for childId, depEdge := range rootNode.Dependencies {
		if childNode, exists := graphData.Graph.Nodes[childId]; exists {
			cf.processDependencyNode(childId, childNode, depEdge, rootId, seenDependencies)
		}
	}

	log.Debug(fmt.Sprintf("Built requestedBy map with %d entries", len(cf.requestedByMap)))
}

// processDependencyNode processes a single dependency node and its transitive dependencies.
// It builds the dependency entity with checksums and tracks requestedBy relationships.
func (cf *ConanFlexPack) processDependencyNode(nodeId string, node ConanGraphNode, edge ConanDependencyEdge, parentId string, seen map[string]bool) {
	if node.Ref == "" {
		return
	}

	name, version := cf.parseConanReference(node.Ref)
	if name == "" {
		name, version = node.Name, node.Version
		if name == "" {
			return
		}
	}

	dependencyId := fmt.Sprintf("%s:%s", name, version)

	// Track requestedBy relationship (parent -> child)
	if parentId != "" {
		cf.addRequestedBy(dependencyId, parentId)
	}

	// Skip if already processed (avoid duplicates in dependency list)
	if seen[dependencyId] {
		return
	}
	seen[dependencyId] = true

	// Build dependency entity with checksum
	dep := cf.createDependencyEntity(name, version, node, edge)
	cf.dependencies = append(cf.dependencies, dep)

	// Recursively process transitive dependencies
	cf.processChildDependencies(node, dependencyId, seen)
}

// createDependencyEntity creates an entities.Dependency from node information.
// Calculates checksums from the local Conan cache.
func (cf *ConanFlexPack) createDependencyEntity(name, version string, node ConanGraphNode, edge ConanDependencyEdge) entities.Dependency {
	dependencyId := fmt.Sprintf("%s:%s", name, version)
	scopes := cf.determineScopesFromEdge(edge, node.Context)

	dep := entities.Dependency{
		Id:     dependencyId,
		Type:   "conan",
		Scopes: scopes,
	}

	// Calculate checksum from local cache
	checksum, err := cf.calculateDependencyChecksum(name, version)
	if err != nil {
		log.Debug(fmt.Sprintf("Could not calculate checksum for %s: %v", dependencyId, err))
	} else {
		dep.Checksum = checksum
	}

	// Add requestedBy relationships
	if requesters, exists := cf.requestedByMap[dependencyId]; exists && len(requesters) > 0 {
		dep.RequestedBy = [][]string{requesters}
	}

	return dep
}

// calculateDependencyChecksum calculates checksums for a dependency from Conan cache.
// Returns the checksum entity or error if the dependency is not in cache.
func (cf *ConanFlexPack) calculateDependencyChecksum(name, version string) (entities.Checksum, error) {
	ref := fmt.Sprintf("%s/%s", name, version)

	// Get cache path using conan cache path command
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

	// Find a checksummable file in the package directory
	filePath, err := cf.findChecksummableFile(packagePath)
	if err != nil {
		return entities.Checksum{}, err
	}

	// Calculate checksums using gofrog crypto
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

// findChecksummableFile finds a file suitable for checksum calculation in the package directory.
// Priority: archive files (.tgz, .tar.gz, .zip) > conanmanifest.txt > conanfile.py
func (cf *ConanFlexPack) findChecksummableFile(packagePath string) (string, error) {
	// Try archive files first (most reliable for checksums)
	archivePatterns := []string{"*.tgz", "*.tar.gz", "*.zip"}
	for _, pattern := range archivePatterns {
		matches, err := filepath.Glob(filepath.Join(packagePath, pattern))
		if err == nil && len(matches) > 0 {
			return matches[0], nil
		}
	}

	// Try manifest file
	manifestPath := filepath.Join(packagePath, "conanmanifest.txt")
	if _, err := os.Stat(manifestPath); err == nil {
		return manifestPath, nil
	}

	// Try conanfile
	conanfilePath := filepath.Join(packagePath, "conanfile.py")
	if _, err := os.Stat(conanfilePath); err == nil {
		return conanfilePath, nil
	}

	return "", fmt.Errorf("no checksummable file found in %s", packagePath)
}

// processChildDependencies recursively processes transitive dependencies of a node
func (cf *ConanFlexPack) processChildDependencies(node ConanGraphNode, parentId string, seen map[string]bool) {
	if cf.graphData == nil {
		return
	}

	for childId, childEdge := range node.Dependencies {
		if childNode, exists := cf.graphData.Graph.Nodes[childId]; exists {
			cf.processDependencyNode(childId, childNode, childEdge, parentId, seen)
		}
	}
}

// addRequestedBy adds a requestedBy relationship, avoiding duplicates
func (cf *ConanFlexPack) addRequestedBy(dependencyId, parentId string) {
	for _, existing := range cf.requestedByMap[dependencyId] {
		if existing == parentId {
			return
		}
	}
	cf.requestedByMap[dependencyId] = append(cf.requestedByMap[dependencyId], parentId)
}

// parseFromLockFile parses dependencies from conan.lock file (fallback method).
// Lock file contains frozen dependency versions but less metadata than graph info.
func (cf *ConanFlexPack) parseFromLockFile() error {
	lockPath := filepath.Join(cf.config.WorkingDirectory, "conan.lock")

	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read conan.lock: %w", err)
	}

	var lockFile ConanLockFile
	if err := json.Unmarshal(data, &lockFile); err != nil {
		return fmt.Errorf("failed to parse conan.lock: %w", err)
	}

	cf.parseLockFileRequires(lockFile.Requires, "runtime")
	cf.parseLockFileRequires(lockFile.BuildRequires, "build")
	cf.parseLockFileRequires(lockFile.PythonRequires, "python")

	log.Debug(fmt.Sprintf("Parsed %d dependencies from conan.lock", len(cf.dependencies)))
	return nil
}

// parseLockFileRequires parses a list of requirements from lock file
func (cf *ConanFlexPack) parseLockFileRequires(requires []string, scope string) {
	for _, req := range requires {
		name, version := cf.parseConanReference(req)
		if name != "" {
			dep := entities.Dependency{
				Type:   "conan",
				Id:     fmt.Sprintf("%s:%s", name, version),
				Scopes: []string{scope},
			}

			// Try to calculate checksum
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
	// Remove package_id suffix (:pkg_id)
	if idx := strings.Index(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}

	// Remove revision suffix (#revision)
	if idx := strings.Index(ref, "#"); idx != -1 {
		ref = ref[:idx]
	}

	// Remove user/channel suffix (@user/channel)
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
