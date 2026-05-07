package nix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

// NixFlexPack implements the FlexPackManager and BuildInfoCollector interfaces for Nix flakes.
type NixFlexPack struct {
	config          NixConfig
	dependencies    []flexpack.DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	flakeLockData   *NixFlakeLock
	flakeNixExists  bool
}

// NewNixFlexPack creates a new Nix FlexPack instance.
func NewNixFlexPack(config NixConfig) (*NixFlexPack, error) {
	if config.WorkingDirectory == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		config.WorkingDirectory = wd
	}

	absDir, err := filepath.Abs(config.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve working directory: %w", err)
	}
	config.WorkingDirectory = absDir

	nf := &NixFlexPack{
		config:          config,
		dependencies:    []flexpack.DependencyInfo{},
		dependencyGraph: make(map[string][]string),
	}

	if err := nf.loadFlakeNix(); err != nil {
		return nil, err
	}

	nf.projectName = filepath.Base(config.WorkingDirectory)

	if err := nf.loadFlakeLock(); err != nil {
		log.Warn("Could not load flake.lock: " + err.Error())
	}

	return nf, nil
}

// loadFlakeNix verifies that flake.nix exists in the working directory.
func (nf *NixFlexPack) loadFlakeNix() error {
	flakeNixPath := filepath.Join(nf.config.WorkingDirectory, "flake.nix")
	if _, err := os.Stat(flakeNixPath); err != nil {
		return fmt.Errorf("flake.nix not found in %s: %w", nf.config.WorkingDirectory, err)
	}
	nf.flakeNixExists = true
	return nil
}

// loadFlakeLock reads and parses flake.lock from the working directory.
func (nf *NixFlexPack) loadFlakeLock() error {
	lockPath := filepath.Join(nf.config.WorkingDirectory, "flake.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read flake.lock: %w", err)
	}

	var lockData NixFlakeLock
	if err := json.Unmarshal(data, &lockData); err != nil {
		return fmt.Errorf("failed to parse flake.lock: %w", err)
	}

	if lockData.Version != 7 {
		return fmt.Errorf("unsupported flake.lock version %d, expected 7", lockData.Version)
	}

	nf.flakeLockData = &lockData

	// Extract project version from root node's locked rev if available
	if rootNodeName := lockData.Root; rootNodeName != "" {
		if rootNode, exists := lockData.Nodes[rootNodeName]; exists {
			if rootNode.Locked != nil && rootNode.Locked.Rev != "" {
				nf.projectVersion = rootNode.Locked.Rev
			}
		}
	}

	if err := nf.parseDependencies(); err != nil {
		return fmt.Errorf("failed to parse dependencies: %w", err)
	}

	nf.buildDependencyGraph()

	return nil
}

// parseDependencies walks the flake.lock nodes and builds the dependency list.
// Skips the root node and alias/follows nodes (those with Locked == nil).
func (nf *NixFlexPack) parseDependencies() error {
	if nf.flakeLockData == nil {
		return nil
	}

	nf.dependencies = []flexpack.DependencyInfo{}

	for name, node := range nf.flakeLockData.Nodes {
		if name == nf.flakeLockData.Root {
			continue
		}
		if isAliasNode(node) {
			continue
		}

		depID := buildDepId(name, node.Locked)
		sourceURL := buildSourceURL(node.Locked)

		dep := flexpack.DependencyInfo{
			ID:      depID,
			Name:    name,
			Version: node.Locked.Rev,
			SHA256:  node.Locked.NarHash,
			Scopes:  []string{"build"},
			Path:    sourceURL,
		}

		nf.dependencies = append(nf.dependencies, dep)
	}

	return nil
}

// buildDependencyGraph builds the forward dependency graph from node inputs.
func (nf *NixFlexPack) buildDependencyGraph() {
	if nf.flakeLockData == nil {
		return
	}

	nf.dependencyGraph = make(map[string][]string)

	for name, node := range nf.flakeLockData.Nodes {
		if node.Inputs == nil {
			continue
		}

		var parentID string
		if name == nf.flakeLockData.Root {
			parentID = nf.projectName
		} else if isAliasNode(node) {
			continue
		} else {
			parentID = buildDepId(name, node.Locked)
		}

		for _, inputVal := range node.Inputs {
			targetName := resolveNodeRef(inputVal)
			if targetName == "" {
				continue
			}
			targetNode, exists := nf.flakeLockData.Nodes[targetName]
			if !exists || targetName == nf.flakeLockData.Root || isAliasNode(targetNode) {
				continue
			}
			childID := buildDepId(targetName, targetNode.Locked)
			nf.dependencyGraph[parentID] = append(nf.dependencyGraph[parentID], childID)
		}
	}
}

// buildRequestedBy inverts the dependency graph: child → list of parents.
func (nf *NixFlexPack) buildRequestedBy() map[string][]string {
	requestedBy := make(map[string][]string)
	for parent, children := range nf.dependencyGraph {
		for _, child := range children {
			if requestedBy[child] == nil {
				requestedBy[child] = []string{}
			}
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	return requestedBy
}

// GetDependency returns a formatted string of all dependencies.
func (nf *NixFlexPack) GetDependency() string {
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s\n", nf.projectName)
	result.WriteString("Dependencies:\n")
	for _, dep := range nf.dependencies {
		fmt.Fprintf(&result, "  - %s [%s]\n", dep.ID, dep.SHA256)
	}
	return result.String()
}

// ParseDependencyToList returns a list of dependency IDs.
func (nf *NixFlexPack) ParseDependencyToList() []string {
	var depList []string
	for _, dep := range nf.dependencies {
		depList = append(depList, dep.ID)
	}
	return depList
}

// CalculateChecksum returns checksum maps for each dependency.
// narHash in SRI format is stored directly — no hex conversion.
func (nf *NixFlexPack) CalculateChecksum() []map[string]interface{} {
	var checksums []map[string]interface{}
	for _, dep := range nf.dependencies {
		checksumMap := map[string]interface{}{
			"sha256": dep.SHA256,
		}
		checksums = append(checksums, checksumMap)
	}
	return checksums
}

// CalculateScopes returns the unique set of scopes across all dependencies.
func (nf *NixFlexPack) CalculateScopes() []string {
	scopesMap := make(map[string]bool)
	for _, dep := range nf.dependencies {
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

// CalculateRequestedBy returns the inverted dependency graph.
func (nf *NixFlexPack) CalculateRequestedBy() map[string][]string {
	return nf.buildRequestedBy()
}

// CollectBuildInfo collects complete build information for the Nix flake project.
func (nf *NixFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	buildInfo := &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "nix",
			Version: "1.0",
		},
		BuildAgent: &entities.Agent{
			Name:    "Generic",
			Version: "1.0",
		},
		Modules: []entities.Module{},
	}

	module := entities.Module{
		Id:   nf.projectName,
		Type: entities.Nix,
	}

	deps, err := nf.GetProjectDependencies()
	if err != nil {
		return nil, err
	}

	requestedBy := nf.buildRequestedBy()

	for _, dep := range deps {
		entityDep := entities.Dependency{
			Id:     dep.ID,
			Scopes: dep.Scopes,
			Checksum: entities.Checksum{
				Sha256: dep.SHA256,
			},
		}

		if parents, exists := requestedBy[dep.ID]; exists && len(parents) > 0 {
			entityDep.RequestedBy = [][]string{parents}
		}

		module.Dependencies = append(module.Dependencies, entityDep)
	}

	buildInfo.Modules = append(buildInfo.Modules, module)

	log.Debug(fmt.Sprintf("Collected %d dependencies for Nix module %s", len(module.Dependencies), module.Id))

	return buildInfo, nil
}

// GetProjectDependencies returns all project dependencies.
func (nf *NixFlexPack) GetProjectDependencies() ([]flexpack.DependencyInfo, error) {
	return nf.dependencies, nil
}

// GetDependencyGraph returns the forward dependency graph.
func (nf *NixFlexPack) GetDependencyGraph() (map[string][]string, error) {
	return nf.dependencyGraph, nil
}

// isAliasNode returns true if the node is a follows/alias (has no locked reference)
// and is not the root node.
func isAliasNode(node NixFlakeLockNode) bool {
	return node.Locked == nil
}

// buildDepId formats a dependency ID as "name:rev" or "name:narHash" if no rev.
func buildDepId(name string, locked *NixLockedRef) string {
	if locked == nil {
		return name
	}
	if locked.Rev != "" {
		return fmt.Sprintf("%s:%s", name, locked.Rev)
	}
	if locked.NarHash != "" {
		return fmt.Sprintf("%s:%s", name, locked.NarHash)
	}
	return name
}

// buildSourceURL reconstructs the source URL from the locked reference type.
func buildSourceURL(locked *NixLockedRef) string {
	if locked == nil {
		return ""
	}
	switch locked.Type {
	case "github":
		return fmt.Sprintf("https://github.com/%s/%s", locked.Owner, locked.Repo)
	case "gitlab":
		host := locked.Host
		if host == "" {
			host = "gitlab.com"
		}
		return fmt.Sprintf("https://%s/%s/%s", host, locked.Owner, locked.Repo)
	case "sourcehut":
		return fmt.Sprintf("https://git.sr.ht/%s/%s", locked.Owner, locked.Repo)
	case "git":
		return locked.URL
	case "tarball":
		return locked.URL
	case "path":
		return locked.Path
	default:
		return ""
	}
}

// resolveNodeRef resolves a polymorphic input value to a node name.
// String values are direct references. Array values are follows paths —
// only the first element (the target flake name) is used to resolve.
func resolveNodeRef(inputVal interface{}) string {
	switch v := inputVal.(type) {
	case string:
		return v
	case []interface{}:
		// Follows path: the last element is the input name in the target flake.
		// We resolve to the first element which is the target flake node name,
		// then look up that node's input. However, for graph purposes we
		// only need to know the ultimate resolved node. Since follows aliases
		// point to an existing node, we resolve the chain:
		// e.g. ["nix", "nixpkgs"] means: follow nix's nixpkgs input.
		// For simplicity in the graph, we skip follows edges since the
		// target node is already included as a direct dependency elsewhere.
		return ""
	default:
		return ""
	}
}
