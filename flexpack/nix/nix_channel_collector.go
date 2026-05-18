package nix

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

// NixChannelCollector implements FlexPackManager and BuildInfoCollector for
// Nix channel-based workflows. It collects dependencies from the Nix store
// after a package has been installed/built via channels.
type NixChannelCollector struct {
	config          NixChannelConfig
	dependencies    []flexpack.DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
}

// NewNixChannelCollector creates a new channel-based collector.
func NewNixChannelCollector(config NixChannelConfig) (*NixChannelCollector, error) {
	if config.WorkingDirectory == "" {
		config.WorkingDirectory = "."
	}

	absDir, err := filepath.Abs(config.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	config.WorkingDirectory = absDir

	return &NixChannelCollector{
		config:          config,
		dependencies:    []flexpack.DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		projectName:     filepath.Base(absDir),
	}, nil
}

// CollectStorePathDependencies runs "nix path-info --json -r" on the given store paths
// and collects the runtime closure as dependencies.
func (c *NixChannelCollector) CollectStorePathDependencies(storePaths ...string) error {
	if len(storePaths) == 0 {
		return nil
	}

	args := append([]string{"path-info", "--json", "--recursive"}, storePaths...)
	cmd := exec.Command("nix", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nix path-info failed: %w", err)
	}

	// nix path-info --json outputs a map: { "/nix/store/...": { ... }, ... }
	var pathInfoMap map[string]NixStorePathInfo
	if err := json.Unmarshal(output, &pathInfoMap); err != nil {
		return fmt.Errorf("parse path-info output: %w", err)
	}

	// Fill Path field from map key
	for path, info := range pathInfoMap {
		info.Path = path
		pathInfoMap[path] = info
	}

	// Build dependency graph from references
	c.dependencyGraph = make(map[string][]string)
	for parentPath, info := range pathInfoMap {
		parentID := StorePathToDepID(parentPath)
		for _, refPath := range info.References {
			if refPath == parentPath {
				continue // skip self-references
			}
			childID := StorePathToDepID(refPath)
			c.dependencyGraph[parentID] = append(c.dependencyGraph[parentID], childID)
		}
	}

	// Convert to DependencyInfo, skip the root store paths (they're the project output, not deps)
	rootIDs := make(map[string]bool)
	for _, sp := range storePaths {
		rootIDs[StorePathToDepID(sp)] = true
	}

	c.dependencies = []flexpack.DependencyInfo{}
	for path, info := range pathInfoMap {
		depID := StorePathToDepID(path)
		if rootIDs[depID] {
			continue
		}

		pkgName := ExtractPackageName(path)
		name, version := ExtractNameAndVersion(pkgName)

		// Convert narHash from SRI to hex for Artifactory matching
		sha256 := info.NarHash
		if hexHash, err := SriToHex(info.NarHash); err == nil {
			sha256 = hexHash
		}

		dep := flexpack.DependencyInfo{
			ID:      depID,
			Name:    name,
			Version: version,
			SHA256:  sha256,
			Scopes:  []string{"runtime"},
			Path:    path,
		}

		c.dependencies = append(c.dependencies, dep)
	}

	log.Debug(fmt.Sprintf("Collected %d runtime dependencies from store closure", len(c.dependencies)))
	return nil
}

// CollectBuildInfo creates a complete BuildInfo from the collected dependencies.
func (c *NixChannelCollector) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
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
		Id:   c.projectName,
		Type: entities.Nix,
	}

	// Build requestedBy (inverse of dependency graph)
	requestedBy := make(map[string][]string)
	for parent, children := range c.dependencyGraph {
		for _, child := range children {
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}

	for _, dep := range c.dependencies {
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

// GetProjectDependencies returns all collected dependencies.
func (c *NixChannelCollector) GetProjectDependencies() ([]flexpack.DependencyInfo, error) {
	return c.dependencies, nil
}

// GetDependencyGraph returns the forward dependency graph.
func (c *NixChannelCollector) GetDependencyGraph() (map[string][]string, error) {
	return c.dependencyGraph, nil
}

// GetDependency returns a formatted string summary of dependencies.
func (c *NixChannelCollector) GetDependency() string {
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s\n", c.projectName)
	result.WriteString("Dependencies:\n")
	for _, dep := range c.dependencies {
		fmt.Fprintf(&result, "  - %s [%s]\n", dep.ID, dep.SHA256)
	}
	return result.String()
}

// ParseDependencyToList returns a list of dependency IDs.
func (c *NixChannelCollector) ParseDependencyToList() []string {
	var depList []string
	for _, dep := range c.dependencies {
		depList = append(depList, dep.ID)
	}
	return depList
}

// CalculateChecksum returns checksum maps for each dependency.
func (c *NixChannelCollector) CalculateChecksum() []map[string]interface{} {
	var checksums []map[string]interface{}
	for _, dep := range c.dependencies {
		checksums = append(checksums, map[string]interface{}{
			"sha256": dep.SHA256,
		})
	}
	return checksums
}

// CalculateScopes returns the unique set of scopes across all dependencies.
func (c *NixChannelCollector) CalculateScopes() []string {
	return []string{"runtime"}
}

// CalculateRequestedBy returns the inverted dependency graph.
func (c *NixChannelCollector) CalculateRequestedBy() map[string][]string {
	requestedBy := make(map[string][]string)
	for parent, children := range c.dependencyGraph {
		for _, child := range children {
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	return requestedBy
}
