package apm

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
	"gopkg.in/yaml.v3"
)

const (
	apmLockfileName = "apm.lock.yaml"
	apmManifestName = "apm.yml"
	sha256Prefix    = "sha256:"
)

// ApmFlexPack implements the FlexPackManager and BuildInfoCollector interfaces for APM
// (Microsoft's Agent Package Manager). It collects dependencies by parsing apm.lock.yaml -
// the resolved graph apm itself writes after `apm install` - rather than invoking apm.
//
// Only registry-sourced dependencies (source: registry in the lockfile) are collected.
// GitHub-direct dependencies have no resolved_url/resolved_hash and aren't reproducible
// from a registry, so they're intentionally excluded, the same way local/editable/path
// dependencies are excluded for other package managers.
type ApmFlexPack struct {
	config       ApmConfig
	dependencies []entities.Dependency
	projectName  string
	projectVer   string
	initialized  bool
}

// NewApmFlexPack creates a new APM FlexPack collector. Initialization (reading apm.yml and
// apm.lock.yaml) is deferred until CollectBuildInfo is called.
func NewApmFlexPack(config ApmConfig) (*ApmFlexPack, error) {
	if config.WorkingDirectory == "" {
		config.WorkingDirectory = "."
	}
	return &ApmFlexPack{
		config: config,
	}, nil
}

// CollectBuildInfo parses apm.yml and apm.lock.yaml from the working directory and returns
// a complete BuildInfo. Both files are expected to already exist - written by a prior
// `apm install` - this method does not invoke apm itself.
func (af *ApmFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if err := af.ensureInitialized(); err != nil {
		return nil, err
	}

	buildInfo := &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "build-info-go",
			Version: "1.0.0",
		},
		BuildAgent: &entities.Agent{
			Name:    "apm",
			Version: af.getApmVersion(),
		},
		Modules: []entities.Module{},
	}

	module := entities.Module{
		Id:           af.moduleID(),
		Type:         entities.Apm,
		Dependencies: af.dependencies,
	}
	buildInfo.Modules = append(buildInfo.Modules, module)
	log.Debug(fmt.Sprintf("Collected %d dependencies for APM module %s", len(af.dependencies), module.Id))
	return buildInfo, nil
}

// ensureInitialized loads apm.yml and apm.lock.yaml and parses dependencies, once.
func (af *ApmFlexPack) ensureInitialized() error {
	if af.initialized {
		return nil
	}
	af.loadManifest()
	if err := af.parseDependenciesFromLockfile(); err != nil {
		return fmt.Errorf("failed to parse %s: %w", apmLockfileName, err)
	}
	af.initialized = true
	return nil
}

// loadManifest reads apm.yml for the project name/version used to derive the module ID.
// A missing manifest is expected and falls back silently to the working directory's
// basename. A manifest that exists but fails to parse is a real user error, so it's
// surfaced at Warn level even though moduleID() still falls back the same way.
func (af *ApmFlexPack) loadManifest() {
	data, err := os.ReadFile(filepath.Join(af.config.WorkingDirectory, apmManifestName))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn(fmt.Sprintf("Could not read %s: %s", apmManifestName, err))
		}
		return
	}
	var manifest ApmManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		log.Warn(fmt.Sprintf("Could not parse %s: %s", apmManifestName, err))
		return
	}
	af.projectName = manifest.Name
	af.projectVer = manifest.Version
}

// parseDependenciesFromLockfile reads apm.lock.yaml and builds the dependency list.
func (af *ApmFlexPack) parseDependenciesFromLockfile() error {
	lockfilePath := filepath.Join(af.config.WorkingDirectory, apmLockfileName)
	data, err := os.ReadFile(lockfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// apm doesn't write apm.lock.yaml at all for a zero-dependency project ("No
			// changes -- install state already up to date") - that's a valid module with no
			// dependencies, not a collection failure.
			log.Debug(fmt.Sprintf("No %s found, treating as zero dependencies", apmLockfileName))
			af.dependencies = []entities.Dependency{}
			return nil
		}
		return err
	}
	var lockfile ApmLockFile
	if err := yaml.Unmarshal(data, &lockfile); err != nil {
		return err
	}

	af.dependencies = make([]entities.Dependency, 0, len(lockfile.Dependencies))
	for _, pkg := range lockfile.Dependencies {
		if pkg.Source != "registry" {
			log.Debug(fmt.Sprintf("Skipping non-registry dependency %s (source=%q)", pkg.RepoURL, pkg.Source))
			continue
		}
		scopes, requestedBy := af.resolveScopeAndRequestedBy(pkg.RepoURL)
		af.dependencies = append(af.dependencies, entities.Dependency{
			Id:          pkg.RepoURL + ":" + pkg.Version,
			Type:        "zip",
			Scopes:      scopes,
			RequestedBy: requestedBy,
			Checksum: entities.Checksum{
				Sha256: sha256Hex(pkg.ResolvedHash),
			},
		})
	}
	return nil
}

// requestedByMaxPaths caps how many distinct requestedBy paths are reported per dependency,
// mirroring entities.RequestedByMaxLength - the same limit golang.go/yarn.go/uv_flexpack.go
// apply to len(dependency.RequestedBy) to bound fan-in from widely-shared packages (the
// common runaway case; a diamond dependency is exactly this: many packages sharing one base).
const requestedByMaxPaths = entities.RequestedByMaxLength

// resolveScopeAndRequestedBy shells out to `apm deps why <repoURL> --json` to determine
// whether a dependency is direct or transitive, and - for transitive ones - which package(s)
// requested it. This is preferred over the lockfile's own depth/resolved_by fields: `deps why`
// is a documented, stable command built for exactly this question, and naturally handles a
// dependency reachable through more than one parent (each returned path becomes one
// RequestedBy chain), which a single resolved_by string in the lockfile can't represent.
//
// Best-effort: if apm isn't on PATH or the command fails for any reason, this falls back to
// the previous behavior (runtime scope, no requestedBy) rather than failing the whole
// collection - the dependency's id/checksum are still correct either way.
func (af *ApmFlexPack) resolveScopeAndRequestedBy(repoURL string) (scopes []string, requestedBy [][]string) {
	// repoURL comes from apm.lock.yaml, not a trusted CLI arg - a tampered lockfile could set
	// it to something starting with "-" to smuggle an extra flag into the apm invocation
	// below. Real repo_url values are always "owner/repo"; reject anything flag-shaped instead
	// of passing it through.
	if strings.HasPrefix(repoURL, "-") {
		log.Debug(fmt.Sprintf("Refusing to run apm deps why for suspicious repo_url %q, defaulting to runtime scope", repoURL))
		return []string{"runtime"}, nil
	}

	cmd := exec.Command("apm", "deps", "why", repoURL, "--json")
	cmd.Dir = af.config.WorkingDirectory
	out, err := cmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("apm deps why %s failed, defaulting to runtime scope: %s", repoURL, err))
		return []string{"runtime"}, nil
	}
	return parseDepsWhyOutput(out, repoURL)
}

// parseDepsWhyOutput turns `apm deps why --json` output into a scope and requestedBy chains.
// Split out from resolveScopeAndRequestedBy so the parsing logic is testable without shelling
// out to a real apm binary.
func parseDepsWhyOutput(out []byte, repoURL string) (scopes []string, requestedBy [][]string) {
	var result apmDepsWhyResult
	if err := json.Unmarshal(out, &result); err != nil {
		log.Debug(fmt.Sprintf("could not parse apm deps why %s output, defaulting to runtime scope: %s", repoURL, err))
		return []string{"runtime"}, nil
	}

	if result.Package.IsDirect {
		return []string{"runtime"}, nil
	}

	for _, path := range result.Paths {
		if len(requestedBy) >= requestedByMaxPaths {
			break // widely-shared package (e.g. a diamond dependency's base) - cap fan-in
		}
		if len(path.Chain) <= 1 {
			continue // no parent to report
		}
		parents := path.Chain[:len(path.Chain)-1] // drop the target package itself
		chain := make([]string, 0, len(parents))
		for _, node := range parents {
			chain = append(chain, node.RepoURL)
		}
		requestedBy = append(requestedBy, chain)
	}
	return []string{"transitive"}, requestedBy
}

// sha256Hex extracts the hex-encoded digest from a "sha256:<hex>" string.
func sha256Hex(resolvedHash string) string {
	if !strings.HasPrefix(resolvedHash, sha256Prefix) {
		return ""
	}
	return strings.TrimPrefix(resolvedHash, sha256Prefix)
}

// moduleID returns "name:version" from apm.yml, falling back to the working directory's
// basename when the manifest is missing or incomplete.
func (af *ApmFlexPack) moduleID() string {
	if af.projectName != "" && af.projectVer != "" {
		return af.projectName + ":" + af.projectVer
	}
	if af.projectName != "" {
		return af.projectName
	}
	absDir, err := filepath.Abs(af.config.WorkingDirectory)
	if err != nil {
		return "apm-project"
	}
	base := filepath.Base(absDir)
	if base == "." || base == "" {
		return "apm-project"
	}
	return base
}

// getApmVersion parses "apm --version" output. Returns "unknown" if apm isn't on PATH -
// this is a metadata field only, collection proceeds from the lockfile either way.
func (af *ApmFlexPack) getApmVersion() string {
	out, err := exec.Command("apm", "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// GetProjectDependencies returns all collected dependencies.
func (af *ApmFlexPack) GetProjectDependencies() ([]flexpack.DependencyInfo, error) {
	if err := af.ensureInitialized(); err != nil {
		return nil, err
	}
	deps := make([]flexpack.DependencyInfo, 0, len(af.dependencies))
	for _, dep := range af.dependencies {
		deps = append(deps, flexpack.DependencyInfo{
			Type:   dep.Type,
			SHA256: dep.Sha256,
			ID:     dep.Id,
			Scopes: dep.Scopes,
		})
	}
	return deps, nil
}

// GetDependencyGraph returns the dependency graph. apm.lock.yaml is a flat list with no
// parent/child relationships, so this is always empty.
func (af *ApmFlexPack) GetDependencyGraph() (map[string][]string, error) {
	return map[string][]string{}, nil
}

// GetDependency returns a formatted string summary of dependencies.
func (af *ApmFlexPack) GetDependency() string {
	if err := af.ensureInitialized(); err != nil {
		log.Debug(fmt.Sprintf("apm: lazy init failed: %s", err))
	}
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s\n", af.moduleID())
	result.WriteString("Dependencies:\n")
	for _, dep := range af.dependencies {
		fmt.Fprintf(&result, "  - %s [%s]\n", dep.Id, dep.Sha256)
	}
	return result.String()
}

// ParseDependencyToList returns a list of dependency IDs.
func (af *ApmFlexPack) ParseDependencyToList() []string {
	if err := af.ensureInitialized(); err != nil {
		log.Debug(fmt.Sprintf("apm: lazy init failed: %s", err))
	}
	depList := make([]string, 0, len(af.dependencies))
	for _, dep := range af.dependencies {
		depList = append(depList, dep.Id)
	}
	return depList
}

// CalculateChecksum returns checksum maps for each dependency.
func (af *ApmFlexPack) CalculateChecksum() []map[string]interface{} {
	if err := af.ensureInitialized(); err != nil {
		log.Debug(fmt.Sprintf("apm: lazy init failed: %s", err))
	}
	checksums := make([]map[string]interface{}, 0, len(af.dependencies))
	for _, dep := range af.dependencies {
		checksums = append(checksums, map[string]interface{}{
			"sha256": dep.Sha256,
		})
	}
	return checksums
}

// CalculateScopes returns every distinct scope across collected dependencies (e.g. "runtime",
// "transitive" - see resolveScopeAndRequestedBy).
func (af *ApmFlexPack) CalculateScopes() []string {
	if err := af.ensureInitialized(); err != nil {
		log.Debug(fmt.Sprintf("apm: lazy init failed: %s", err))
	}
	seen := make(map[string]bool)
	var scopes []string
	for _, dep := range af.dependencies {
		for _, scope := range dep.Scopes {
			if !seen[scope] {
				seen[scope] = true
				scopes = append(scopes, scope)
			}
		}
	}
	return scopes
}

// CalculateRequestedBy returns each dependency's immediate parent(s), keyed by dependency ID.
// Mirrors UVFlexPack's convention: the FlexPackManager interface can only carry one parent
// list per ID ([]string), so each dependency's requestedBy chains (built by
// resolveScopeAndRequestedBy, one per path to a shared package) are flattened down to their
// first (closest) parent.
func (af *ApmFlexPack) CalculateRequestedBy() map[string][]string {
	if err := af.ensureInitialized(); err != nil {
		log.Debug(fmt.Sprintf("apm: lazy init failed: %s", err))
	}
	result := make(map[string][]string)
	for _, dep := range af.dependencies {
		seen := make(map[string]bool)
		for _, chain := range dep.RequestedBy {
			if len(chain) == 0 || seen[chain[0]] {
				continue
			}
			seen[chain[0]] = true
			result[dep.Id] = append(result[dep.Id], chain[0])
		}
	}
	return result
}
