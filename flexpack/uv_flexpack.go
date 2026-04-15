package flexpack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// UvLockFile represents the top-level structure of uv.lock
type UvLockFile struct {
	Version  int          `toml:"version"`
	Revision int          `toml:"revision"`
	Packages []UvPackage  `toml:"package"`
}

// UvPackage represents a [[package]] entry in uv.lock
type UvPackage struct {
	Name            string                        `toml:"name"`
	Version         string                        `toml:"version"`
	Source          UvSource                      `toml:"source"`
	Dependencies    []UvDependencyEdge            `toml:"dependencies"`
	DevDependencies map[string][]UvDependencyEdge `toml:"dev-dependencies"`
	Sdist           *UvArtifact                   `toml:"sdist"`
	Wheels          []UvArtifact                  `toml:"wheels"`
}

// UvSource is an inline table with exactly one key identifying the source type.
type UvSource struct {
	Registry  string `toml:"registry"`
	Virtual   string `toml:"virtual"`
	Editable  string `toml:"editable"`
	Directory string `toml:"directory"`
	Git       string `toml:"git"`
	URL       string `toml:"url"`
}

// IsWorkspacePackage returns true if this source represents a local workspace package.
func (s UvSource) IsWorkspacePackage() bool {
	return s.Virtual != "" || s.Editable != "" || s.Directory != ""
}

// HasArtifacts returns true if this source type provides sdist/wheel artifacts.
func (s UvSource) HasArtifacts() bool {
	return s.Registry != "" || s.URL != ""
}

// UvArtifact represents an sdist or wheel entry in uv.lock
type UvArtifact struct {
	URL        string `toml:"url"`
	Path       string `toml:"path"`
	Hash       string `toml:"hash"`        // "sha256:<hex>"; absent for git
	Size       int64  `toml:"size"`
	UploadTime string `toml:"upload-time"` // ISO 8601; may be absent (revision < 3)
}

// UvDependencyEdge represents a dependency reference inside a [[package]] entry
type UvDependencyEdge struct {
	Name    string   `toml:"name"`
	Marker  string   `toml:"marker"`
	Extra   []string `toml:"extra"`
	Version string   `toml:"version"`
}

// UvPyProjectToml reads only [project] (PEP 621) — UV format, not Poetry
type UvPyProjectToml struct {
	Project struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
	} `toml:"project"`
}

// UvFlexPack implements FlexPackManager and BuildInfoCollector for the UV package manager
type UvFlexPack struct {
	config            UvConfig
	lockFileData      *UvLockFile
	pyprojectData     *UvPyProjectToml
	projectName       string
	projectVersion    string
	dependencies      []DependencyInfo
	depGraph          map[string][]string  // normalized-name -> []normalized-name
	requestedByChains map[string][][]string // dep ID -> full chains back to root (UV-specific)
}

// NewUvFlexPack creates a new UvFlexPack instance.
func NewUvFlexPack(config UvConfig) (*UvFlexPack, error) {
	uf := &UvFlexPack{
		config:            config,
		dependencies:      []DependencyInfo{},
		depGraph:          make(map[string][]string),
		requestedByChains: make(map[string][][]string),
	}
	if err := uf.loadPyProjectToml(); err != nil {
		return nil, fmt.Errorf("failed to load pyproject.toml: %w", err)
	}
	if err := uf.loadUvLock(); err != nil {
		log.Debug("Failed to load uv.lock, dependency collection will be empty: " + err.Error())
	}
	return uf, nil
}

// loadPyProjectToml reads and parses pyproject.toml using PEP 621 [project] section.
func (uf *UvFlexPack) loadPyProjectToml() error {
	pyprojectPath := filepath.Join(uf.config.WorkingDirectory, "pyproject.toml")
	data, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return fmt.Errorf("failed to read pyproject.toml: %w", err)
	}
	uf.pyprojectData = &UvPyProjectToml{}
	if err := toml.Unmarshal(data, uf.pyprojectData); err != nil {
		return fmt.Errorf("failed to parse pyproject.toml: %w", err)
	}
	uf.projectName = uf.pyprojectData.Project.Name
	uf.projectVersion = uf.pyprojectData.Project.Version
	if uf.projectName == "" {
		return fmt.Errorf("project name not found in pyproject.toml (checked [project.name])")
	}
	if uf.projectVersion == "" {
		return fmt.Errorf("project version not found in pyproject.toml (checked [project.version])")
	}
	return nil
}

// loadUvLock reads and parses uv.lock.
func (uf *UvFlexPack) loadUvLock() error {
	lockPath := filepath.Join(uf.config.WorkingDirectory, "uv.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read uv.lock: %w", err)
	}
	uf.lockFileData = &UvLockFile{}
	if err := toml.Unmarshal(data, uf.lockFileData); err != nil {
		return fmt.Errorf("failed to parse uv.lock: %w", err)
	}
	return nil
}

// normalizeName converts package names to lowercase with hyphens.
func normalizeName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", "-"))
}

// extractSHA256 strips the "sha256:" prefix from a hash string.
func extractSHA256(hash string) string {
	return strings.TrimPrefix(hash, "sha256:")
}

// bestHash returns the best available hash for a package.
// Prefers pure-Python wheel (none-any), falls back to first wheel, then sdist.
func bestHash(pkg UvPackage) string {
	for _, w := range pkg.Wheels {
		if strings.Contains(w.URL, "none-any") && w.Hash != "" {
			return w.Hash
		}
	}
	for _, w := range pkg.Wheels {
		if w.Hash != "" {
			return w.Hash
		}
	}
	if pkg.Sdist != nil && pkg.Sdist.Hash != "" {
		return pkg.Sdist.Hash
	}
	return ""
}

// depFileType returns the file extension type for a package artifact.
// Prefers pure-Python wheel (none-any), falls back to first wheel, then sdist.
// Matches pip/pipenv behavior: "whl", "tar.gz", "zip", etc.
func depFileType(pkg UvPackage) string {
	bestURL := ""
	for _, w := range pkg.Wheels {
		if strings.Contains(w.URL, "none-any") && w.URL != "" {
			bestURL = w.URL
			break
		}
	}
	if bestURL == "" {
		for _, w := range pkg.Wheels {
			if w.URL != "" {
				bestURL = w.URL
				break
			}
		}
	}
	if bestURL == "" && pkg.Sdist != nil && pkg.Sdist.URL != "" {
		bestURL = pkg.Sdist.URL
	}
	if bestURL == "" {
		return ""
	}
	base := filepath.Base(bestURL)
	if strings.HasSuffix(base, ".tar.gz") {
		return "tar.gz"
	}
	if i := strings.LastIndex(base, "."); i != -1 {
		return base[i+1:]
	}
	return ""
}

// parseDependencies populates uf.dependencies and uf.depGraph from the lock file.
// ID format and requestedBy chains match the pip/pipenv canonical build-info format:
//   - dep ID:  "name:version"  (e.g. "certifi:2026.2.25")
//   - dep type: file extension (e.g. "whl", "tar.gz")
//   - requestedBy: full chain back to root module (e.g. [["requests:2.33.1","myapp:0.1.0"]])
//   - direct deps: requestedBy = [["myapp:0.1.0"]]
//   - no scopes (Python has no compile/runtime distinction; matches pip/pipenv)
func (uf *UvFlexPack) parseDependencies() {
	if uf.lockFileData == nil {
		return
	}

	moduleID := fmt.Sprintf("%s:%s", uf.projectName, uf.projectVersion)

	// Build name->package map
	pkgByName := make(map[string]*UvPackage)
	for i := range uf.lockFileData.Packages {
		pkg := &uf.lockFileData.Packages[i]
		pkgByName[normalizeName(pkg.Name)] = pkg
	}

	// Find root workspace package
	var rootPkg *UvPackage
	for i := range uf.lockFileData.Packages {
		pkg := &uf.lockFileData.Packages[i]
		if pkg.Source.Virtual == "." || pkg.Source.Editable == "." {
			rootPkg = pkg
			break
		}
	}

	// Collect direct main and dev dep names
	directMainDeps := make(map[string]bool)
	directDevDeps := make(map[string]bool)
	if rootPkg != nil {
		for _, edge := range rootPkg.Dependencies {
			directMainDeps[normalizeName(edge.Name)] = true
		}
		for _, edges := range rootPkg.DevDependencies {
			for _, edge := range edges {
				directDevDeps[normalizeName(edge.Name)] = true
			}
		}
	}

	// When excluding dev deps and the project has BOTH main deps and dev deps declared,
	// compute the reachable set from main deps via BFS. This excludes transitive-only dev deps
	// (e.g. pytest's dependencies like pluggy, iniconfig) — not just the direct dev dep itself.
	// If only one side is present (no main deps or no dev deps), fall back to the simple check.
	var mainReachable map[string]bool
	if !uf.config.IncludeDevDependencies && rootPkg != nil &&
		len(directMainDeps) > 0 && len(directDevDeps) > 0 {
		mainReachable = computeMainReachable(directMainDeps, pkgByName)
	}

	// Build depInfo map: normalizedName -> *DependencyInfo
	// Only non-workspace packages. Exclusion logic when IncludeDevDependencies=false:
	//   - With reachability analysis: skip anything not reachable from main deps
	//   - Without (no dev deps or no main deps declared): skip direct dev deps only
	depInfoMap := make(map[string]*DependencyInfo)
	for i := range uf.lockFileData.Packages {
		pkg := &uf.lockFileData.Packages[i]
		if pkg.Source.IsWorkspacePackage() {
			continue
		}
		normalizedName := normalizeName(pkg.Name)
		if !uf.config.IncludeDevDependencies {
			if mainReachable != nil {
				if !mainReachable[normalizedName] {
					continue // reachability analysis: exclude dev-only transitive deps
				}
			} else if directDevDeps[normalizedName] {
				continue // simple fallback: exclude direct dev deps only
			}
		}
		depInfoMap[normalizedName] = &DependencyInfo{
			ID:      fmt.Sprintf("%s:%s", pkg.Name, pkg.Version),
			Name:    pkg.Name,
			Version: pkg.Version,
			Type:    depFileType(*pkg),
			SHA256:  extractSHA256(bestHash(*pkg)),
		}
	}

	// Build forward dep graph: normalizedName -> []normalizedName (children present in depInfoMap)
	fwdGraph := make(map[string][]string)
	for normalizedName := range depInfoMap {
		pkg := pkgByName[normalizedName]
		if pkg == nil {
			continue
		}
		var children []string
		for _, edge := range pkg.Dependencies {
			childName := normalizeName(edge.Name)
			if _, ok := depInfoMap[childName]; ok {
				children = append(children, childName)
			}
		}
		fwdGraph[normalizedName] = children
		uf.depGraph[depInfoMap[normalizedName].ID] = func() []string {
			var ids []string
			for _, c := range children {
				ids = append(ids, depInfoMap[c].ID)
			}
			return ids
		}()
	}

	// Collect direct children of root (main deps; optionally dev deps)
	var rootChildren []string
	if rootPkg != nil {
		for _, edge := range rootPkg.Dependencies {
			n := normalizeName(edge.Name)
			if _, ok := depInfoMap[n]; ok {
				rootChildren = append(rootChildren, n)
			}
		}
		if uf.config.IncludeDevDependencies {
			for _, edges := range rootPkg.DevDependencies {
				for _, edge := range edges {
					n := normalizeName(edge.Name)
					if _, ok := depInfoMap[n]; ok {
						rootChildren = append(rootChildren, n)
					}
				}
			}
		}
	} else {
		// No lockfile root found — treat all non-workspace packages as direct deps
		for n := range depInfoMap {
			rootChildren = append(rootChildren, n)
		}
	}

	// Build requestedBy chains using pip's recursive DFS approach.
	// Results go into uf.requestedByChains (map[string][][]string), NOT into DependencyInfo.RequestedBy.
	// This keeps the shared DependencyInfo type ([]string) unchanged for poetry/maven.
	buildUvRequestedBy(moduleID, []string{}, rootChildren, depInfoMap, fwdGraph, uf.requestedByChains, entities.RequestedByMaxLength)

	for _, dep := range depInfoMap {
		uf.dependencies = append(uf.dependencies, *dep)
	}
}

// computeMainReachable returns the set of normalizedNames reachable from main (non-dev) deps
// via BFS through the forward dependency graph. Used to exclude dev-only transitive deps.
func computeMainReachable(directMainDeps map[string]bool, pkgByName map[string]*UvPackage) map[string]bool {
	reachable := make(map[string]bool)
	queue := make([]string, 0, len(directMainDeps))
	for name := range directMainDeps {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if reachable[name] {
			continue
		}
		reachable[name] = true
		if pkg, ok := pkgByName[name]; ok {
			for _, edge := range pkg.Dependencies {
				childName := normalizeName(edge.Name)
				if !reachable[childName] {
					queue = append(queue, childName)
				}
			}
		}
	}
	return reachable
}

// buildUvRequestedBy recursively builds requestedBy chains matching pip/pipenv format.
// Results are written into chains (dep ID → [][]string), not into DependencyInfo.
// parentID is the current parent's "name:version" ID.
// parentChain is the chain from parentID back to the root (not including parentID itself).
func buildUvRequestedBy(parentID string, parentChain []string, children []string, depInfoMap map[string]*DependencyInfo, fwdGraph map[string][]string, chains map[string][][]string, maxDepth int) {
	for _, childName := range children {
		child, ok := depInfoMap[childName]
		if !ok {
			continue
		}
		if len(chains[child.ID]) >= maxDepth {
			continue
		}
		// New chain entry: [parentID, ...parentChain]
		newChain := append([]string{parentID}, parentChain...)
		// Cycle check: if child's own ID already appears in the chain, skip
		for _, id := range newChain {
			if id == child.ID {
				goto next
			}
		}
		chains[child.ID] = append(chains[child.ID], newChain)
		buildUvRequestedBy(child.ID, newChain, fwdGraph[childName], depInfoMap, fwdGraph, chains, maxDepth)
	next:
	}
}

// ===== FlexPackManager Interface =====

// GetDependency returns a formatted string with dependency information.
func (uf *UvFlexPack) GetDependency() string {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s:%s\n", uf.projectName, uf.projectVersion)
	result.WriteString("Dependencies:\n")
	for _, dep := range uf.dependencies {
		fmt.Fprintf(&result, "  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type)
	}
	return result.String()
}

// ParseDependencyToList returns a list of "name:version" strings for all dependencies.
func (uf *UvFlexPack) ParseDependencyToList() []string {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	var depList []string
	for _, dep := range uf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum returns checksum maps for all dependencies.
func (uf *UvFlexPack) CalculateChecksum() []map[string]interface{} {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	var checksums []map[string]interface{}
	for _, dep := range uf.dependencies {
		checksumMap := map[string]interface{}{
			"type":    dep.Type,
			"sha1":    dep.SHA1,
			"sha256":  dep.SHA256,
			"md5":     dep.MD5,
			"id":      dep.ID,
			"scopes":  dep.Scopes,
			"name":    dep.Name,
			"version": dep.Version,
		}
		checksums = append(checksums, checksumMap)
	}
	return checksums
}

// CalculateScopes returns the unique set of scopes across all dependencies.
func (uf *UvFlexPack) CalculateScopes() []string {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	scopesMap := make(map[string]bool)
	for _, dep := range uf.dependencies {
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

// CalculateRequestedBy returns the direct parent for each dependency ID.
// Satisfies the FlexPackManager interface (returns map[string][]string).
// For the full [][]string chains, see requestedByChains.
func (uf *UvFlexPack) CalculateRequestedBy() map[string][]string {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	// Flatten each chain to its first element (direct parent) for the []string interface.
	result := make(map[string][]string)
	for depID, chains := range uf.requestedByChains {
		seen := make(map[string]bool)
		for _, chain := range chains {
			if len(chain) > 0 && !seen[chain[0]] {
				result[depID] = append(result[depID], chain[0])
				seen[chain[0]] = true
			}
		}
	}
	return result
}

// GetRequestedByChains returns the full [][]string requestedBy chains for each dependency.
// Each inner slice is a path from the immediate parent back to the root module.
// Use this when you need the complete chain (e.g. in tests or build-info consumers).
func (uf *UvFlexPack) GetRequestedByChains() map[string][][]string {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	return uf.requestedByChains
}

// ===== BuildInfoCollector Interface =====

// CollectBuildInfo builds a complete entities.BuildInfo for this UV project.
func (uf *UvFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	buildInfo := &entities.BuildInfo{
		Name:   buildName,
		Number: buildNumber,
		Agent: &entities.Agent{
			Name:    "uv",
			Version: uf.getUvVersion(),
		},
		BuildAgent: &entities.Agent{Name: "Generic", Version: "1.0"},
		Modules:    []entities.Module{},
	}

	module := entities.Module{
		Id:   fmt.Sprintf("%s:%s", uf.projectName, uf.projectVersion),
		Type: entities.Uv,
	}

	deps, err := uf.GetProjectDependencies()
	if err != nil {
		return nil, err
	}

	for _, dep := range deps {
		entityDep := entities.Dependency{
			Id:          dep.ID,
			Type:        dep.Type,
			RequestedBy: uf.requestedByChains[dep.ID], // full [][]string chains (UV-specific field)
			Checksum: entities.Checksum{
				Sha1:   dep.SHA1,
				Sha256: dep.SHA256,
				Md5:    dep.MD5,
			},
		}
		module.Dependencies = append(module.Dependencies, entityDep)
	}

	buildInfo.Modules = append(buildInfo.Modules, module)
	return buildInfo, nil
}

// GetProjectDependencies returns all project dependencies with full details.
func (uf *UvFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	return uf.dependencies, nil
}

// GetDependencyGraph returns the complete dependency graph.
func (uf *UvFlexPack) GetDependencyGraph() (map[string][]string, error) {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	return uf.depGraph, nil
}

// getUvVersion returns the installed UV version string.
func (uf *UvFlexPack) getUvVersion() string {
	cmd := exec.Command("uv", "--version")
	output, err := cmd.Output()
	if err != nil {
		log.Debug("Failed to get UV version: " + err.Error())
		return "unknown"
	}
	version := strings.TrimSpace(string(output))
	// UV version output format: "uv 0.4.10"
	if parts := strings.Fields(version); len(parts) >= 2 {
		return parts[1]
	}
	return version
}
