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
	config         UvConfig
	lockFileData   *UvLockFile
	pyprojectData  *UvPyProjectToml
	projectName    string
	projectVersion string
	dependencies   []DependencyInfo
	depGraph       map[string][]string // normalized-name -> []normalized-name
}

// NewUvFlexPack creates a new UvFlexPack instance.
func NewUvFlexPack(config UvConfig) (*UvFlexPack, error) {
	uf := &UvFlexPack{
		config:       config,
		dependencies: []DependencyInfo{},
		depGraph:     make(map[string][]string),
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

// depFilename returns the filename-based ID for a package.
// Prefers pure-Python wheel filename, falls back to first wheel filename, then sdist filename.
// Falls back to "name-version" if no artifacts are present.
func depFilename(pkg UvPackage) string {
	for _, w := range pkg.Wheels {
		if strings.Contains(w.URL, "none-any") && w.URL != "" {
			return filepath.Base(w.URL)
		}
	}
	for _, w := range pkg.Wheels {
		if w.URL != "" {
			return filepath.Base(w.URL)
		}
	}
	if pkg.Sdist != nil && pkg.Sdist.URL != "" {
		return filepath.Base(pkg.Sdist.URL)
	}
	return fmt.Sprintf("%s-%s", pkg.Name, pkg.Version)
}

// parseDependencies populates uf.dependencies and uf.depGraph from the lock file.
func (uf *UvFlexPack) parseDependencies() {
	if uf.lockFileData == nil {
		return
	}

	// Build a name->package map (last-write-wins for resolver forks)
	pkgByName := make(map[string]*UvPackage)
	for i := range uf.lockFileData.Packages {
		pkg := &uf.lockFileData.Packages[i]
		pkgByName[normalizeName(pkg.Name)] = pkg
	}

	// Find the root workspace package (virtual or editable at ".")
	var rootPkg *UvPackage
	for i := range uf.lockFileData.Packages {
		pkg := &uf.lockFileData.Packages[i]
		if pkg.Source.Virtual == "." || pkg.Source.Editable == "." {
			rootPkg = pkg
			break
		}
	}

	// Collect direct main and dev dep names (always collect both for exclusion logic)
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

	// Build inverse RequestedBy map: depName -> []parentName
	inverse := make(map[string][]string)
	for _, pkg := range uf.lockFileData.Packages {
		if pkg.Source.IsWorkspacePackage() {
			continue
		}
		parentName := normalizeName(pkg.Name)
		for _, edge := range pkg.Dependencies {
			childName := normalizeName(edge.Name)
			inverse[childName] = append(inverse[childName], parentName)
		}
	}

	// Build dep graph and dependency list
	for _, pkg := range uf.lockFileData.Packages {
		if pkg.Source.IsWorkspacePackage() {
			continue
		}
		normalizedName := normalizeName(pkg.Name)

		// Determine scope
		var scope string
		if directMainDeps[normalizedName] {
			scope = "compile"
		} else if directDevDeps[normalizedName] {
			scope = "test"
		} else {
			// Transitive: use "compile" as default
			scope = "compile"
		}

		// Skip dev deps if not including them
		if !uf.config.IncludeDevDependencies && directDevDeps[normalizedName] {
			continue
		}

		// Build dep graph entry
		var childNames []string
		for _, edge := range pkg.Dependencies {
			childNames = append(childNames, normalizeName(edge.Name))
		}
		uf.depGraph[normalizedName] = childNames

		dep := DependencyInfo{
			ID:          depFilename(pkg),
			Name:        pkg.Name,
			Version:     pkg.Version,
			Type:        "pypi",
			SHA256:      extractSHA256(bestHash(pkg)),
			SHA1:        "",
			MD5:         "",
			Scopes:      []string{scope},
			RequestedBy: inverse[normalizedName],
		}
		uf.dependencies = append(uf.dependencies, dep)
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

// CalculateRequestedBy returns a map from dependency ID to the list of packages that request it.
func (uf *UvFlexPack) CalculateRequestedBy() map[string][]string {
	if len(uf.dependencies) == 0 {
		uf.parseDependencies()
	}
	result := make(map[string][]string)
	for _, dep := range uf.dependencies {
		if len(dep.RequestedBy) > 0 {
			result[dep.ID] = dep.RequestedBy
		}
	}
	return result
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
			Id:     dep.ID,
			Type:   dep.Type,
			Scopes: dep.Scopes,
			Checksum: entities.Checksum{
				Sha1:   dep.SHA1,
				Sha256: dep.SHA256,
				Md5:    dep.MD5,
			},
		}
		if len(dep.RequestedBy) > 0 {
			entityDep.RequestedBy = [][]string{dep.RequestedBy}
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
