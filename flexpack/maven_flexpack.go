package flexpack

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// MavenFlexPack implements the FlexPackManager interface for Maven package manager
type MavenFlexPack struct {
	config          MavenConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	groupId         string
	artifactId      string
	pomData         *MavenPOM
	requestedByMap  map[string][]string
	// moduleLocations maps each collected module's id ("groupId:artifactId:version") to where Maven
	// built it, captured from the dependency-tree output. It is the authoritative module list (it
	// reflects what Maven actually ran, including profile-activated modules) and lets callers attach
	// deployed artifacts without re-parsing the pom tree.
	moduleLocations map[string]ModuleLocation
	// checksumCache memoizes file checksums by artifact path, so a dependency shared across several
	// reactor modules is read and hashed only once instead of once per module.
	checksumCache map[string]artifactChecksum
	// localRepo caches the resolved Maven local-repository path (see localRepositoryPath).
	localRepo         string
	localRepoResolved bool
}

// artifactChecksum holds the checksums of a single file, cached across modules by artifact path.
type artifactChecksum struct {
	sha1, sha256, md5 string
}

// checksumCacheKey returns a normalized path suitable for use as a checksumCache map key.
// On case-insensitive filesystems (macOS default, Windows) the same file can be reached
// via different-cased paths, so the key is lowercased on those platforms.
func checksumCacheKey(path string) string {
	key := filepath.Clean(path)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(key)
	}
	return key
}

// ModuleLocation records where a reactor module was built and its packaging, derived from the
// module's maven-deps.json (the JSON root node carries the resolved coordinates, type, and its file
// path is the module's base directory).
type ModuleLocation struct {
	Dir       string
	Packaging string
}

// MavenConfig represents configuration for Maven FlexPack
type MavenConfig struct {
	WorkingDirectory        string
	IncludeTestDependencies bool
	MavenExecutable         string
	SkipTests               bool
	// ExtraArgs are appended to the internal `mvn dependency:tree` invocation so dependency
	// resolution matches the profiles/settings/properties the user built with (e.g. -Pprod,
	// -s settings.xml, -Drevision=1.2.3).
	ExtraArgs []string
}

// mavenDepsFileName is the file each reactor module writes its JSON dependency tree to when
// `mvn dependency:tree -DoutputFile=<name>` runs. Maven runs the goal once per module and writes
// this file into every module's own base directory, so a multi-module build produces one file per
// module (root aggregator included).
const mavenDepsFileName = "maven-deps.json"

// mavenDependencyPluginTreeGoal is the fully-qualified plugin coordinate used to invoke the
// dependency:tree goal. The version is pinned because `-DoutputType=json` was only added in
// maven-dependency-plugin 3.7.0; older versions silently write plain-text output which the JSON
// parser then rejects. 3.8.1 (current latest) is used to also pick up bug fixes.
const mavenDependencyPluginTreeGoal = "org.apache.maven.plugins:maven-dependency-plugin:3.8.1:tree"

// MavenPOM represents the structure of pom.xml file
type MavenPOM struct {
	XMLName      xml.Name `xml:"project"`
	GroupId      string   `xml:"groupId"`
	ArtifactId   string   `xml:"artifactId"`
	Version      string   `xml:"version"`
	Packaging    string   `xml:"packaging"`
	Name         string   `xml:"name"`
	Description  string   `xml:"description"`
	URL          string   `xml:"url"`
	Dependencies struct {
		Dependency []MavenDependency `xml:"dependency"`
	} `xml:"dependencies"`
}

// MavenDependency represents a dependency in pom.xml
type MavenDependency struct {
	GroupId    string `xml:"groupId"`
	ArtifactId string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Type       string `xml:"type"`
	Optional   bool   `xml:"optional"`
}

// MavenDependencyTreeEntry represents an entry from mvn dependency:tree output
type MavenDependencyTreeEntry struct {
	GroupId    string
	ArtifactId string
	Version    string
	Scope      string
	Type       string
	Level      int
	Parent     string
}

// NewMavenFlexPack creates a new Maven FlexPack instance
func NewMavenFlexPack(config MavenConfig) (*MavenFlexPack, error) {
	mf := &MavenFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
		moduleLocations: make(map[string]ModuleLocation),
		checksumCache:   make(map[string]artifactChecksum),
	}

	if mf.config.MavenExecutable == "" {
		mf.config.MavenExecutable = mf.getMavenExecutablePath()
	}
	if err := mf.loadPOM(); err != nil {
		return nil, fmt.Errorf("failed to load pom.xml: %w", err)
	}

	return mf, nil
}

// GetDependency fetches and parses dependencies, then returns dependency information
func (mf *MavenFlexPack) GetDependency() string {
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}
	var result strings.Builder
	fmt.Fprintf(&result, "Project: %s:%s:%s\n", mf.groupId, mf.artifactId, mf.projectVersion)
	result.WriteString("Dependencies:\n")
	for _, dep := range mf.dependencies {
		fmt.Fprintf(&result, "  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type)
	}
	return result.String()
}

// ParseDependencyToList converts parsed dependencies to a list format
func (mf *MavenFlexPack) ParseDependencyToList() []string {
	var depList []string
	for _, dep := range mf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum calculates checksums for dependencies
func (mf *MavenFlexPack) CalculateChecksum() []map[string]interface{} {
	var checksums []map[string]interface{}
	for _, dep := range mf.dependencies {
		checksumMap := mf.calculateChecksumWithFallback(dep)
		if checksumMap != nil {
			checksums = append(checksums, checksumMap)
		}
	}
	// Always return a non-nil slice, even if empty
	if checksums == nil {
		checksums = []map[string]interface{}{}
	}
	return checksums
}

// CalculateScopes calculates and returns the scopes for dependencies
// For Maven, this returns the official Maven dependency scopes in consistent order: compile, runtime, test, provided, system, import
func (mf *MavenFlexPack) CalculateScopes() []string {
	scopesMap := make(map[string]bool)

	// Collect all unique scopes from dependencies
	for _, dep := range mf.dependencies {
		for _, scope := range dep.Scopes {
			scopesMap[scope] = true
		}
	}

	// Return scopes in Maven standard order
	var orderedScopes []string
	mavenScopeOrder := []string{"compile", "runtime", "test", "provided", "system", "import"}

	for _, scope := range mavenScopeOrder {
		if scopesMap[scope] {
			orderedScopes = append(orderedScopes, scope)
		}
	}

	return orderedScopes
}

// CalculateRequestedBy determines which dependencies requested a particular package
func (mf *MavenFlexPack) CalculateRequestedBy() map[string][]string {
	if len(mf.requestedByMap) == 0 {
		mf.buildRequestedByMap()
	}
	return mf.requestedByMap
}

// loadPOM loads and parses the pom.xml file
func (mf *MavenFlexPack) loadPOM() error {
	pomPath := filepath.Join(mf.config.WorkingDirectory, "pom.xml")
	data, err := os.ReadFile(pomPath)
	if err != nil {
		return fmt.Errorf("failed to read pom.xml: %w", err)
	}

	mf.pomData = &MavenPOM{}
	if err := xml.Unmarshal(data, mf.pomData); err != nil {
		return fmt.Errorf("failed to parse pom.xml: %w", err)
	}

	mf.groupId = mf.pomData.GroupId
	mf.artifactId = mf.pomData.ArtifactId
	mf.projectVersion = mf.pomData.Version
	mf.projectName = fmt.Sprintf("%s:%s", mf.groupId, mf.artifactId)

	return nil
}

// parseDependencies parses dependencies using hybrid strategy
func (mf *MavenFlexPack) parseDependencies() {
	if err := mf.parseWithMavenDependencyTree(); err == nil {
		return
	} else {
		log.Warn("Maven dependency:tree parsing failed, falling back to POM parsing: " + err.Error())
	}
	mf.parseFromPOM()
}

// parseWithMavenDependencyTree uses mvn dependency:tree to get complete dependency information
func (mf *MavenFlexPack) parseWithMavenDependencyTree() error {
	// Generate dependency tree as JSON
	depsJsonPath := filepath.Join(mf.config.WorkingDirectory, "maven-deps.json")
	// Clean up any existing dependency JSON file
	defer func() {
		if err := os.Remove(depsJsonPath); err != nil && !os.IsNotExist(err) {
			log.Debug("Failed to remove temporary maven-deps.json file: " + err.Error())
		}
	}()

	args := []string{mavenDependencyPluginTreeGoal, "-DoutputType=json", "-DoutputFile=maven-deps.json"}
	if mf.config.SkipTests {
		args = append(args, "-DskipTests")
	}

	cmd := exec.Command(mf.config.MavenExecutable, args...)
	cmd.Dir = mf.config.WorkingDirectory

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mvn dependency:tree failed: %w\nOutput: %s", err, string(output))
	}

	// Read and parse the generated JSON file
	content, err := os.ReadFile(depsJsonPath)
	if err != nil {
		return fmt.Errorf("failed to read dependency JSON: %w", err)
	}
	return mf.parseDependencyTreeJSON(content)
}

// MavenDependencyJSON represents a dependency in Maven's JSON dependency tree
type MavenDependencyJSON struct {
	GroupID    string                `json:"groupId"`
	ArtifactID string                `json:"artifactId"`
	Version    string                `json:"version"`
	Type       string                `json:"type"`
	Scope      string                `json:"scope"`
	Classifier string                `json:"classifier"`
	Optional   string                `json:"optional"`
	Children   []MavenDependencyJSON `json:"children,omitempty"`
}

// parseDependencyTreeJSON parses Maven's JSON dependency tree output
func (mf *MavenFlexPack) parseDependencyTreeJSON(content []byte) error {
	var rootDep MavenDependencyJSON
	if err := json.Unmarshal(content, &rootDep); err != nil {
		return fmt.Errorf("failed to parse dependency JSON: %w", err)
	}

	// Process all dependencies recursively
	seenDependencies := make(map[string]bool)
	mf.processDependencyNode(rootDep, "", seenDependencies)
	log.Debug(fmt.Sprintf("Collected %d dependencies", len(mf.dependencies)))
	return nil
}

// processDependencyNode recursively processes a dependency node and its children
func (mf *MavenFlexPack) processDependencyNode(dep MavenDependencyJSON, parent string, seen map[string]bool) {
	// Skip empty or invalid dependencies
	if dep.GroupID == "" || dep.ArtifactID == "" || dep.Version == "" {
		return
	}

	dependencyId := fmt.Sprintf("%s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)

	// Skip the root project itself - it's not a dependency
	// Compare groupId and artifactId only (version should match but let's be safe)
	if dep.GroupID == mf.groupId && dep.ArtifactID == mf.artifactId {
		for _, child := range dep.Children {
			mf.processDependencyNode(child, dependencyId, seen)
		}
		return
	}

	// Skip duplicates
	if seen[dependencyId] {
		return
	}
	seen[dependencyId] = true
	// Check if this is a test dependency (for filtering purposes)
	isTestDependency := strings.ToLower(dep.Scope) == "test"
	if !mf.config.IncludeTestDependencies && isTestDependency {
		return
	}
	// Create dependency info
	depInfo := DependencyInfo{
		ID:      dependencyId,
		Name:    fmt.Sprintf("%s:%s", dep.GroupID, dep.ArtifactID),
		Version: dep.Version,
		Type:    mf.mapPackagingToType(dep.Type),
		Scopes:  mf.mapMavenScopeToScopes(dep.Scope),
	}
	mf.dependencies = append(mf.dependencies, depInfo)
	// Build dependency graph
	if parent != "" {
		if mf.dependencyGraph[parent] == nil {
			mf.dependencyGraph[parent] = []string{}
		}
		mf.dependencyGraph[parent] = append(mf.dependencyGraph[parent], dependencyId)
	}
	// Process children recursively
	for _, child := range dep.Children {
		mf.processDependencyNode(child, dependencyId, seen)
	}
}

// parseFromPOM parses dependencies directly from pom.xml
func (mf *MavenFlexPack) parseFromPOM() {
	for _, dep := range mf.pomData.Dependencies.Dependency {
		dependencyId := fmt.Sprintf("%s:%s:%s", dep.GroupId, dep.ArtifactId, dep.Version)
		depInfo := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", dep.GroupId, dep.ArtifactId),
			Version: dep.Version,
			Type:    mf.mapPackagingToType(dep.Type),
			Scopes:  mf.mapMavenScopeToScopes(dep.Scope), // Use actual Maven scope from POM
		}
		// Check if this is a test dependency (for filtering purposes)
		isTestDependency := strings.ToLower(dep.Scope) == "test"
		if !mf.config.IncludeTestDependencies && isTestDependency {
			continue
		}
		mf.dependencies = append(mf.dependencies, depInfo)
	}
}

// mapMavenScopeToScopes maps Maven dependency scope to build-info scopes
func (mf *MavenFlexPack) mapMavenScopeToScopes(scope string) []string {
	// Handle empty scope (Maven default is compile)
	if scope == "" {
		scope = "compile"
	}
	normalizedScope := strings.ToLower(scope)
	// Validate against known Maven scopes
	validScopes := []string{"compile", "runtime", "test", "provided", "system", "import"}
	for _, validScope := range validScopes {
		if normalizedScope == validScope {
			return []string{normalizedScope}
		}
	}
	// Unknown scope, default to compile
	return []string{"compile"}
}

// mapPackagingToType maps Maven packaging types to artifact types
func (mf *MavenFlexPack) mapPackagingToType(packaging string) string {
	if packaging == "" {
		return "jar" // Maven default
	}

	switch strings.ToLower(packaging) {
	case "jar":
		return "jar"
	case "war":
		return "war"
	case "ear":
		return "ear"
	case "pom":
		return "pom"
	case "maven-plugin":
		return "maven-plugin"
	default:
		return packaging
	}
}

// calculateChecksumWithFallback calculates checksums with multiple fallback strategies
func (mf *MavenFlexPack) calculateChecksumWithFallback(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  mf.validateAndNormalizeScopes(dep.Scopes),
	}

	// Strategy 1: Try to find artifact in Maven local repository. Checksums are cached by path so a
	// dependency shared across reactor modules is hashed only once.
	if artifactPath := mf.findMavenArtifact(dep); artifactPath != "" {
		cacheKey := checksumCacheKey(artifactPath)
		cached, ok := mf.checksumCache[cacheKey]
		if !ok {
			if sha1, sha256, md5, err := mf.calculateFileChecksum(artifactPath); err == nil {
				cached = artifactChecksum{sha1: sha1, sha256: sha256, md5: md5}
				mf.checksumCache[cacheKey] = cached
				ok = true
			} else {
				log.Warn(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
			}
		}
		if ok {
			checksumMap["sha1"] = cached.sha1
			checksumMap["sha256"] = cached.sha256
			checksumMap["md5"] = cached.md5
			checksumMap["path"] = artifactPath
			return checksumMap
		}
	}

	// Strategy 2: Future enhancement - could call Artifactory API to get real checksums
	// Example: GET /api/storage/{repo}/{path}?checksums=sha1,sha256,md5
	// This would provide authentic checksums from the repository

	// Strategy 3: Handle missing checksums gracefully
	// For test dependencies during compile phase, this is expected behavior
	isTestDependency := false
	for _, scope := range dep.Scopes {
		if strings.ToLower(scope) == "test" {
			isTestDependency = true
			break
		}
	}

	if isTestDependency {
		log.Debug(fmt.Sprintf("Skipping checksum calculation for test dependency: %s:%s (not downloaded during compile)", dep.Name, dep.Version))
	} else {
		log.Warn(fmt.Sprintf("Failed to calculate checksums for dependency: %s:%s", dep.Name, dep.Version))
	}
	return nil
}

// localRepositoryPath returns the Maven local repository to search for downloaded artifacts. It
// honors a `-Dmaven.repo.local=<path>` override forwarded from the user's invocation (so checksum
// lookup works when the build used a custom local repo), falling back to the default ~/.m2/repository.
func (mf *MavenFlexPack) localRepositoryPath() string {
	// Resolved once and reused: it is queried for every dependency of every module, but neither
	// ExtraArgs nor the home directory changes during a collection.
	if mf.localRepoResolved {
		return mf.localRepo
	}
	mf.localRepo = mf.resolveLocalRepositoryPath()
	mf.localRepoResolved = true
	return mf.localRepo
}

func (mf *MavenFlexPack) resolveLocalRepositoryPath() string {
	const flag = "-Dmaven.repo.local="
	// Later occurrences win, matching Maven's last-value-wins behavior for repeated properties.
	custom := ""
	for _, arg := range mf.config.ExtraArgs {
		if v, ok := strings.CutPrefix(arg, flag); ok && v != "" {
			custom = v
		}
	}
	if custom != "" {
		return custom
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Debug("Failed to get user home directory: " + err.Error())
		return ""
	}
	return filepath.Join(homeDir, ".m2", "repository")
}

// findMavenArtifact locates a Maven artifact in the local repository
func (mf *MavenFlexPack) findMavenArtifact(dep DependencyInfo) string {
	// Parse dependency name to get groupId and artifactId
	parts := strings.Split(dep.Name, ":")
	if len(parts) != 2 {
		return ""
	}

	groupId := parts[0]
	artifactId := parts[1]

	localRepo := mf.localRepositoryPath()
	if localRepo == "" {
		return ""
	}

	// Convert groupId to path (e.g., com.example -> com/example)
	groupPath := strings.ReplaceAll(groupId, ".", string(filepath.Separator))

	// Build artifact path
	artifactPath := filepath.Join(localRepo, groupPath, artifactId, dep.Version,
		fmt.Sprintf("%s-%s.%s", artifactId, dep.Version, dep.Type))

	// Check if artifact exists
	if _, err := os.Stat(artifactPath); err == nil {
		return artifactPath
	}

	return ""
}

// calculateFileChecksum calculates checksums for a file
func (mf *MavenFlexPack) calculateFileChecksum(filePath string) (string, string, string, error) {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return "", "", "", err
	}

	// Verify fileDetails and checksum are not nil before accessing
	if fileDetails == nil {
		return "", "", "", fmt.Errorf("fileDetails is nil for file: %s", filePath)
	}

	return fileDetails.Checksum.Sha1,
		fileDetails.Checksum.Sha256,
		fileDetails.Checksum.Md5,
		nil
}

// validateAndNormalizeScopes ensures scopes are valid and normalized
func (mf *MavenFlexPack) validateAndNormalizeScopes(scopes []string) []string {
	validScopes := map[string]bool{
		"compile":  true,
		"runtime":  true,
		"test":     true,
		"provided": true,
		"system":   true,
		"import":   true,
	}

	var normalized []string
	for _, scope := range scopes {
		if validScopes[scope] {
			normalized = append(normalized, scope)
		}
	}

	if len(normalized) == 0 {
		normalized = []string{"compile"} // Default Maven scope
	}

	return normalized
}

// buildRequestedByMap builds the requested-by relationship map by inverting the dependency graph.
func (mf *MavenFlexPack) buildRequestedByMap() {
	mf.requestedByMap = invertDependencyGraph(mf.dependencyGraph)
}

// CollectBuildInfo collects complete build information for a Maven project.
// For multi-module (reactor) projects it produces one entities.Module per reactor module, each with
// its own dependencies, instead of collapsing everything into a single module.
func (mf *MavenFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	modules, err := mf.collectModules()
	if err != nil {
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
			Name:    "Maven",
			Version: mf.getMavenVersion(),
		},
		Modules: modules,
		// Mark how this build info was produced. FlexPack is the native collector, so the value is always
		// "native"; the legacy build-info-extractor path stamps "legacy". Consumers can branch on it and
		// it makes the two paths distinguishable in the published JSON.
		Properties: entities.Env{entities.MavenBuildModeProperty: entities.MavenBuildModeNative},
	}

	return buildInfo, nil
}

// collectModules produces one entities.Module per reactor module. It runs `mvn dependency:tree` at
// the project root (Maven executes it for every module, each writing its own maven-deps.json), then
// builds a module from each generated file. If the tree cannot be generated it falls back to parsing
// the root pom.xml directly, yielding a single module (pre-reactor behavior).
func (mf *MavenFlexPack) collectModules() ([]entities.Module, error) {
	treeFiles, err := mf.generateReactorDependencyTrees()
	if err != nil {
		log.Warn("Maven dependency:tree failed, falling back to single-module POM parsing: " + err.Error())
		return mf.collectSingleModuleFromPOM(), nil
	}
	// Clean up generated files once we are done reading them.
	defer func() {
		for _, tf := range treeFiles {
			if removeErr := os.Remove(tf); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Debug("Failed to remove temporary dependency tree file " + tf + ": " + removeErr.Error())
			}
		}
	}()

	var modules []entities.Module
	for _, tf := range treeFiles {
		module, buildErr := mf.buildModuleFromTreeFile(tf)
		if buildErr != nil {
			log.Warn(fmt.Sprintf("Skipping dependency tree %s: %s", tf, buildErr))
			continue
		}
		modules = append(modules, module)
	}

	if len(modules) == 0 {
		log.Warn("No modules resolved from Maven dependency trees, falling back to single-module POM parsing")
		return mf.collectSingleModuleFromPOM(), nil
	}
	log.Debug(fmt.Sprintf("Collected %d Maven module(s) for build info", len(modules)))
	return modules, nil
}

// generateReactorDependencyTrees runs `mvn dependency:tree` at the project root and returns the path
// of every maven-deps.json produced (one per reactor module). Maven overwrites the file for every
// module that participates, and collectModules removes them all when done, so no separate stale-file
// sweep is needed. The plugin version is pinned so `-DoutputType=json` is honored — older 2.x
// versions silently write plain-text output which JSON parsing then rejects.
func (mf *MavenFlexPack) generateReactorDependencyTrees() ([]string, error) {
	args := []string{mavenDependencyPluginTreeGoal, "-DoutputType=json", "-DoutputFile=" + mavenDepsFileName}
	if mf.config.SkipTests {
		args = append(args, "-DskipTests")
	}
	// Forward the user's resolution flags (profiles/settings/properties) so the dependency tree is
	// resolved under the same conditions the project was actually built with.
	args = append(args, mf.config.ExtraArgs...)

	cmd := exec.Command(mf.config.MavenExecutable, args...)
	cmd.Dir = mf.config.WorkingDirectory
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mvn dependency:tree failed: %w\nOutput: %s", err, string(output))
	}

	return mf.findDependencyTreeFiles()
}

// findDependencyTreeFiles walks the working directory and returns every maven-deps.json file, one per
// reactor module. WalkDir yields lexical order, keeping module ordering deterministic across runs.
func (mf *MavenFlexPack) findDependencyTreeFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(mf.config.WorkingDirectory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// maven-deps.json only ever lives in a module base directory. Skip Maven's build output
			// ("target", full of .class files after install/deploy) and dot-directories (.git, ...) so
			// the walk does not stat thousands of irrelevant files on a large reactor.
			if path != mf.config.WorkingDirectory && (d.Name() == "target" || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == mavenDepsFileName {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// buildModuleFromTreeFile parses a single module's maven-deps.json into an entities.Module. The JSON
// root node carries the module's own resolved coordinates; its children are the module dependencies.
func (mf *MavenFlexPack) buildModuleFromTreeFile(path string) (entities.Module, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return entities.Module{}, fmt.Errorf("failed to read dependency tree: %w", err)
	}

	var root MavenDependencyJSON
	if err := json.Unmarshal(content, &root); err != nil {
		return entities.Module{}, fmt.Errorf("failed to parse dependency tree JSON: %w", err)
	}
	if root.GroupID == "" || root.ArtifactID == "" || root.Version == "" {
		return entities.Module{}, fmt.Errorf("dependency tree is missing module coordinates")
	}

	moduleId := fmt.Sprintf("%s:%s:%s", root.GroupID, root.ArtifactID, root.Version)
	// The maven-deps.json lives in the module's base directory, and the JSON root type is the module's
	// packaging. Record both so callers can attach artifacts without re-discovering modules.
	mf.moduleLocations[moduleId] = ModuleLocation{Dir: filepath.Dir(path), Packaging: root.Type}
	depInfos, graph := mf.collectModuleDependencies(moduleId, root)
	requestedBy := invertDependencyGraph(graph)

	return entities.Module{
		Id:           moduleId,
		Type:         entities.Maven,
		Dependencies: mf.toDependencyEntities(depInfos, requestedBy),
	}, nil
}

// GetModuleLocations returns the id -> location map captured during the last CollectBuildInfo call.
// The map is authoritative: it contains exactly the modules Maven built (profile-activated modules
// included) and is empty when collection fell back to single-module POM parsing.
func (mf *MavenFlexPack) GetModuleLocations() map[string]ModuleLocation {
	return mf.moduleLocations
}

// collectModuleDependencies flattens a single module's dependency tree. The tree root is the module
// itself (identified by moduleId), so collection starts from its direct children. Duplicates within
// the module are collapsed while still recording requested-by (parent -> child) edges.
func (mf *MavenFlexPack) collectModuleDependencies(moduleId string, root MavenDependencyJSON) ([]DependencyInfo, map[string][]string) {
	var deps []DependencyInfo
	graph := make(map[string][]string)
	seen := make(map[string]bool)
	for _, child := range root.Children {
		mf.collectDependencyNode(child, moduleId, seen, &deps, graph)
	}
	return deps, graph
}

// collectDependencyNode recursively walks a dependency subtree, appending each distinct dependency
// to deps and recording the parent -> child edge in graph.
func (mf *MavenFlexPack) collectDependencyNode(dep MavenDependencyJSON, parent string, seen map[string]bool, deps *[]DependencyInfo, graph map[string][]string) {
	if dep.GroupID == "" || dep.ArtifactID == "" || dep.Version == "" {
		return
	}

	dependencyId := fmt.Sprintf("%s:%s:%s", dep.GroupID, dep.ArtifactID, dep.Version)

	// A test-scoped dependency that is excluded contributes neither a node nor an edge. It is filtered
	// before the seen-guard so a later non-test occurrence of the same coordinate is still collected.
	isTestDependency := strings.ToLower(dep.Scope) == "test"
	if !mf.config.IncludeTestDependencies && isTestDependency {
		return
	}

	// Record the parent -> child edge for EVERY occurrence, even when the dependency was already seen
	// via another parent. This preserves all requesters of a diamond dependency (a package pulled in by
	// more than one parent), so requested-by can report every path back to the module root rather than
	// only the first one discovered.
	if parent != "" {
		graph[parent] = append(graph[parent], dependencyId)
	}

	// The node itself (and its subtree) is collected only once; subsequent occurrences contribute their
	// edge above but are not re-added or re-walked.
	if seen[dependencyId] {
		return
	}
	seen[dependencyId] = true

	*deps = append(*deps, DependencyInfo{
		ID:      dependencyId,
		Name:    fmt.Sprintf("%s:%s", dep.GroupID, dep.ArtifactID),
		Version: dep.Version,
		Type:    mf.mapPackagingToType(dep.Type),
		Scopes:  mf.mapMavenScopeToScopes(dep.Scope),
	})

	for _, child := range dep.Children {
		mf.collectDependencyNode(child, dependencyId, seen, deps, graph)
	}
}

// collectSingleModuleFromPOM is the fallback used when a dependency tree cannot be generated. It
// parses the root pom.xml directly and returns a single module (pre-reactor behavior).
func (mf *MavenFlexPack) collectSingleModuleFromPOM() []entities.Module {
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}
	return []entities.Module{{
		Id:           fmt.Sprintf("%s:%s:%s", mf.groupId, mf.artifactId, mf.projectVersion),
		Type:         entities.Maven,
		Dependencies: mf.toDependencyEntities(mf.dependencies, mf.CalculateRequestedBy()),
	}}
}

// toDependencyEntities converts internal DependencyInfo values into entities.Dependency, attaching
// checksums (best effort) and requested-by relationships. Shared by the reactor and fallback paths.
func (mf *MavenFlexPack) toDependencyEntities(depInfos []DependencyInfo, requestedBy map[string][]string) []entities.Dependency {
	var dependencies []entities.Dependency
	for _, dep := range depInfos {
		checksumMap := mf.calculateChecksumWithFallback(dep)
		checksum := entities.Checksum{}
		if checksumMap != nil {
			if sha1, ok := checksumMap["sha1"].(string); ok {
				checksum.Sha1 = sha1
			}
			if sha256, ok := checksumMap["sha256"].(string); ok {
				checksum.Sha256 = sha256
			}
			if md5, ok := checksumMap["md5"].(string); ok {
				checksum.Md5 = md5
			}
		}

		entity := entities.Dependency{
			Id:       dep.ID,
			Type:     dep.Type,
			Scopes:   dep.Scopes,
			Checksum: checksum,
		}
		// Full ancestor paths (dep's direct requester -> ... -> module root), one per route, matching the
		// legacy extractor. requestedBy is the child -> direct-parents map; buildRequestedByPaths expands
		// each direct parent into the complete chain up to the root.
		if paths := buildRequestedByPaths(dep.ID, requestedBy); len(paths) > 0 {
			entity.RequestedBy = paths
		}
		dependencies = append(dependencies, entity)
	}
	return dependencies
}

// invertDependencyGraph turns a parent -> children graph into a child -> parents (requested-by) map.
// A child's parents are de-duplicated so a repeated parent -> child edge contributes a single parent.
type edgeKey struct{ parent, child string }

func invertDependencyGraph(graph map[string][]string) map[string][]string {
	requestedBy := make(map[string][]string)
	seen := make(map[edgeKey]bool)
	for parent, children := range graph {
		for _, child := range children {
			edge := edgeKey{parent, child}
			if seen[edge] {
				continue
			}
			seen[edge] = true
			requestedBy[child] = append(requestedBy[child], parent)
		}
	}
	return requestedBy
}

// buildRequestedByPaths returns every ancestor path for depID, walking the child -> parents map from
// depID's direct parents up to a root (a node that is itself requested by nothing, i.e. the module).
// Each path lists ancestors nearest-first and ends at the module root, matching the build-info
// convention and the legacy Maven build-info extractor. A diamond dependency (reached through more
// than one parent) yields one path per distinct route. A cycle, which a resolved Maven tree should
// never contain, is broken defensively by terminating the offending path at the repeated node.
func buildRequestedByPaths(depID string, parents map[string][]string) [][]string {
	const maxPaths = 15
	var paths [][]string
	var walk func(node string, acc []string, onPath map[string]bool)
	walk = func(node string, acc []string, onPath map[string]bool) {
		if len(paths) >= maxPaths {
			return
		}
		directParents := parents[node]
		if len(directParents) == 0 {
			// Reached a root: the accumulated ancestors form one complete path.
			if len(acc) > 0 {
				paths = append(paths, append([]string(nil), acc...))
			}
			return
		}
		for _, parent := range directParents {
			if len(paths) >= maxPaths {
				return
			}
			if onPath[parent] {
				// Cycle guard: close the path at the repeated node instead of recursing forever.
				paths = append(paths, append(append([]string(nil), acc...), parent))
				continue
			}
			onPath[parent] = true
			walk(parent, append(acc, parent), onPath)
			delete(onPath, parent)
		}
	}
	walk(depID, nil, map[string]bool{depID: true})
	if len(paths) == maxPaths {
		log.Debug(fmt.Sprintf("dependency %s RequestedBy paths capped at %d", depID, maxPaths))
	}
	return paths
}

// RunMavenInstallWithBuildInfo runs mvn install and collects build information
// Parameters:
//   - workingDir: Maven project directory
//   - buildName, buildNumber: Build info identifiers
//   - includeTestDeps: Whether to include test dependencies in build info (does NOT affect test execution)
//   - extraArgs: Additional Maven arguments (use "-DskipTests" here to skip test execution)
func RunMavenInstallWithBuildInfo(workingDir string, buildName, buildNumber string, includeTestDeps bool, extraArgs []string) error {
	config := MavenConfig{
		WorkingDirectory:        workingDir,
		IncludeTestDependencies: includeTestDeps,
	}
	mavenFlex, err := NewMavenFlexPack(config)
	if err != nil {
		return fmt.Errorf("failed to create Maven instance: %w", err)
	}
	args := append([]string{"install"}, extraArgs...)
	// Note: Test execution control should be managed by the user via extraArgs
	// The includeTestDeps parameter only affects build info dependency collection

	cmd := exec.Command(config.MavenExecutable, args...)
	cmd.Dir = workingDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mvn install failed: %w\nOutput: %s", err, string(output))
	}

	log.Info("Maven install completed successfully")

	if buildName != "" && buildNumber != "" {
		buildInfo, err := mavenFlex.CollectBuildInfo(buildName, buildNumber)
		if err != nil {
			return fmt.Errorf("failed to collect build info: %w", err)
		}

		err = saveMavenBuildInfoForJfrogCli(buildInfo)
		if err != nil {
			log.Warn("Failed to save build info for jfrog-cli compatibility: " + err.Error())
		} else {
			log.Debug("Build info saved for jfrog-cli compatibility")
		}
	}

	return nil
}

// GetProjectDependencies returns all project dependencies with full details
func (mf *MavenFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {

	// Calculate RequestedBy relationships
	requestedBy := mf.CalculateRequestedBy()

	// Add RequestedBy information to dependencies
	for i, dep := range mf.dependencies {
		if parents, exists := requestedBy[dep.ID]; exists {
			mf.dependencies[i].RequestedBy = parents
		}
	}

	return mf.dependencies, nil
}

// GetDependencyGraph returns the complete dependency graph
func (mf *MavenFlexPack) GetDependencyGraph() (map[string][]string, error) {
	return mf.dependencyGraph, nil
}

// getMavenExecutablePath gets the Maven executable path with proper detection
func (mf *MavenFlexPack) getMavenExecutablePath() string {
	// Check for Maven wrapper first (following existing pattern from build/maven.go)
	wrapperPath := filepath.Join(mf.config.WorkingDirectory, "mvnw")
	if _, err := os.Stat(wrapperPath); err == nil {
		return "./mvnw"
	}
	wrapperCmdPath := filepath.Join(mf.config.WorkingDirectory, "mvnw.cmd")
	if _, err := os.Stat(wrapperCmdPath); err == nil {
		return "mvnw.cmd"
	}
	// Default to system Maven
	return "mvn"
}

// getMavenVersion executes 'mvn --version' and extracts the Maven version number.
// It parses the first line of output which typically looks like:
// "Apache Maven 3.9.4 (dfbb324ad4a7c8fb0bf182e6d91b0ae20e3d2dd9)"
// Returns "unknown" if the command fails or version cannot be parsed.
// This version is used in build-info metadata to track the build tool version.
func (mf *MavenFlexPack) getMavenVersion() string {
	cmd := exec.Command(mf.config.MavenExecutable, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	version := strings.TrimSpace(string(output))
	lines := strings.Split(version, "\n")
	if len(lines) > 0 {
		firstLine := lines[0]
		if parts := strings.Fields(firstLine); len(parts) >= 3 {
			return parts[2]
		}
	}
	return "unknown"
}

// saveMavenBuildInfoForJfrogCli saves build info in a format compatible with jfrog-cli
func saveMavenBuildInfoForJfrogCli(buildInfo *entities.BuildInfo) error {
	buildInfoService := build.NewBuildInfoService()
	buildInstance, err := buildInfoService.GetOrCreateBuildWithProject(
		buildInfo.Name,
		buildInfo.Number,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to get or create build: %w", err)
	}

	err = buildInstance.SaveBuildInfo(buildInfo)
	if err != nil {
		return fmt.Errorf("failed to save build info: %w", err)
	}

	log.Debug("Successfully saved Maven build info for jfrog-cli")
	return nil
}

// --- Deployment repository resolution ---------------------------------------------------------
// The deploy repository is needed to scope build-property tagging of deployed artifacts. Rather than
// parse raw pom.xml/settings.xml (which would miss property interpolation, parent inheritance and
// profile activation), we ask Maven for its EFFECTIVE model via help:effective-settings /
// help:effective-pom and parse only the deployment-repository fields out of that resolved output.

type effectivePomProject struct {
	GroupID                string `xml:"groupId"`
	ArtifactID             string `xml:"artifactId"`
	Version                string `xml:"version"`
	DistributionManagement struct {
		Repository struct {
			URL string `xml:"url"`
		} `xml:"repository"`
		SnapshotRepository struct {
			URL string `xml:"url"`
		} `xml:"snapshotRepository"`
	} `xml:"distributionManagement"`
}

type effectiveSettingsProfile struct {
	AltDeploymentRepository         string `xml:"properties>altDeploymentRepository"`
	AltReleaseDeploymentRepository  string `xml:"properties>altReleaseDeploymentRepository"`
	AltSnapshotDeploymentRepository string `xml:"properties>altSnapshotDeploymentRepository"`
}

// GetDeploymentRepositories resolves where Maven deploys, from its effective model. It returns exactly
// one of:
//   - overrideURL: a single URL applied to ALL modules - Maven's -DaltDeploymentRepository or an
//     active-profile altDeploymentRepository in (effective) settings, which override distributionManagement; or
//   - moduleURLs: per-module (moduleId "groupId:artifactId:version" -> URL) distributionManagement from
//     the effective pom, so a reactor whose modules deploy to different repos is handled correctly.
//
// Both empty means no deployment repository is configured. Precedence follows Maven (override wins).
func (mf *MavenFlexPack) GetDeploymentRepositories() (moduleURLs map[string]string, overrideURL string, err error) {
	isSnapshot := strings.HasSuffix(strings.TrimSpace(mf.projectVersion), "-SNAPSHOT")

	// 1. Command-line -Dalt[Snapshot|Release]DeploymentRepository (highest precedence, no mvn run).
	if repoURL := altDeploymentRepoURLFromArgs(mf.config.ExtraArgs, isSnapshot); repoURL != "" {
		return nil, repoURL, nil
	}
	// 2. altDeploymentRepository from active-profile settings (effective-settings).
	if repoURL, sErr := mf.deployURLFromEffectiveSettings(isSnapshot); sErr != nil {
		log.Debug("Could not resolve deploy repository from effective settings: " + sErr.Error())
	} else if repoURL != "" {
		return nil, repoURL, nil
	}
	// 3. Per-module distributionManagement from the pom (effective-pom: inheritance + interpolation +
	//    active profiles resolved by Maven; snapshot/release from each module's effective version).
	moduleURLs, err = mf.moduleDeployURLsFromEffectivePom(isSnapshot)
	return moduleURLs, "", err
}

func (mf *MavenFlexPack) moduleDeployURLsFromEffectivePom(fallbackSnapshot bool) (map[string]string, error) {
	content, err := mf.runMavenHelpGoal("help:effective-pom")
	if err != nil {
		return nil, err
	}
	return parseModuleDeployURLs(content, fallbackSnapshot)
}

// parseModuleDeployURLs maps each effective-pom project's module id ("groupId:artifactId:version") to its
// distributionManagement URL, choosing snapshotRepository vs repository from each project's EFFECTIVE
// (interpolated) version - so "${revision}" resolved to -SNAPSHOT is classified correctly - falling back
// to fallbackSnapshot when a project has no version. Modules without distributionManagement are omitted.
func parseModuleDeployURLs(content string, fallbackSnapshot bool) (map[string]string, error) {
	// help:effective-pom emits a single <project> for one module, or a <projects> wrapper for a reactor.
	var wrapper struct {
		Projects []effectivePomProject `xml:"project"`
	}
	b := []byte(content)
	var projects []effectivePomProject
	if err := xml.Unmarshal(b, &wrapper); err == nil && len(wrapper.Projects) > 0 {
		projects = wrapper.Projects
	} else {
		var single effectivePomProject
		if err := xml.Unmarshal(b, &single); err != nil {
			return nil, fmt.Errorf("failed to parse effective-pom: %w", err)
		}
		projects = []effectivePomProject{single}
	}
	urls := make(map[string]string)
	for _, project := range projects {
		url := distributionURL(project, fallbackSnapshot)
		if url == "" || project.GroupID == "" || project.ArtifactID == "" || project.Version == "" {
			continue
		}
		urls[project.GroupID+":"+project.ArtifactID+":"+project.Version] = url
	}
	return urls, nil
}

// distributionURL returns a project's distributionManagement URL, snapshot vs release chosen from its
// effective version (fallbackSnapshot when the version is absent).
func distributionURL(project effectivePomProject, fallbackSnapshot bool) string {
	isSnapshot := fallbackSnapshot
	if project.Version != "" {
		isSnapshot = strings.HasSuffix(strings.TrimSpace(project.Version), "-SNAPSHOT")
	}
	dm := project.DistributionManagement
	if isSnapshot && dm.SnapshotRepository.URL != "" {
		return dm.SnapshotRepository.URL
	}
	if dm.Repository.URL != "" {
		return dm.Repository.URL
	}
	return ""
}

func (mf *MavenFlexPack) deployURLFromEffectiveSettings(isSnapshot bool) (string, error) {
	content, err := mf.runMavenHelpGoal("help:effective-settings")
	if err != nil {
		return "", err
	}
	var settings struct {
		Profiles []effectiveSettingsProfile `xml:"profiles>profile"`
	}
	if err := xml.Unmarshal([]byte(content), &settings); err != nil {
		return "", fmt.Errorf("failed to parse effective-settings: %w", err)
	}
	result := ""
	for _, profile := range settings.Profiles {
		switch {
		case isSnapshot && profile.AltSnapshotDeploymentRepository != "":
			result = repoURLFromAltValue(profile.AltSnapshotDeploymentRepository)
		case !isSnapshot && profile.AltReleaseDeploymentRepository != "":
			result = repoURLFromAltValue(profile.AltReleaseDeploymentRepository)
		case profile.AltDeploymentRepository != "":
			result = repoURLFromAltValue(profile.AltDeploymentRepository)
		}
	}
	return result, nil
}

// runMavenHelpGoal runs a maven-help-plugin goal that writes its output to a file (-Doutput), forwarding
// the same resolution flags used for the build, and returns the file content.
func (mf *MavenFlexPack) runMavenHelpGoal(goal string) (string, error) {
	outFile, err := os.CreateTemp("", "flexpack-effective-*.xml")
	if err != nil {
		return "", err
	}
	outPath := outFile.Name()
	_ = outFile.Close()
	defer func() {
		if removeErr := os.Remove(outPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Debug("Failed to remove temporary file " + outPath + ": " + removeErr.Error())
		}
	}()

	args := []string{goal, "-Doutput=" + outPath, "-q", "-B"}
	args = append(args, mf.config.ExtraArgs...)
	cmd := exec.Command(mf.config.MavenExecutable, args...)
	cmd.Dir = mf.config.WorkingDirectory
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return "", fmt.Errorf("mvn %s failed: %w\nOutput: %s", goal, runErr, string(out))
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// altDeploymentRepoURLFromArgs extracts the deployment URL from a -Dalt[Snapshot|Release]DeploymentRepository
// argument (value form "id::layout::url" or "id::url"), honoring snapshot/release specificity.
func altDeploymentRepoURLFromArgs(args []string, isSnapshot bool) string {
	var general, release, snapshot string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "-DaltSnapshotDeploymentRepository="); ok {
			snapshot = repoURLFromAltValue(value)
			continue
		}
		if value, ok := strings.CutPrefix(arg, "-DaltReleaseDeploymentRepository="); ok {
			release = repoURLFromAltValue(value)
			continue
		}
		if value, ok := strings.CutPrefix(arg, "-DaltDeploymentRepository="); ok {
			general = repoURLFromAltValue(value)
		}
	}
	if isSnapshot && snapshot != "" {
		return snapshot
	}
	if !isSnapshot && release != "" {
		return release
	}
	return general
}

// repoURLFromAltValue returns the URL component of an altDeploymentRepository value ("id::layout::url"
// or "id::url"); the URL is the final "::"-separated segment.
func repoURLFromAltValue(value string) string {
	parts := strings.Split(value, "::")
	return parts[len(parts)-1]
}
