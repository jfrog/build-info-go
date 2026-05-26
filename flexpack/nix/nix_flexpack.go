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

// defaultBuildResultSymlink is the conventional Nix build output symlink
// produced by both `nix-build` (channels) and `nix build` (flakes).
const defaultBuildResultSymlink = "./result"

// NixFlexPack implements FlexPackManager and BuildInfoCollector for Nix.
// It collects dependencies from the local Nix store after a build.
// The same implementation works for channel-based and flakes-based workflows
// because both share the /nix/store and the `nix path-info` interface.
type NixFlexPack struct {
	config          NixConfig
	dependencies    []flexpack.DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
}

// NewNixFlexPack creates a new Nix FlexPack collector.
func NewNixFlexPack(config NixConfig) (*NixFlexPack, error) {
	if config.WorkingDirectory == "" {
		config.WorkingDirectory = "."
	}
	absDir, err := filepath.Abs(config.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	config.WorkingDirectory = absDir
	if config.NixExecutable == "" {
		config.NixExecutable = "nix"
	}

	return &NixFlexPack{
		config:          config,
		dependencies:    []flexpack.DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		projectName:     filepath.Base(absDir),
	}, nil
}

// CollectStorePathDependencies runs "nix path-info --json --recursive" on the given store
// paths and collects the runtime closure as dependencies.
func (c *NixFlexPack) CollectStorePathDependencies(storePaths ...string) error {
	if len(storePaths) == 0 {
		return nil
	}

	args := append([]string{"path-info", "--json", "--recursive"}, storePaths...)
	cmd := exec.Command(c.config.NixExecutable, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nix path-info failed: %w", err)
	}

	// nix path-info --json outputs a map: { "/nix/store/...": { ... }, ... }
	var pathInfoMap map[string]NixStorePathInfo
	if err := json.Unmarshal(output, &pathInfoMap); err != nil {
		return fmt.Errorf("parse path-info output: %w", err)
	}

	for path, info := range pathInfoMap {
		info.Path = path
		pathInfoMap[path] = info
	}

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

	// Skip the root store paths (they're the project output, not deps)
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

		// NOTE: narHash is the hash of the NAR archive of the unpacked store path,
		// not the sha256 of the uploaded file. Artifactory computes sha256 over
		// the uploaded object, so this value will not match by-checksum lookups
		// against the repository. If SRI conversion fails, leave the field empty
		// rather than storing an opaque "sha256:<nix32>" string.
		sha256 := ""
		if info.NarHash != "" {
			if hexHash, err := SriToHex(info.NarHash); err == nil {
				sha256 = hexHash
			} else {
				log.Warn(fmt.Sprintf("Could not convert narHash %q for %s to hex; leaving SHA256 empty: %s",
					info.NarHash, path, err))
			}
		}

		c.dependencies = append(c.dependencies, flexpack.DependencyInfo{
			ID:      depID,
			Name:    name,
			Version: version,
			SHA256:  sha256,
			Scopes:  []string{"runtime"},
			Path:    path,
		})
	}

	log.Debug(fmt.Sprintf("Collected %d runtime dependencies from store closure", len(c.dependencies)))
	return nil
}

// CollectBuildInfo creates a complete BuildInfo from the collected dependencies.
// If no dependencies have been collected yet, it auto-discovers them by following
// the conventional ./result symlink produced by `nix build` / `nix-build`.
func (c *NixFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if len(c.dependencies) == 0 && len(c.dependencyGraph) == 0 {
		if err := c.autoDiscoverDependencies(); err != nil {
			// Auto-discovery is best-effort; surface as a debug message but
			// continue so callers that intend an empty build-info still work.
			log.Debug(fmt.Sprintf("Auto-discover skipped: %s", err))
		}
	}

	nixVersion := c.getNixVersion()
	buildInfo := &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "build-info-go",
			Version: "1.0.0",
		},
		BuildAgent: &entities.Agent{
			Name:    "Nix",
			Version: nixVersion,
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

		// Each parent is its own request-chain entry: [[p1], [p2]] not [[p1, p2]].
		if parents, exists := requestedBy[dep.ID]; exists {
			chains := make([][]string, 0, len(parents))
			for _, p := range parents {
				chains = append(chains, []string{p})
			}
			entityDep.RequestedBy = chains
		}

		module.Dependencies = append(module.Dependencies, entityDep)
	}

	buildInfo.Modules = append(buildInfo.Modules, module)
	log.Debug(fmt.Sprintf("Collected %d dependencies for Nix module %s", len(module.Dependencies), module.Id))

	return buildInfo, nil
}

// autoDiscoverDependencies tries to follow the conventional ./result symlink
// produced by `nix build` / `nix-build` and collect its runtime closure.
func (c *NixFlexPack) autoDiscoverDependencies() error {
	resultPath := filepath.Join(c.config.WorkingDirectory, defaultBuildResultSymlink)
	target, err := filepath.EvalSymlinks(resultPath)
	if err != nil {
		return fmt.Errorf("no %s symlink found in %s: %w",
			defaultBuildResultSymlink, c.config.WorkingDirectory, err)
	}
	log.Debug(fmt.Sprintf("Auto-discovered build root %s -> %s", resultPath, target))
	return c.CollectStorePathDependencies(target)
}

// getNixVersion parses `nix --version` output, e.g. "nix (Nix) 2.18.1".
// Returns "unknown" if the executable is missing or output is unexpected.
func (c *NixFlexPack) getNixVersion() string {
	out, err := exec.Command(c.config.NixExecutable, "--version").Output()
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[len(fields)-1]
}

// GetProjectDependencies returns all collected dependencies.
func (c *NixFlexPack) GetProjectDependencies() ([]flexpack.DependencyInfo, error) {
	return c.dependencies, nil
}

// GetDependencyGraph returns the forward dependency graph.
func (c *NixFlexPack) GetDependencyGraph() (map[string][]string, error) {
	return c.dependencyGraph, nil
}

// GetDependency returns a formatted string summary of dependencies.
func (c *NixFlexPack) GetDependency() string {
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s\n", c.projectName)
	result.WriteString("Dependencies:\n")
	for _, dep := range c.dependencies {
		fmt.Fprintf(&result, "  - %s [%s]\n", dep.ID, dep.SHA256)
	}
	return result.String()
}

// ParseDependencyToList returns a list of dependency IDs.
func (c *NixFlexPack) ParseDependencyToList() []string {
	var depList []string
	for _, dep := range c.dependencies {
		depList = append(depList, dep.ID)
	}
	return depList
}

// CalculateChecksum returns checksum maps for each dependency.
func (c *NixFlexPack) CalculateChecksum() []map[string]interface{} {
	var checksums []map[string]interface{}
	for _, dep := range c.dependencies {
		checksums = append(checksums, map[string]interface{}{
			"sha256": dep.SHA256,
		})
	}
	return checksums
}

// CalculateScopes returns the unique set of scopes across all dependencies.
func (c *NixFlexPack) CalculateScopes() []string {
	return []string{"runtime"}
}

// CalculateRequestedBy returns the inverted dependency graph.
func (c *NixFlexPack) CalculateRequestedBy() map[string][]string {
	requestedBy := make(map[string][]string)
	for parent, children := range c.dependencyGraph {
		for _, child := range children {
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	return requestedBy
}
