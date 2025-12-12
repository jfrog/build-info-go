package conan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/gofrog/log"
)

// parseDependencies parses dependencies using conan graph info with fallback to lock file
func (cf *ConanFlexPack) parseDependencies() {
	if err := cf.parseWithConanGraphInfo(); err == nil {
		log.Debug("Successfully parsed dependencies using 'conan graph info'")
		return
	} else {
		log.Warn("Conan graph info parsing failed, falling back to lock file: " + err.Error())
	}

	cf.parseFromLockFile()
}

// parseWithConanGraphInfo uses 'conan graph info --format=json' to get dependency information
func (cf *ConanFlexPack) parseWithConanGraphInfo() error {
	args := cf.buildGraphInfoArgs()

	cmd := exec.Command(cf.config.ConanExecutable, args...)
	cmd.Dir = cf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("conan graph info failed: %w", err)
	}

	var graphData ConanGraphOutput
	if err := json.Unmarshal(output, &graphData); err != nil {
		return fmt.Errorf("failed to parse graph info JSON: %w", err)
	}

	cf.graphData = &graphData
	cf.parseDependenciesFromGraphInfo(&graphData)

	log.Debug(fmt.Sprintf("Collected %d dependencies from conan graph info", len(cf.dependencies)))
	return nil
}

// buildGraphInfoArgs builds command arguments for conan graph info
func (cf *ConanFlexPack) buildGraphInfoArgs() []string {
	args := []string{"graph", "info", cf.conanfilePath, "--format=json"}

	if cf.config.Profile != "" {
		args = append(args, "-pr", cf.config.Profile)
	}

	for key, value := range cf.config.Settings {
		args = append(args, "-s", fmt.Sprintf("%s=%s", key, value))
	}

	for key, value := range cf.config.Options {
		args = append(args, "-o", fmt.Sprintf("%s=%s", key, value))
	}

	return args
}

// parseDependenciesFromGraphInfo extracts dependencies from graph info JSON
func (cf *ConanFlexPack) parseDependenciesFromGraphInfo(graphData *ConanGraphOutput) {
	cf.dependencies = []DependencyInfo{}
	cf.requestedByMap = make(map[string][]string)
	seenDependencies := make(map[string]bool)

	rootNode, exists := graphData.Graph.Nodes["0"]
	if !exists {
		log.Warn("No root node found in Conan graph")
		return
	}

	rootId := cf.getProjectRootId()

	for childId, depEdge := range rootNode.Dependencies {
		if childNode, exists := graphData.Graph.Nodes[childId]; exists {
			cf.processDependencyNodeWithRequestedBy(childId, childNode, depEdge, rootId, seenDependencies)
		}
	}

	log.Debug(fmt.Sprintf("Built requestedBy map with %d entries", len(cf.requestedByMap)))
}

// processDependencyNodeWithRequestedBy processes a dependency node and tracks relationships
func (cf *ConanFlexPack) processDependencyNodeWithRequestedBy(nodeId string, node ConanGraphNode, edge ConanDependencyEdge, parentId string, seen map[string]bool) {
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

	dependencyId := formatDependencyKey(name, version)

	// Track requestedBy relationship
	if parentId != "" {
		cf.addRequestedBy(dependencyId, parentId)
	}

	if seen[dependencyId] {
		return
	}
	seen[dependencyId] = true

	depInfo := DependencyInfo{
		ID:       dependencyId,
		Name:     name,
		Version:  version,
		Type:     "conan",
		Scopes:   cf.determineScopesFromEdge(edge, node.Context),
		Path:     node.Path,
		IsDirect: edge.Direct,
	}
	cf.dependencies = append(cf.dependencies, depInfo)

	// Process children recursively
	cf.processChildDependencies(node, dependencyId, seen)
}

// processChildDependencies processes child dependencies of a node
func (cf *ConanFlexPack) processChildDependencies(node ConanGraphNode, parentId string, seen map[string]bool) {
	if cf.graphData == nil {
		return
	}

	for childId, childEdge := range node.Dependencies {
		if childNode, exists := cf.graphData.Graph.Nodes[childId]; exists {
			cf.processDependencyNodeWithRequestedBy(childId, childNode, childEdge, parentId, seen)
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

// parseFromLockFile parses dependencies from conan.lock (fallback)
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

	cf.parseLockFileRequires(lockFile.Requires, "runtime")
	cf.parseLockFileRequires(lockFile.BuildRequires, "build")
	cf.parseLockFileRequires(lockFile.PythonRequires, "python")

	log.Debug(fmt.Sprintf("Parsed %d dependencies from conan.lock", len(cf.dependencies)))
}

// parseLockFileRequires parses a list of requirements from lock file
func (cf *ConanFlexPack) parseLockFileRequires(requires []string, scope string) {
	for _, req := range requires {
		name, version := cf.parseConanReference(req)
		if name != "" {
			cf.dependencies = append(cf.dependencies, DependencyInfo{
				Type:    "conan",
				ID:      formatDependencyKey(name, version),
				Name:    name,
				Version: version,
				Scopes:  []string{scope},
			})
		}
	}
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

// buildRequestedByMapFromGraph builds the requested-by relationship map from graph data
func (cf *ConanFlexPack) buildRequestedByMapFromGraph() {
	if cf.graphData == nil {
		return
	}

	if cf.requestedByMap == nil {
		cf.requestedByMap = make(map[string][]string)
	}

	for parent, children := range cf.dependencyGraph {
		for _, child := range children {
			cf.addRequestedBy(child, parent)
		}
	}
}

