package flexpack

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// gemDepType is the build-info dependency/artifact type for RubyGems packages.
const gemDepType = "gem"

// bundlerSelfGemName is Bundler's own gem. It is always present in
// Bundler.definition.specs (Bundler depends on itself to run), but it is never a real
// project dependency, so it is filtered out of the collected dependency set.
const bundlerSelfGemName = "bundler"

// bundlerGemSpec mirrors one entry of the JSON array emitted by
// bundlerDependencyGraphScript: a single resolved gem from Bundler's own dependency graph.
type bundlerGemSpec struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Deps    []string `json:"deps"`
	// Groups is non-nil only for gems declared directly in the Gemfile (i.e. present in
	// Bundler.definition.dependencies). Transitive gems get no group of their own and
	// inherit one from their parent chain (see assignScopes/propagateScopes).
	Groups []string `json:"groups"`
	// Source is "GEM" (registry), "GIT", or "PATH".
	Source string `json:"source"`
	// Remote is the git/path location for GIT/PATH gems; empty for registry gems.
	Remote string `json:"remote"`
}

// bundlerDependencyGraphScript asks Bundler itself, via its own public Definition API,
// for the resolved dependency graph, each direct dependency's Gemfile groups, and each
// gem's source. This is read straight from Bundler.definition rather than by parsing
// Gemfile.lock: Bundler owns that file's on-disk format and may change it across
// versions, while Definition's public API is Bundler's own stable contract.
const bundlerDependencyGraphScript = `
require "bundler"
require "json"

definition = Bundler.definition
direct_groups = {}
definition.dependencies.each { |dep| direct_groups[dep.name] = dep.groups.map(&:to_s) }

gems = definition.specs.map do |spec|
  git_source  = spec.source.is_a?(Bundler::Source::Git)
  path_source = !git_source && spec.source.is_a?(Bundler::Source::Path)
  # Match Gemfile.lock's own "name (version-platform)" convention for platform-specific
  # gems (e.g. native extensions like nokogiri): spec.version alone drops the platform.
  platform = spec.platform.to_s
  version  = platform == "ruby" ? spec.version.to_s : "#{spec.version}-#{platform}"
  {
    name:    spec.name,
    version: version,
    deps:    spec.dependencies.select { |d| d.type == :runtime }.map(&:name),
    groups:  direct_groups[spec.name],
    source:  git_source ? "GIT" : (path_source ? "PATH" : "GEM"),
    remote:  git_source ? spec.source.uri : (path_source ? spec.source.path.to_s : nil),
  }
end

puts JSON.generate(gems)
`

// RubygemsFlexPack implements FlexPackManager and BuildInfoCollector for RubyGems / Bundler.
//   - dep ID:      "name:version"   (e.g. "rake:13.0.6")
//   - dep type:    "gem"
//   - requestedBy: full chain back to the root module
//   - scopes:      Bundler's own Gemfile group names (e.g. "default", "development"),
//     read live from Bundler rather than a hardcoded label.
//
// Bundler's dependency graph carries no checksums, so sha1/sha256/md5 enrichment is left
// to the JFrog CLI layer (Artifactory AQL).
type RubygemsFlexPack struct {
	config            GemConfig
	bundlerGems       []bundlerGemSpec
	projectName       string
	projectVersion    string
	parsed            bool
	dependencies      []DependencyInfo
	depGraph          map[string][]string   // dep ID ("name:version") -> []dep IDs
	requestedByChains map[string][][]string // dep ID -> full chains back to root
}

// NewRubygemsFlexPack creates a new RubygemsFlexPack instance.
func NewRubygemsFlexPack(config GemConfig) (*RubygemsFlexPack, error) {
	if config.BundleExecutable == "" {
		config.BundleExecutable = "bundle"
	}
	rf := &RubygemsFlexPack{
		config:            config,
		dependencies:      []DependencyInfo{},
		depGraph:          make(map[string][]string),
		requestedByChains: make(map[string][][]string),
	}
	rf.resolveProjectIdentity()
	gems, err := rf.loadBundlerDependencyGraph()
	if err != nil {
		log.Debug("Failed to query Bundler dependency graph, dependency collection will be empty: " + err.Error())
	}
	rf.bundlerGems = gems
	return rf, nil
}

// resolveProjectIdentity derives the module name/version. Gemfile-only projects have no
// inherent name/version, so the working-directory base name is used unless overridden.
func (rf *RubygemsFlexPack) resolveProjectIdentity() {
	rf.projectName = rf.config.ProjectName
	rf.projectVersion = rf.config.ProjectVersion
	if rf.projectName == "" {
		if rf.config.WorkingDirectory != "" {
			rf.projectName = filepath.Base(rf.config.WorkingDirectory)
		}
		if rf.projectName == "" || rf.projectName == "." || rf.projectName == string(filepath.Separator) {
			rf.projectName = "ruby-project"
		}
	}
}

// loadBundlerDependencyGraph runs bundlerDependencyGraphScript via Bundler and decodes
// its JSON output.
func (rf *RubygemsFlexPack) loadBundlerDependencyGraph() ([]bundlerGemSpec, error) {
	cmd := exec.Command(rf.config.BundleExecutable, "exec", "ruby", "-e", bundlerDependencyGraphScript) // #nosec G204 -- executable is operator-configured, not user input
	cmd.Dir = rf.config.WorkingDirectory
	if gemfile := rf.gemfileOverride(); gemfile != "" {
		cmd.Env = append(os.Environ(), "BUNDLE_GEMFILE="+gemfile)
	}

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("bundler dependency graph query failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("bundler dependency graph query failed: %w", err)
	}

	var gems []bundlerGemSpec
	if err := json.Unmarshal(output, &gems); err != nil {
		return nil, fmt.Errorf("failed to parse Bundler dependency graph output: %w", err)
	}
	return gems, nil
}

// gemfileOverride derives a Gemfile path from LockFilePath (stripping the ".lock"
// suffix), for BUNDLE_GEMFILE, when LockFilePath is set.
func (rf *RubygemsFlexPack) gemfileOverride() string {
	if rf.config.LockFilePath == "" {
		return ""
	}
	return strings.TrimSuffix(rf.config.LockFilePath, ".lock")
}

// ensureParsed builds the dependency model exactly once.
func (rf *RubygemsFlexPack) ensureParsed() {
	if rf.parsed {
		return
	}
	rf.parseDependencies()
	rf.parsed = true
}

// parseDependencies populates rf.dependencies, rf.depGraph and rf.requestedByChains from
// the gems reported by Bundler.
func (rf *RubygemsFlexPack) parseDependencies() {
	if len(rf.bundlerGems) == 0 {
		return
	}

	moduleID := rf.moduleID()

	specByName := make(map[string]*bundlerGemSpec, len(rf.bundlerGems))
	for i := range rf.bundlerGems {
		spec := &rf.bundlerGems[i]
		if spec.Name == bundlerSelfGemName {
			continue
		}
		specByName[spec.Name] = spec
	}

	// Build dep info map keyed by exact gem name. When InstalledPackages is provided,
	// only the gems actually installed are included (handles bundler group filtering).
	depInfoMap := make(map[string]*DependencyInfo, len(specByName))
	for name, spec := range specByName {
		if rf.config.InstalledPackages != nil {
			if _, ok := rf.config.InstalledPackages[name]; !ok {
				continue
			}
		}
		// GIT/PATH gems are not stored in Artifactory; flag them so the CLI layer
		// can skip checksum enrichment for them.
		directURL := ""
		if spec.Source == "GIT" || spec.Source == "PATH" {
			directURL = spec.Remote
		}
		depInfoMap[name] = &DependencyInfo{
			ID:        fmt.Sprintf("%s:%s", spec.Name, spec.Version),
			Name:      spec.Name,
			Version:   spec.Version,
			Type:      gemDepType,
			DirectURL: directURL,
		}
	}

	// Forward graph (name → child names) limited to gems present in depInfoMap, plus the
	// Gemfile groups Bundler reported for direct dependencies.
	fwdGraph := make(map[string][]string, len(depInfoMap))
	directGroups := make(map[string][]string)
	for name, info := range depInfoMap {
		spec := specByName[name]
		var children []string
		var childIDs []string
		for _, child := range spec.Deps {
			if childInfo, ok := depInfoMap[child]; ok {
				children = append(children, child)
				childIDs = append(childIDs, childInfo.ID)
			}
		}
		fwdGraph[name] = children
		rf.depGraph[info.ID] = childIDs

		if spec.Groups != nil {
			directGroups[name] = spec.Groups
		}
	}

	rootChildren := rf.collectRootChildren(depInfoMap, directGroups)
	rf.assignScopes(depInfoMap, fwdGraph, rootChildren, directGroups)

	// Reuse the shared chain builder (defined in uv_flexpack.go) — it operates purely
	// on depInfoMap + fwdGraph keys, so exact gem names work the same as UV's normalised names.
	buildRequestedByChains(moduleID, []string{}, rootChildren, depInfoMap, fwdGraph, rf.requestedByChains, entities.RequestedByMaxLength)

	for _, dep := range depInfoMap {
		rf.dependencies = append(rf.dependencies, *dep)
	}
}

// assignScopes classifies dependencies using Bundler's own Gemfile group names.
// Direct deps get their scope from directGroups (as reported live by Bundler); transitive
// deps inherit the union of scopes from their parent chain. An explicit GemGroups entry
// in the config, when present, overrides Bundler's own value for that gem.
func (rf *RubygemsFlexPack) assignScopes(depInfoMap map[string]*DependencyInfo, fwdGraph map[string][]string, rootChildren []string, directGroups map[string][]string) {
	scopesByName := make(map[string][]string, len(directGroups))
	for name, groups := range directGroups {
		scopesByName[name] = groups
	}
	for name, groups := range rf.config.GemGroups {
		scopesByName[name] = groups
	}

	// BFS from root to propagate scopes to transitive deps. A transitive dep inherits
	// the union of scopes of its ancestors.
	resolved := make(map[string][]string)
	for _, name := range rootChildren {
		scopes := scopesByName[name]
		if len(scopes) == 0 {
			scopes = []string{"default"}
		}
		rf.propagateScopes(name, scopes, fwdGraph, depInfoMap, resolved)
	}

	// Apply resolved scopes to DependencyInfo.
	for name, dep := range depInfoMap {
		if scopes, ok := resolved[name]; ok {
			dep.Scopes = scopes
		} else {
			dep.Scopes = []string{"default"}
		}
	}
}

// propagateScopes recursively assigns scopes via BFS. When a dep is reachable from
// multiple parents with different scopes, it gets all unique scopes (e.g., a gem used
// by both a dev gem and a default gem is classified as ["default", "development"]).
func (rf *RubygemsFlexPack) propagateScopes(name string, scopes []string, fwdGraph map[string][]string, depInfoMap map[string]*DependencyInfo, resolved map[string][]string) {
	existing := resolved[name]
	merged := mergeScopes(existing, scopes)
	if scopesEqual(existing, merged) {
		return // already resolved with these scopes
	}
	resolved[name] = merged

	for _, child := range fwdGraph[name] {
		if _, ok := depInfoMap[child]; ok {
			rf.propagateScopes(child, merged, fwdGraph, depInfoMap, resolved)
		}
	}
}

// mergeScopes combines two scope slices, deduplicating entries.
func mergeScopes(a, b []string) []string {
	seen := make(map[string]bool)
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		seen[s] = true
	}
	var result []string
	for s := range seen {
		result = append(result, s)
	}
	return result
}

// scopesEqual reports whether a and b hold the same scopes, ignoring order.
//
// Occurrences are counted rather than merely tested for membership. A membership check
// treats ["a", "a", "b"] and ["b", "b", "a"] as equal, because they have the same length
// and draw from the same distinct values, which would suppress a genuine scope update.
func scopesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	remaining := make(map[string]int, len(a))
	for _, s := range a {
		remaining[s]++
	}
	for _, s := range b {
		remaining[s]--
		if remaining[s] < 0 {
			return false
		}
	}
	return true
}

// collectRootChildren returns the project's direct dependencies: gems Bundler reported
// a Gemfile group for. Falls back to every gem when none were identified as direct
// (e.g. the dependency graph query failed to report any groups).
func (rf *RubygemsFlexPack) collectRootChildren(depInfoMap map[string]*DependencyInfo, directGroups map[string][]string) []string {
	var rootChildren []string
	for name := range depInfoMap {
		if _, ok := directGroups[name]; ok {
			rootChildren = append(rootChildren, name)
		}
	}
	if len(rootChildren) == 0 {
		for name := range depInfoMap {
			rootChildren = append(rootChildren, name)
		}
	}
	return rootChildren
}

// moduleID returns the build-info module ID for this project.
func (rf *RubygemsFlexPack) moduleID() string {
	if rf.projectVersion != "" {
		return fmt.Sprintf("%s:%s", rf.projectName, rf.projectVersion)
	}
	return rf.projectName
}

// ===== FlexPackManager Interface =====

// GetDependency returns a formatted string with dependency information.
func (rf *RubygemsFlexPack) GetDependency() string {
	rf.ensureParsed()
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s\n", rf.moduleID())
	result.WriteString("Dependencies:\n")
	for _, dep := range rf.dependencies {
		fmt.Fprintf(&result, "  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type)
	}
	return result.String()
}

// ParseDependencyToList returns a list of "name:version" strings for all dependencies.
func (rf *RubygemsFlexPack) ParseDependencyToList() []string {
	rf.ensureParsed()
	var depList []string
	for _, dep := range rf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum returns checksum maps for all dependencies.
func (rf *RubygemsFlexPack) CalculateChecksum() []map[string]interface{} {
	rf.ensureParsed()
	var checksums []map[string]interface{}
	for _, dep := range rf.dependencies {
		checksums = append(checksums, map[string]interface{}{
			"type":    dep.Type,
			"sha1":    dep.SHA1,
			"sha256":  dep.SHA256,
			"md5":     dep.MD5,
			"id":      dep.ID,
			"scopes":  dep.Scopes,
			"name":    dep.Name,
			"version": dep.Version,
		})
	}
	return checksums
}

// CalculateScopes returns the unique set of scopes across all dependencies.
func (rf *RubygemsFlexPack) CalculateScopes() []string {
	rf.ensureParsed()
	scopesMap := make(map[string]bool)
	for _, dep := range rf.dependencies {
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
func (rf *RubygemsFlexPack) CalculateRequestedBy() map[string][]string {
	rf.ensureParsed()
	result := make(map[string][]string)
	for depID, chains := range rf.requestedByChains {
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
func (rf *RubygemsFlexPack) GetRequestedByChains() map[string][][]string {
	rf.ensureParsed()
	return rf.requestedByChains
}

// ===== BuildInfoCollector Interface =====

// CollectBuildInfo builds a complete entities.BuildInfo for this RubyGems project.
func (rf *RubygemsFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	buildInfo := &entities.BuildInfo{
		Name:   buildName,
		Number: buildNumber,
		Agent: &entities.Agent{
			Name:    "gem",
			Version: rf.getGemVersion(),
		},
		BuildAgent: &entities.Agent{Name: "Generic", Version: "1.0"},
		Modules:    []entities.Module{},
	}

	module := entities.Module{
		Id:   rf.moduleID(),
		Type: entities.Gem,
	}

	deps, err := rf.GetProjectDependencies()
	if err != nil {
		return nil, err
	}

	for _, dep := range deps {
		module.Dependencies = append(module.Dependencies, entities.Dependency{
			Id:          dep.ID,
			Type:        dep.Type,
			Scopes:      dep.Scopes,
			Repository:  dep.Repository,
			RequestedBy: rf.requestedByChains[dep.ID],
			Checksum: entities.Checksum{
				Sha1:   dep.SHA1,
				Sha256: dep.SHA256,
				Md5:    dep.MD5,
			},
		})
	}

	buildInfo.Modules = append(buildInfo.Modules, module)
	return buildInfo, nil
}

// GetProjectDependencies returns all project dependencies with full details.
func (rf *RubygemsFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	rf.ensureParsed()
	return rf.dependencies, nil
}

// GetDirectURLDeps returns a map of dep ID ("name:version") → source for gems sourced
// from GIT/PATH rather than a registry. These are not in Artifactory, so sha1/md5
// enrichment via AQL should be skipped for them.
func (rf *RubygemsFlexPack) GetDirectURLDeps() map[string]string {
	rf.ensureParsed()
	result := make(map[string]string)
	for _, dep := range rf.dependencies {
		if dep.DirectURL != "" {
			result[dep.ID] = dep.DirectURL
		}
	}
	return result
}

// GetDependencyGraph returns the complete dependency graph (ID → child IDs).
func (rf *RubygemsFlexPack) GetDependencyGraph() (map[string][]string, error) {
	rf.ensureParsed()
	return rf.depGraph, nil
}

// getGemVersion returns the installed RubyGems version string (e.g. "3.4.10").
func (rf *RubygemsFlexPack) getGemVersion() string {
	cmd := exec.Command("gem", "--version")
	output, err := cmd.Output()
	if err != nil {
		log.Debug("Failed to get gem version: " + err.Error())
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}
