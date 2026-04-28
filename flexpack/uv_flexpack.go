package flexpack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// UVLockFile represents the top-level structure of uv.lock
type UVLockFile struct {
	Version  int          `toml:"version"`
	Revision int          `toml:"revision"`
	Packages []UVPackage  `toml:"package"`
}

// UVPackage represents a [[package]] entry in uv.lock
type UVPackage struct {
	Name            string                         `toml:"name"`
	Version         string                         `toml:"version"`
	Source          UVSource                       `toml:"source"`
	Dependencies    []UVDependencyEdge             `toml:"dependencies"`
	DevDependencies map[string][]UVDependencyEdge  `toml:"dev-dependencies"`
	Sdist           *UVArtifact                    `toml:"sdist"`
	Wheels          []UVArtifact                   `toml:"wheels"`
}

// UVSource is an inline table with exactly one key identifying the source type.
type UVSource struct {
	Registry  string `toml:"registry"`
	Virtual   string `toml:"virtual"`
	Editable  string `toml:"editable"`
	Directory string `toml:"directory"`
	Git       string `toml:"git"`
	URL       string `toml:"url"`
}

// IsWorkspacePackage returns true if this source represents a local workspace package.
func (s UVSource) IsWorkspacePackage() bool {
	return s.Virtual != "" || s.Editable != "" || s.Directory != ""
}

// UVArtifact represents an sdist or wheel entry in uv.lock
type UVArtifact struct {
	URL        string `toml:"url"`
	Path       string `toml:"path"`
	Hash       string `toml:"hash"`        // "sha256:<hex>"; absent for git
	Size       int64  `toml:"size"`
	UploadTime string `toml:"upload-time"` // ISO 8601; may be absent (revision < 3)
}

// UVDependencyEdge represents a dependency reference inside a [[package]] entry
type UVDependencyEdge struct {
	Name    string   `toml:"name"`
	Marker  string   `toml:"marker"`
	Extra   []string `toml:"extra"`
	Version string   `toml:"version"`
}

// UVPyProjectToml reads only [project] (PEP 621) — UV format, not Poetry
type UVPyProjectToml struct {
	Project struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
	} `toml:"project"`
}

// UVFlexPack implements FlexPackManager and BuildInfoCollector for the UV package manager
type UVFlexPack struct {
	config            UVConfig
	lockFileData      *UVLockFile
	pyprojectData     *UVPyProjectToml
	projectName       string
	projectVersion    string
	parsed            bool
	dependencies      []DependencyInfo
	depGraph          map[string][]string   // dep ID ("name:version") -> []dep IDs
	requestedByChains map[string][][]string // dep ID -> full chains back to root (UV-specific)
}

// NewUVFlexPack creates a new UVFlexPack instance.
func NewUVFlexPack(config UVConfig) (*UVFlexPack, error) {
	uf := &UVFlexPack{
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
func (uf *UVFlexPack) loadPyProjectToml() error {
	// When ProjectName is supplied via config (e.g. for PEP 723 inline scripts that
	// have no pyproject.toml), skip file loading and use the overrides directly.
	if uf.config.ProjectName != "" {
		uf.projectName = uf.config.ProjectName
		uf.projectVersion = uf.config.ProjectVersion
		return nil
	}
	pyprojectPath := filepath.Join(uf.config.WorkingDirectory, "pyproject.toml")
	data, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return fmt.Errorf("failed to read pyproject.toml: %w", err)
	}
	uf.pyprojectData = &UVPyProjectToml{}
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

// loadUvLock reads and parses the lock file. Uses LockFilePath if set (for PEP 723
// inline scripts whose lock file is adjacent to the script), otherwise uv.lock.
func (uf *UVFlexPack) loadUvLock() error {
	lockPath := uf.config.LockFilePath
	if lockPath == "" {
		lockPath = filepath.Join(uf.config.WorkingDirectory, "uv.lock")
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read uv.lock: %w", err)
	}
	uf.lockFileData = &UVLockFile{}
	if err := toml.Unmarshal(data, uf.lockFileData); err != nil {
		return fmt.Errorf("failed to parse uv.lock: %w", err)
	}
	return nil
}

var pep503Re = regexp.MustCompile(`[-_.]+`)

// normalizeName converts package names to lowercase with hyphens per PEP 503.
func normalizeName(name string) string {
	return pep503Re.ReplaceAllString(strings.ToLower(name), "-")
}

// extractSHA256 strips the "sha256:" prefix from a hash string.
func extractSHA256(hash string) string {
	return strings.TrimPrefix(hash, "sha256:")
}

// bestHash returns the best available hash for a package.
// Prefers pure-Python wheel (none-any), falls back to first wheel, then sdist.
func bestHash(pkg UVPackage) string {
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
func depFileType(pkg UVPackage) string {
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
		// Git deps have no archive — return "git" so build-info reflects the source type.
		if pkg.Source.Git != "" {
			return "git"
		}
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

// ensureParsed calls parseDependencies exactly once.
func (uf *UVFlexPack) ensureParsed() {
	if uf.parsed {
		return
	}
	uf.parseDependencies()
	uf.parsed = true
}

// parseDependencies populates uf.dependencies and uf.depGraph from the lock file.
// ID format and requestedBy chains match the pip/pipenv canonical build-info format:
//   - dep ID:  "name:version"  (e.g. "certifi:2026.2.25")
//   - dep type: file extension (e.g. "whl", "tar.gz")
//   - requestedBy: full chain back to root module (e.g. [["requests:2.33.1","myapp:0.1.0"]])
//   - direct deps: requestedBy = [["myapp:0.1.0"]]
//   - no scopes (Python has no compile/runtime distinction; matches pip/pipenv)
func (uf *UVFlexPack) parseDependencies() {
	if uf.lockFileData == nil {
		return
	}

	moduleID := fmt.Sprintf("%s:%s", uf.projectName, uf.projectVersion)
	pkgByName := buildPackageMap(uf.lockFileData.Packages)
	rootPkg := findRootPackage(uf.lockFileData.Packages)

	var depInfoMap map[string]*DependencyInfo
	var rootChildren []string

	if uf.config.InstalledPackages != nil {
		// Ground-truth path: only include packages that uv actually installed.
		// This correctly handles --no-dev, --only-dev, --group, --no-group and all
		// other flag combinations without any flag parsing on our side.
		depInfoMap = buildDepInfoMapFromInstalled(uf.lockFileData.Packages, uf.config.InstalledPackages)
		rootChildren = collectRootChildrenFromInstalled(rootPkg, depInfoMap)
	} else {
		// Fallback path (lock/build/publish — no venv): use IncludeDevDependencies flag.
		mainDeps, devDeps := collectDirectDeps(rootPkg)
		var mainReachable map[string]bool
		if !uf.config.IncludeDevDependencies && rootPkg != nil &&
			len(mainDeps) > 0 && len(devDeps) > 0 {
			mainReachable = computeMainReachable(mainDeps, pkgByName)
		}
		depInfoMap = buildDepInfoMap(uf.lockFileData.Packages, uf.config.IncludeDevDependencies, mainReachable, devDeps)
		rootChildren = collectRootChildren(rootPkg, depInfoMap, uf.config.IncludeDevDependencies)
	}

	fwdGraph := buildForwardGraph(depInfoMap, pkgByName, uf.depGraph)

	// Build requestedBy chains using pip's recursive DFS approach.
	// Results go into uf.requestedByChains (map[string][][]string), NOT into DependencyInfo.RequestedBy.
	// This keeps the shared DependencyInfo type ([]string) unchanged for poetry/maven.
	buildUvRequestedBy(moduleID, []string{}, rootChildren, depInfoMap, fwdGraph, uf.requestedByChains, entities.RequestedByMaxLength)

	for _, dep := range depInfoMap {
		uf.dependencies = append(uf.dependencies, *dep)
	}
}

// buildPackageMap builds a normalizedName → *UVPackage lookup map.
func buildPackageMap(packages []UVPackage) map[string]*UVPackage {
	pkgByName := make(map[string]*UVPackage, len(packages))
	for i := range packages {
		pkg := &packages[i]
		pkgByName[normalizeName(pkg.Name)] = pkg
	}
	return pkgByName
}

// findRootPackage returns the workspace root package (source.virtual/editable/directory == ".").
func findRootPackage(packages []UVPackage) *UVPackage {
	for i := range packages {
		pkg := &packages[i]
		if pkg.Source.Virtual == "." || pkg.Source.Editable == "." || pkg.Source.Directory == "." {
			return pkg
		}
	}
	return nil
}

// collectDirectDeps returns the direct main and dev dep normalised names from the root package.
func collectDirectDeps(rootPkg *UVPackage) (mainDeps, devDeps map[string]bool) {
	mainDeps = make(map[string]bool)
	devDeps = make(map[string]bool)
	if rootPkg == nil {
		return
	}
	for _, edge := range rootPkg.Dependencies {
		mainDeps[normalizeName(edge.Name)] = true
	}
	for _, edges := range rootPkg.DevDependencies {
		for _, edge := range edges {
			devDeps[normalizeName(edge.Name)] = true
		}
	}
	return
}

// buildDepInfoMap builds normalizedName → *DependencyInfo for non-workspace packages,
// applying dev-dep exclusion logic.
// Exclusion logic when includeDevDeps=false:
//   - With reachability analysis (mainReachable != nil): skip anything not reachable from main deps
//   - Without (no dev deps or no main deps declared): skip direct dev deps only
func buildDepInfoMap(packages []UVPackage, includeDevDeps bool, mainReachable map[string]bool, directDevDeps map[string]bool) map[string]*DependencyInfo {
	depInfoMap := make(map[string]*DependencyInfo)
	for i := range packages {
		pkg := &packages[i]
		if pkg.Source.IsWorkspacePackage() {
			continue
		}
		normalizedName := normalizeName(pkg.Name)
		if !includeDevDeps {
			if mainReachable != nil {
				if !mainReachable[normalizedName] {
					continue // reachability analysis: exclude dev-only transitive deps
				}
			} else if directDevDeps[normalizedName] {
				continue // simple fallback: exclude direct dev deps only
			}
		}
		// DirectURL is set for deps not from a registry: direct URL or git.
		// Both are not in Artifactory so AQL enrichment is skipped for them.
		directURL := pkg.Source.URL
		if directURL == "" {
			directURL = pkg.Source.Git
		}
		depInfoMap[normalizedName] = &DependencyInfo{
			ID:        fmt.Sprintf("%s:%s", pkg.Name, pkg.Version),
			Name:      pkg.Name,
			Version:   pkg.Version,
			Type:      depFileType(*pkg),
			SHA256:    extractSHA256(bestHash(*pkg)),
			DirectURL: directURL,
		}
	}
	return depInfoMap
}

// buildForwardGraph builds the normalizedName forward graph and returns it.
// Also populates idGraph with ID-keyed edges.
func buildForwardGraph(depInfoMap map[string]*DependencyInfo, pkgByName map[string]*UVPackage, idGraph map[string][]string) map[string][]string {
	fwdGraph := make(map[string][]string, len(depInfoMap))
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
		idGraph[depInfoMap[normalizedName].ID] = func() []string {
			var ids []string
			for _, c := range children {
				ids = append(ids, depInfoMap[c].ID)
			}
			return ids
		}()
	}
	return fwdGraph
}

// collectRootChildren returns normalised child names reachable from root that are in depInfoMap.
// buildDepInfoMapFromInstalled builds depInfoMap using the ground-truth installed set
// from `uv pip list`. Only packages whose normalised name appears in installedPkgs are included.
func buildDepInfoMapFromInstalled(packages []UVPackage, installedPkgs map[string]string) map[string]*DependencyInfo {
	depInfoMap := make(map[string]*DependencyInfo)
	for i := range packages {
		pkg := &packages[i]
		if pkg.Source.IsWorkspacePackage() {
			continue
		}
		normName := normalizeName(pkg.Name)
		if _, ok := installedPkgs[normName]; !ok {
			continue // not installed by this uv invocation
		}
		directURL := pkg.Source.URL
		if directURL == "" {
			directURL = pkg.Source.Git
		}
		depInfoMap[normName] = &DependencyInfo{
			ID:        fmt.Sprintf("%s:%s", pkg.Name, pkg.Version),
			Name:      pkg.Name,
			Version:   pkg.Version,
			Type:      depFileType(*pkg),
			SHA256:    extractSHA256(bestHash(*pkg)),
			DirectURL: directURL,
		}
	}
	return depInfoMap
}

// collectRootChildrenFromInstalled returns root's direct children that are in depInfoMap,
// scanning both main and dev dependency edges (since InstalledPackages already filtered the set).
func collectRootChildrenFromInstalled(rootPkg *UVPackage, depInfoMap map[string]*DependencyInfo) []string {
	if rootPkg == nil {
		var all []string
		for n := range depInfoMap {
			all = append(all, n)
		}
		return all
	}
	var children []string
	for _, edge := range rootPkg.Dependencies {
		n := normalizeName(edge.Name)
		if _, ok := depInfoMap[n]; ok {
			children = append(children, n)
		}
	}
	for _, edges := range rootPkg.DevDependencies {
		for _, edge := range edges {
			n := normalizeName(edge.Name)
			if _, ok := depInfoMap[n]; ok {
				children = append(children, n)
			}
		}
	}
	return children
}

func collectRootChildren(rootPkg *UVPackage, depInfoMap map[string]*DependencyInfo, includeDevDeps bool) []string {
	if rootPkg == nil {
		// No lockfile root found — treat all non-workspace packages as direct deps
		rootChildren := make([]string, 0, len(depInfoMap))
		for n := range depInfoMap {
			rootChildren = append(rootChildren, n)
		}
		return rootChildren
	}
	var rootChildren []string
	for _, edge := range rootPkg.Dependencies {
		n := normalizeName(edge.Name)
		if _, ok := depInfoMap[n]; ok {
			rootChildren = append(rootChildren, n)
		}
	}
	if includeDevDeps {
		for _, edges := range rootPkg.DevDependencies {
			for _, edge := range edges {
				n := normalizeName(edge.Name)
				if _, ok := depInfoMap[n]; ok {
					rootChildren = append(rootChildren, n)
				}
			}
		}
	}
	return rootChildren
}

// computeMainReachable returns the set of normalizedNames reachable from main (non-dev) deps
// via BFS through the forward dependency graph. Used to exclude dev-only transitive deps.
func computeMainReachable(directMainDeps map[string]bool, pkgByName map[string]*UVPackage) map[string]bool {
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
func (uf *UVFlexPack) GetDependency() string {
	uf.ensureParsed()
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s:%s\n", uf.projectName, uf.projectVersion)
	result.WriteString("Dependencies:\n")
	for _, dep := range uf.dependencies {
		fmt.Fprintf(&result, "  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type)
	}
	return result.String()
}

// ParseDependencyToList returns a list of "name:version" strings for all dependencies.
func (uf *UVFlexPack) ParseDependencyToList() []string {
	uf.ensureParsed()
	var depList []string
	for _, dep := range uf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum returns checksum maps for all dependencies.
func (uf *UVFlexPack) CalculateChecksum() []map[string]interface{} {
	uf.ensureParsed()
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
func (uf *UVFlexPack) CalculateScopes() []string {
	uf.ensureParsed()
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
func (uf *UVFlexPack) CalculateRequestedBy() map[string][]string {
	uf.ensureParsed()
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
func (uf *UVFlexPack) GetRequestedByChains() map[string][][]string {
	uf.ensureParsed()
	return uf.requestedByChains
}

// ===== BuildInfoCollector Interface =====

// CollectBuildInfo builds a complete entities.BuildInfo for this UV project.
func (uf *UVFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
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
func (uf *UVFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	uf.ensureParsed()
	return uf.dependencies, nil
}

// GetDirectURLDeps returns a map of dep ID ("name:version") → source URL for all
// dependencies that were installed from a direct URL rather than from a registry.
// These deps are not in Artifactory so sha1/md5 enrichment via AQL should be skipped.
func (uf *UVFlexPack) GetDirectURLDeps() map[string]string {
	uf.ensureParsed()
	result := make(map[string]string)
	for _, dep := range uf.dependencies {
		if dep.DirectURL != "" {
			result[dep.ID] = dep.DirectURL
		}
	}
	return result
}

// GetDependencyGraph returns the complete dependency graph.
func (uf *UVFlexPack) GetDependencyGraph() (map[string][]string, error) {
	uf.ensureParsed()
	return uf.depGraph, nil
}

// getUvVersion returns the installed UV version string.
func (uf *UVFlexPack) getUvVersion() string {
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
