package flexpack

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// gemSourceType identifies where a gem spec originated in the lock file.
type gemSourceType string

const (
	gemSourceGEM  gemSourceType = "GEM"
	gemSourceGIT  gemSourceType = "GIT"
	gemSourcePATH gemSourceType = "PATH"

	// gemDepType is the build-info dependency/artifact type for RubyGems packages.
	gemDepType = "gem"
)

// gemSpec represents a single resolved gem in the lock file's "specs:" section.
type gemSpec struct {
	name    string
	version string
	deps    []string      // names of the gem's direct dependencies
	source  gemSourceType // GEM / GIT / PATH
	remote  string        // remote URL (GEM) or repo (GIT/PATH); not in Artifactory for GIT/PATH
}

// RubygemsLockFile is the parsed representation of a Gemfile.lock.
type RubygemsLockFile struct {
	// specs is keyed by exact gem name → resolved spec.
	specs map[string]*gemSpec
	// directDeps are the gem names declared in the DEPENDENCIES section
	// (i.e. the gems requested directly by the project's Gemfile).
	directDeps []string
	// bundlerVersion is read from the "BUNDLED WITH" section, if present.
	bundlerVersion string
}

// RubygemsFlexPack implements FlexPackManager and BuildInfoCollector for RubyGems / Bundler.
// Gemfile.lock is lock-file driven (like uv.lock), so this mirrors the UV FlexPack:
//   - dep ID:      "name:version"   (e.g. "rake:13.0.6")
//   - dep type:    "gem"
//   - requestedBy: full chain back to the root module
//   - no scopes:   Gemfile groups are not represented in the lock specs section
//
// Gemfile.lock carries no checksums, so sha1/sha256/md5 enrichment is left to the
// JFrog CLI layer (Artifactory AQL), exactly like UV.
type RubygemsFlexPack struct {
	config            GemConfig
	lockFileData      *RubygemsLockFile
	projectName       string
	projectVersion    string
	parsed            bool
	dependencies      []DependencyInfo
	depGraph          map[string][]string   // dep ID ("name:version") -> []dep IDs
	requestedByChains map[string][][]string // dep ID -> full chains back to root
}

// NewRubygemsFlexPack creates a new RubygemsFlexPack instance.
func NewRubygemsFlexPack(config GemConfig) (*RubygemsFlexPack, error) {
	rf := &RubygemsFlexPack{
		config:            config,
		dependencies:      []DependencyInfo{},
		depGraph:          make(map[string][]string),
		requestedByChains: make(map[string][][]string),
	}
	rf.resolveProjectIdentity()
	if err := rf.loadGemfileLock(); err != nil {
		log.Debug("Failed to load Gemfile.lock, dependency collection will be empty: " + err.Error())
	}
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

// gemLockPath returns the path of the Gemfile.lock to parse.
func (rf *RubygemsFlexPack) gemLockPath() string {
	if rf.config.LockFilePath != "" {
		return rf.config.LockFilePath
	}
	return filepath.Join(rf.config.WorkingDirectory, "Gemfile.lock")
}

// loadGemfileLock reads and parses the Gemfile.lock.
func (rf *RubygemsFlexPack) loadGemfileLock() error {
	lockPath := rf.gemLockPath()
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read Gemfile.lock: %w", err)
	}
	rf.lockFileData = parseGemfileLock(string(data))
	return nil
}

// parseGemfileLock parses the indentation-based Gemfile.lock format.
//
// Layout:
//
//	GEM
//	  remote: https://rubygems.org/
//	  specs:
//	    rake (13.0.6)
//	    rspec (3.12.0)
//	      rspec-core (~> 3.12.0)
//	  ...
//	PLATFORMS
//	  ruby
//	DEPENDENCIES
//	  rake
//	  rspec
//	BUNDLED WITH
//	   2.4.10
//
// GIT and PATH blocks follow the same "specs:" structure as GEM.
func parseGemfileLock(content string) *RubygemsLockFile {
	lock := &RubygemsLockFile{
		specs: make(map[string]*gemSpec),
	}

	var (
		currentSection gemSourceType
		currentRemote  string
		inSpecs        bool
		inDeps         bool
		inBundledWith  bool
		currentSpec    *gemSpec
	)

	scanner := bufio.NewScanner(strings.NewReader(content))
	// Gemfile.lock lines are short; default buffer is sufficient but raise it to be safe.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		rawLine := scanner.Text()
		if strings.TrimSpace(rawLine) == "" {
			// Blank line ends the current spec context but section headers reset state anyway.
			currentSpec = nil
			continue
		}

		indent := countLeadingSpaces(rawLine)
		line := strings.TrimRight(rawLine, " \t")
		trimmed := strings.TrimSpace(line)

		// Section headers sit at column 0.
		if indent == 0 {
			inSpecs = false
			inDeps = false
			inBundledWith = false
			currentSpec = nil
			currentRemote = ""
			switch trimmed {
			case "GEM":
				currentSection = gemSourceGEM
			case "GIT":
				currentSection = gemSourceGIT
			case "PATH":
				currentSection = gemSourcePATH
			case "DEPENDENCIES":
				currentSection = ""
				inDeps = true
			case "BUNDLED WITH":
				currentSection = ""
				inBundledWith = true
			default:
				// PLATFORMS, RUBY VERSION, CHECKSUMS, etc. — ignored.
				currentSection = ""
			}
			continue
		}

		// DEPENDENCIES entries are indented by 2 spaces: "name", "name (constraint)", "name!".
		if inDeps {
			name := gemDependencyName(trimmed)
			if name != "" {
				lock.directDeps = append(lock.directDeps, name)
			}
			continue
		}

		// BUNDLED WITH holds a single indented version line.
		if inBundledWith {
			lock.bundlerVersion = trimmed
			continue
		}

		// Inside a GEM/GIT/PATH block.
		if currentSection != "" {
			switch {
			case strings.HasPrefix(trimmed, "remote:"):
				currentRemote = strings.TrimSpace(strings.TrimPrefix(trimmed, "remote:"))
				continue
			case trimmed == "specs:":
				inSpecs = true
				continue
			case !inSpecs:
				// revision:, ref:, branch:, glob:, etc. — metadata we don't need.
				continue
			}

			// Within "specs:": indent 4 = spec, indent 6+ = that spec's dependency.
			if indent <= 4 {
				name, version := parseSpecLine(trimmed)
				if name == "" {
					continue
				}
				spec := &gemSpec{
					name:    name,
					version: version,
					source:  currentSection,
					remote:  currentRemote,
				}
				lock.specs[name] = spec
				currentSpec = spec
			} else if currentSpec != nil {
				depName := gemDependencyName(trimmed)
				if depName != "" {
					currentSpec.deps = append(currentSpec.deps, depName)
				}
			}
		}
	}

	return lock
}

// countLeadingSpaces returns the number of leading space characters.
func countLeadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r == ' ' {
			count++
			continue
		}
		break
	}
	return count
}

// parseSpecLine parses "name (version)" → name, version. The version may carry a
// platform suffix, e.g. "nokogiri (1.13.9-x86_64-linux)"; the full string is kept.
func parseSpecLine(line string) (name, version string) {
	open := strings.Index(line, " (")
	if open == -1 {
		return strings.TrimSpace(line), ""
	}
	name = strings.TrimSpace(line[:open])
	rest := line[open+2:]
	if closeIdx := strings.LastIndex(rest, ")"); closeIdx != -1 {
		version = strings.TrimSpace(rest[:closeIdx])
	}
	return name, version
}

// gemDependencyName extracts the gem name from a dependency line, stripping any
// version constraint in parentheses and the trailing "!" pin marker.
//
//	"rspec-core (~> 3.12.0)" → "rspec-core"
//	"rails (>= 6.0, < 7)"    → "rails"
//	"my_gem!"                → "my_gem"
func gemDependencyName(line string) string {
	name := line
	if open := strings.Index(name, " ("); open != -1 {
		name = name[:open]
	} else if open := strings.Index(name, "("); open != -1 {
		name = name[:open]
	}
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, "!")
	return strings.TrimSpace(name)
}

// ensureParsed builds the dependency model exactly once.
func (rf *RubygemsFlexPack) ensureParsed() {
	if rf.parsed {
		return
	}
	rf.parseDependencies()
	rf.parsed = true
}

// parseDependencies populates rf.dependencies, rf.depGraph and rf.requestedByChains.
func (rf *RubygemsFlexPack) parseDependencies() {
	if rf.lockFileData == nil {
		return
	}

	moduleID := rf.moduleID()

	// Build dep info map keyed by exact gem name. When InstalledPackages is provided,
	// only the gems actually installed are included (handles bundler group filtering).
	depInfoMap := make(map[string]*DependencyInfo)
	for name, spec := range rf.lockFileData.specs {
		if rf.config.InstalledPackages != nil {
			if _, ok := rf.config.InstalledPackages[name]; !ok {
				continue
			}
		}
		// GIT/PATH gems are not stored in Artifactory; flag them so the CLI layer
		// can skip checksum enrichment for them.
		directURL := ""
		if spec.source == gemSourceGIT || spec.source == gemSourcePATH {
			directURL = spec.remote
		}
		depInfoMap[name] = &DependencyInfo{
			ID:        fmt.Sprintf("%s:%s", spec.name, spec.version),
			Name:      spec.name,
			Version:   spec.version,
			Type:      gemDepType,
			DirectURL: directURL,
		}
	}

	// Forward graph (name → child names) limited to gems present in depInfoMap.
	fwdGraph := make(map[string][]string, len(depInfoMap))
	for name, info := range depInfoMap {
		spec := rf.lockFileData.specs[name]
		if spec == nil {
			continue
		}
		var children []string
		var childIDs []string
		for _, child := range spec.deps {
			if _, ok := depInfoMap[child]; ok {
				children = append(children, child)
				childIDs = append(childIDs, depInfoMap[child].ID)
			}
		}
		fwdGraph[name] = children
		rf.depGraph[info.ID] = childIDs
	}

	rootChildren := rf.collectRootChildren(depInfoMap)

	// Reuse the shared chain builder (defined in uv_flexpack.go) — it operates purely
	// on depInfoMap + fwdGraph keys, so exact gem names work the same as UV's normalised names.
	buildUvRequestedBy(moduleID, []string{}, rootChildren, depInfoMap, fwdGraph, rf.requestedByChains, entities.RequestedByMaxLength)

	for _, dep := range depInfoMap {
		rf.dependencies = append(rf.dependencies, *dep)
	}
}

// collectRootChildren returns the project's direct dependencies that are present in depInfoMap.
// Falls back to every gem when the DEPENDENCIES section is empty/unparsed.
func (rf *RubygemsFlexPack) collectRootChildren(depInfoMap map[string]*DependencyInfo) []string {
	var rootChildren []string
	seen := make(map[string]bool)
	for _, name := range rf.lockFileData.directDeps {
		if _, ok := depInfoMap[name]; ok && !seen[name] {
			rootChildren = append(rootChildren, name)
			seen[name] = true
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
// RubyGems lock specs carry no group/scope information, so this is typically empty.
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
