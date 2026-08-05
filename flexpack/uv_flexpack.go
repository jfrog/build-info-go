package flexpack

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// UVLockFile represents the top-level structure of uv.lock
type UVLockFile struct {
	Version  int         `toml:"version"`
	Revision int         `toml:"revision"`
	Packages []UVPackage `toml:"package"`
}

// UVPackage represents a [[package]] entry in uv.lock
type UVPackage struct {
	Name            string                        `toml:"name"`
	Version         string                        `toml:"version"`
	Source          UVSource                      `toml:"source"`
	Dependencies    []UVDependencyEdge            `toml:"dependencies"`
	DevDependencies map[string][]UVDependencyEdge `toml:"dev-dependencies"`
	Sdist           *UVArtifact                   `toml:"sdist"`
	Wheels          []UVArtifact                  `toml:"wheels"`
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
	Hash       string `toml:"hash"` // "sha256:<hex>"; absent for git
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
		Name    string   `toml:"name"`
		Version string   `toml:"version"`
		Dynamic []string `toml:"dynamic"`
	} `toml:"project"`
}

// UVFlexPack implements FlexPackManager and BuildInfoCollector for the UV package manager
type UVFlexPack struct {
	config            UVConfig
	lockFileData      *UVLockFile
	pyprojectData     *UVPyProjectToml
	projectName       string
	projectVersion    string
	versionIsDynamic  bool // true when pyproject.toml declares `dynamic = ["version"]`
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
	if uf.projectVersion == "" && uf.versionIsDynamic {
		if resolved := uf.resolveDynamicVersion(); resolved != "" {
			uf.projectVersion = resolved
		} else {
			// Same tolerance as the PEP 723 inline-script case above: an empty version
			// still yields a usable "name:" module ID, and failing outright here would
			// throw away dependency/artifact collection too, not just the version.
			log.Warn("UV: project declares dynamic = [\"version\"] but the resolved version could not be found " +
				"(checked uv.lock's workspace root package, installed packages, and dist/ artifacts); " +
				"build info will use an empty version in the module ID")
		}
	}
	return uf, nil
}

// loadPyProjectToml reads and parses pyproject.toml using PEP 621 [project] section.
func (uf *UVFlexPack) loadPyProjectToml() error {
	// When ProjectName is supplied via config (e.g. for PEP 723 inline scripts that
	// have no pyproject.toml), skip file loading and use the overrides directly.
	if uf.config.ProjectName != "" {
		uf.projectName = uf.config.ProjectName
		// ProjectVersion may be empty for PEP 723 inline scripts; results in "name:" module ID which is acceptable
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
	uf.versionIsDynamic = isVersionDynamic(uf.pyprojectData.Project.Dynamic)
	if uf.projectName == "" {
		return fmt.Errorf("project name not found in pyproject.toml (checked [project.name])")
	}
	// A dynamic version (PEP 621 `dynamic = ["version"]`, e.g. via hatch-vcs) is resolved
	// later, once uv.lock/dist/installed-packages state is available — see resolveDynamicVersion.
	if uf.projectVersion == "" && !uf.versionIsDynamic {
		return fmt.Errorf("project version not found in pyproject.toml (checked [project.version] and [project.dynamic] for \"version\")")
	}
	return nil
}

// isVersionDynamic reports whether "version" appears in a PEP 621 [project.dynamic] list.
func isVersionDynamic(dynamic []string) bool {
	for _, field := range dynamic {
		if field == "version" {
			return true
		}
	}
	return false
}

// resolveDynamicVersion finds the backend-resolved version for a project whose
// pyproject.toml only declares `dynamic = ["version"]` (e.g. via hatch-vcs). uv.lock's
// workspace root package entry ([[package]] with source.virtual/editable/directory == ".")
// does NOT carry a version field in practice — verified empirically against real `uv lock`
// output, which omits it entirely for the root/editable entry regardless of a static or
// dynamic pyproject.toml version. The real resolved version instead shows up in whichever
// of these the current uv invocation already produced, checked in order:
//  1. uv.lock's root package version, in case a future uv release (or another tool) does
//     populate it — cheap to check, correct if present.
//  2. UVConfig.InstalledPackages (populated from `uv pip list` for sync/install/add/remove),
//     which includes the project's own resolved version once it's installed into the venv.
//  3. dist/*.whl or dist/*.tar.gz archives — build/publish only reach this point after
//     `uv build`/`uv publish` already succeeded, and each archive's own embedded package
//     metadata (wheel: *.dist-info/METADATA; sdist: top-level PKG-INFO) records the version
//     the backend resolved for it — see resolveVersionFromDist.
//
// Returns "" if none of these have anything to offer.
func (uf *UVFlexPack) resolveDynamicVersion() string {
	if uf.lockFileData != nil {
		if rootPkg := findRootPackage(uf.lockFileData.Packages); rootPkg != nil && rootPkg.Version != "" {
			return rootPkg.Version
		}
	}
	if uf.config.InstalledPackages != nil {
		// Compare via normalizeName on both sides rather than indexing by a normalized key
		// directly: callers (e.g. jfrog-cli-artifactory's uvInstalledPackages) build this map
		// with their own lowercase/underscore-only normalization, which doesn't collapse dots
		// or repeated separators the way full PEP 503 normalization does — a project name
		// containing "." would silently never match a plain map lookup.
		want := normalizeName(uf.projectName)
		for pkgName, pkgVersion := range uf.config.InstalledPackages {
			if pkgVersion != "" && normalizeName(pkgName) == want {
				return pkgVersion
			}
		}
	}
	return uf.resolveVersionFromDist()
}

// resolveVersionFromDist scans WorkingDirectory/dist for wheel/sdist archives belonging to
// this project and returns the resolved version. `uv version` refuses outright to report a
// dynamic version under any circumstance (verified empirically: it errors even right after a
// successful build or sync), so there's no CLI command to ask — but the build backend already
// wrote the real, resolved version into the archive's own package metadata (wheel: a
// `*.dist-info/METADATA` entry; sdist: a top-level `PKG-INFO` file), the same metadata that
// would be published to the index. Reading that directly is strictly more reliable than
// parsing it out of the filename: no PEP 427/625 name-escaping to get right, and the archive's
// declared Name is checked against the project name instead of assumed from a prefix match.
//
// A single `uv build` produces both a wheel and an sdist for the same version, but uv does not
// clean dist/ between builds — it can accumulate archives from earlier versions too. When
// matching archives disagree on version, the most recently modified one wins, since that's the
// artifact the build/publish invocation currently in progress just produced.
func (uf *UVFlexPack) resolveVersionFromDist() string {
	entries, err := os.ReadDir(filepath.Join(uf.config.WorkingDirectory, "dist"))
	if err != nil {
		return ""
	}
	wantName := normalizeName(uf.projectName)
	var bestVersion string
	var bestModTime time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(uf.config.WorkingDirectory, "dist", entry.Name())
		lower := strings.ToLower(entry.Name())
		var name, version string
		switch {
		case strings.HasSuffix(lower, ".whl"):
			name, version = readWheelMetadata(path)
		case strings.HasSuffix(lower, ".tar.gz"):
			name, version = readSdistMetadata(path)
		default:
			continue
		}
		if version == "" || normalizeName(name) != wantName {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if bestVersion == "" || info.ModTime().After(bestModTime) {
			bestVersion = version
			bestModTime = info.ModTime()
		}
	}
	return bestVersion
}

// readWheelMetadata extracts Name/Version from a wheel's `*.dist-info/METADATA` entry.
// Returns ("", "") if the file can't be read as a wheel or has no readable metadata.
func readWheelMetadata(path string) (name, version string) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", ""
	}
	defer func() { _ = r.Close() }()
	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, ".dist-info/METADATA") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", ""
		}
		defer func() { _ = rc.Close() }()
		return parsePackageMetadata(rc)
	}
	return "", ""
}

// readSdistMetadata extracts Name/Version from an sdist's top-level `PKG-INFO` file.
// Returns ("", "") if the file can't be read as a gzipped tarball or has no PKG-INFO.
func readSdistMetadata(path string) (name, version string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", ""
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", ""
		}
		if err != nil {
			return "", ""
		}
		// The authoritative PKG-INFO sits directly under the single top-level
		// "{name}-{version}/" directory — i.e. exactly one path separator. A deeper match
		// (e.g. "{name}-{version}/{name}.egg-info/PKG-INFO", which some sdists accidentally
		// bundle) belongs to a nested, possibly stale build artifact and must be skipped, even
		// if tar ordering happens to list it before the real top-level one.
		if hdr.Name == "PKG-INFO" || (strings.Count(hdr.Name, "/") == 1 && strings.HasSuffix(hdr.Name, "/PKG-INFO")) {
			return parsePackageMetadata(tr)
		}
	}
}

// parsePackageMetadata reads Name/Version from a PEP 566 core-metadata document (email-header
// style: "Key: value" lines followed by a blank line, then an optional free-text description).
// Header parsing stops at the first blank line so header-like text inside the description
// (e.g. a changelog entry starting with "Version:") is never mistaken for a real field.
func parsePackageMetadata(r io.Reader) (name, version string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		switch {
		case name == "" && strings.HasPrefix(line, "Name:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case version == "" && strings.HasPrefix(line, "Version:"):
			version = strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
		}
	}
	return name, version
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

// selectPackageHash returns the best available hash for a package.
// Prefers pure-Python wheel (none-any), falls back to first wheel, then sdist.
func selectPackageHash(pkg UVPackage) string {
	var firstWheelHash string
	for _, w := range pkg.Wheels {
		if w.Hash == "" {
			continue
		}
		if strings.Contains(w.URL, "none-any") {
			return w.Hash // universal wheel — best choice
		}
		if firstWheelHash == "" {
			firstWheelHash = w.Hash
		}
	}
	if firstWheelHash != "" {
		return firstWheelHash
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
	var selectedURL string
	for _, w := range pkg.Wheels {
		if w.URL == "" {
			continue
		}
		if strings.Contains(w.URL, "none-any") {
			selectedURL = w.URL
			break // universal wheel — best choice
		}
		if selectedURL == "" {
			selectedURL = w.URL // first platform wheel as fallback
		}
	}
	if selectedURL == "" && pkg.Sdist != nil && pkg.Sdist.URL != "" {
		selectedURL = pkg.Sdist.URL
	}
	if selectedURL == "" {
		if pkg.Source.Git != "" {
			return "git"
		}
		return ""
	}
	base := filepath.Base(selectedURL)
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
			excluded := (mainReachable != nil && !mainReachable[normalizedName]) ||
				(mainReachable == nil && directDevDeps[normalizedName])
			if excluded {
				continue
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
			SHA256:    extractSHA256(selectPackageHash(*pkg)),
			DirectURL: directURL,
		}
	}
	return depInfoMap
}

// buildForwardGraph builds the normalizedName forward graph and returns it.
// Also populates idGraph with ID-keyed edges.
func buildForwardGraph(depInfoMap map[string]*DependencyInfo, pkgByName map[string]*UVPackage, idGraph map[string][]string) map[string][]string {
	fwdGraph := make(map[string][]string, len(depInfoMap))
	for normalizedName, info := range depInfoMap {
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
		idGraph[info.ID] = func() []string {
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
			SHA256:    extractSHA256(selectPackageHash(*pkg)),
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
		hasCycle := false
		for _, id := range newChain {
			if id == child.ID {
				hasCycle = true
				break
			}
		}
		if !hasCycle {
			chains[child.ID] = append(chains[child.ID], newChain)
			buildUvRequestedBy(child.ID, newChain, fwdGraph[childName], depInfoMap, fwdGraph, chains, maxDepth)
		}
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
