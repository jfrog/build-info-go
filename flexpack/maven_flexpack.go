package flexpack

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
}

// MavenConfig represents configuration for Maven FlexPack
type MavenConfig struct {
	WorkingDirectory        string
	IncludeTestDependencies bool
	MavenExecutable         string
	SkipTests               bool
}

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
	}

	// Set default Maven executable if not specified
	if mf.config.MavenExecutable == "" {
		mf.config.MavenExecutable = "mvn"
	}

	// Load pom.xml
	if err := mf.loadPOM(); err != nil {
		return nil, fmt.Errorf("failed to load pom.xml: %w", err)
	}

	return mf, nil
}

// GetDependency returns dependency information along with name and version
func (mf *MavenFlexPack) GetDependency() string {
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Project: %s:%s:%s\n", mf.groupId, mf.artifactId, mf.projectVersion))
	result.WriteString("Dependencies:\n")
	for _, dep := range mf.dependencies {
		result.WriteString(fmt.Sprintf("  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type))
	}
	return result.String()
}

// ParseDependencyToList parses and returns a list of dependencies with their name and version
func (mf *MavenFlexPack) ParseDependencyToList() []string {
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}
	var depList []string
	for _, dep := range mf.dependencies {
		depList = append(depList, fmt.Sprintf("%s:%s", dep.Name, dep.Version))
	}
	return depList
}

// CalculateChecksum calculates checksums for dependencies
func (mf *MavenFlexPack) CalculateChecksum() []map[string]interface{} {
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}

	var checksums []map[string]interface{}
	for _, dep := range mf.dependencies {
		checksumMap := mf.calculateChecksumWithFallback(dep)
		checksums = append(checksums, checksumMap)
	}
	return checksums
}

// CalculateScopes calculates and returns the scopes for dependencies
func (mf *MavenFlexPack) CalculateScopes() []string {
	scopes := make(map[string]bool)
	for _, dep := range mf.dependencies {
		for _, scope := range dep.Scopes {
			scopes[scope] = true
		}
	}

	var scopeList []string
	for scope := range scopes {
		scopeList = append(scopeList, scope)
	}
	return scopeList
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

	// Extract project information
	mf.groupId = mf.pomData.GroupId
	mf.artifactId = mf.pomData.ArtifactId
	mf.projectVersion = mf.pomData.Version
	mf.projectName = fmt.Sprintf("%s:%s", mf.groupId, mf.artifactId)

	log.Debug(fmt.Sprintf("Loaded Maven project: %s:%s:%s", mf.groupId, mf.artifactId, mf.projectVersion))
	return nil
}

// parseDependencies parses dependencies using hybrid strategy
func (mf *MavenFlexPack) parseDependencies() {
	log.Debug("Starting Maven dependency parsing...")

	// Strategy 1: Try CLI-based resolution (most comprehensive)
	if err := mf.parseWithMavenDependencyTree(); err == nil {
		log.Debug("Successfully parsed dependencies using 'mvn dependency:tree'")
		return
	} else {
		log.Debug("Maven dependency:tree parsing failed, falling back to POM parsing: " + err.Error())
	}

	// Strategy 2: Fallback to POM parsing (more limited but reliable)
	mf.parseFromPOM()
	log.Debug("Used POM-based dependency parsing")
}

// parseWithMavenDependencyTree uses mvn dependency:tree to get complete dependency information
func (mf *MavenFlexPack) parseWithMavenDependencyTree() error {
	args := []string{"dependency:tree", "-DoutputType=text", "-Dverbose"}
	if mf.config.SkipTests {
		args = append(args, "-DskipTests")
	}

	cmd := exec.Command(mf.config.MavenExecutable, args...)
	cmd.Dir = mf.config.WorkingDirectory

	log.Debug(fmt.Sprintf("Executing: %s %s", mf.config.MavenExecutable, strings.Join(args, " ")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mvn dependency:tree failed: %w\nOutput: %s", err, string(output))
	}

	return mf.parseDependencyTreeOutput(string(output))
}

// parseDependencyTreeOutput parses the output of mvn dependency:tree
func (mf *MavenFlexPack) parseDependencyTreeOutput(output string) error {
	lines := strings.Split(output, "\n")
	var currentParent string
	parentStack := []string{}
	seenDependencies := make(map[string]bool) // Track to avoid duplicates

	// Regex to parse dependency tree lines
	// Format: [INFO] +- groupId:artifactId:type:version:scope
	// Must have exactly 5 colon-separated parts after tree characters
	depRegex := regexp.MustCompile(`\[INFO\]\s*[+\\|\s-]*\s*([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+)\s*$`)

	for _, line := range lines {
		// Skip lines that contain "omitted for duplicate"
		if strings.Contains(line, "omitted for duplicate") {
			continue
		}

		// Skip lines that don't look like dependency declarations
		if !strings.Contains(line, "[INFO]") ||
			strings.Contains(line, "BUILD SUCCESS") ||
			strings.Contains(line, "Total time") ||
			strings.Contains(line, "Finished at") ||
			strings.Contains(line, "Scanning for projects") ||
			strings.Contains(line, "Building ") ||
			strings.Contains(line, "Downloaded from") ||
			strings.Contains(line, "Downloading from") ||
			strings.Contains(line, "maven-") ||
			strings.Contains(line, "---") {
			continue
		}

		matches := depRegex.FindStringSubmatch(line)
		if len(matches) != 6 {
			continue
		}

		groupId := strings.TrimSpace(matches[1])
		artifactId := strings.TrimSpace(matches[2])
		packaging := strings.TrimSpace(matches[3])
		version := strings.TrimSpace(matches[4])
		scope := strings.TrimSpace(matches[5])

		// Skip if any field is empty or contains invalid characters
		if groupId == "" || artifactId == "" || version == "" {
			continue
		}

		dependencyId := fmt.Sprintf("%s:%s:%s", groupId, artifactId, version)

		// Skip if we've already seen this dependency
		if seenDependencies[dependencyId] {
			continue
		}
		seenDependencies[dependencyId] = true

		// Calculate dependency level based on line prefix
		level := mf.calculateTreeLevelFromLine(line)

		// Update parent stack based on level
		if level <= len(parentStack) {
			parentStack = parentStack[:level]
		}
		if level > 0 && len(parentStack) > 0 {
			currentParent = parentStack[len(parentStack)-1]
		} else {
			currentParent = ""
		}

		// Create dependency info
		dep := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", groupId, artifactId),
			Version: version,
			Type:    mf.mapPackagingToType(packaging),
			Scopes:  mf.mapMavenScope(scope),
		}

		// Filter test dependencies if not included
		if !mf.config.IncludeTestDependencies && mf.containsScope(dep.Scopes, "test") {
			continue
		}

		// Add to dependencies list
		mf.dependencies = append(mf.dependencies, dep)

		// Build dependency graph
		if currentParent != "" {
			if mf.dependencyGraph[currentParent] == nil {
				mf.dependencyGraph[currentParent] = []string{}
			}
			mf.dependencyGraph[currentParent] = append(mf.dependencyGraph[currentParent], dependencyId)
		}

		// Update parent stack for next iteration
		if level < len(parentStack) {
			parentStack = parentStack[:level+1]
			parentStack[level] = dependencyId
		} else {
			parentStack = append(parentStack, dependencyId)
		}
	}

	log.Debug(fmt.Sprintf("Parsed %d unique dependencies from Maven dependency tree", len(mf.dependencies)))
	return nil
}

// parseFromPOM parses dependencies directly from pom.xml
func (mf *MavenFlexPack) parseFromPOM() {
	log.Debug("Parsing dependencies from pom.xml...")

	for _, dep := range mf.pomData.Dependencies.Dependency {
		dependencyId := fmt.Sprintf("%s:%s:%s", dep.GroupId, dep.ArtifactId, dep.Version)

		depInfo := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", dep.GroupId, dep.ArtifactId),
			Version: dep.Version,
			Type:    mf.mapPackagingToType(dep.Type),
			Scopes:  mf.mapMavenScope(dep.Scope),
		}

		// Filter test dependencies if not included
		if !mf.config.IncludeTestDependencies && mf.containsScope(depInfo.Scopes, "test") {
			continue
		}

		mf.dependencies = append(mf.dependencies, depInfo)
	}

	log.Debug(fmt.Sprintf("Parsed %d dependencies from pom.xml", len(mf.dependencies)))
}

// calculateTreeLevelFromLine calculates the indentation level from the full dependency tree line
func (mf *MavenFlexPack) calculateTreeLevelFromLine(line string) int {
	// Find the position after [INFO] and count the tree depth
	infoPos := strings.Index(line, "[INFO]")
	if infoPos == -1 {
		return 0
	}

	// Look for the dependency part after [INFO]
	afterInfo := line[infoPos+6:] // Skip "[INFO]"

	// Count the number of tree indentation groups
	// Maven tree format: "   +- " or "   |  +- " or "   |  |  +- "
	level := 0
	i := 0
	for i < len(afterInfo) {
		// Skip initial spaces
		for i < len(afterInfo) && afterInfo[i] == ' ' {
			i++
		}

		// Check for tree characters
		if i < len(afterInfo) {
			char := afterInfo[i]
			switch char {
			case '+', '\\':
				// This indicates a dependency at this level
				return level
			case '|':
				// Vertical bar indicates we're going deeper
				level++
				i++
				// Skip spaces after |
				for i < len(afterInfo) && afterInfo[i] == ' ' {
					i++
				}
			default:
				return level
			}
		} else {
			return level
		}
	}

	return level
}

// containsScope checks if a dependency contains a specific scope
func (mf *MavenFlexPack) containsScope(scopes []string, targetScope string) bool {
	for _, scope := range scopes {
		if scope == targetScope {
			return true
		}
	}
	return false
}

// getDeploymentRepository extracts repository information from JFrog CLI config or POM
func (mf *MavenFlexPack) getDeploymentRepository() string {
	// First, try to read from JFrog CLI Maven configuration
	configPath := filepath.Join(mf.config.WorkingDirectory, ".jfrog", "projects", "maven.yaml")
	if content, err := os.ReadFile(configPath); err == nil {
		// Simple parsing for deployment repository
		lines := strings.Split(string(content), "\n")
		inDeployer := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "deployer:" {
				inDeployer = true
				continue
			}
			if inDeployer {
				if strings.HasPrefix(trimmed, "releaseRepo:") {
					repo := strings.TrimSpace(strings.TrimPrefix(trimmed, "releaseRepo:"))
					if repo != "" && !strings.Contains(mf.projectVersion, "SNAPSHOT") {
						log.Debug("Using release repository from maven.yaml: " + repo)
						return repo
					}
				}
				if strings.HasPrefix(trimmed, "snapshotRepo:") {
					repo := strings.TrimSpace(strings.TrimPrefix(trimmed, "snapshotRepo:"))
					if repo != "" && strings.Contains(mf.projectVersion, "SNAPSHOT") {
						log.Debug("Using snapshot repository from maven.yaml: " + repo)
						return repo
					}
				}
				// Stop when we hit another section
				if strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "releaseRepo") && !strings.HasPrefix(trimmed, "snapshotRepo") && !strings.HasPrefix(trimmed, "serverId") {
					break
				}
			}
		}
	}

	// Fallback: Read POM content to look for distributionManagement
	pomPath := filepath.Join(mf.config.WorkingDirectory, "pom.xml")
	content, err := os.ReadFile(pomPath)
	if err != nil {
		return ""
	}

	pomContent := string(content)

	// Look for repository ID in distributionManagement
	if strings.Contains(pomContent, "maven-flexpack-local") {
		return "maven-flexpack-local"
	}

	// Default fallback
	return ""
}

// mapMavenScope maps Maven scopes to standardized scopes
func (mf *MavenFlexPack) mapMavenScope(scope string) []string {
	if scope == "" {
		scope = "compile" // Maven default scope
	}

	switch strings.ToLower(scope) {
	case "compile":
		return []string{"compile", "runtime"}
	case "runtime":
		return []string{"runtime"}
	case "test":
		return []string{"test"}
	case "provided":
		return []string{"provided"}
	case "system":
		return []string{"system"}
	case "import":
		return []string{"import"}
	default:
		return []string{scope}
	}
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

	// Strategy 1: Try to find artifact in Maven local repository
	if artifactPath := mf.findMavenArtifact(dep); artifactPath != "" {
		if sha1, sha256, md5, err := mf.calculateFileChecksum(artifactPath); err == nil {
			checksumMap["sha1"] = sha1
			checksumMap["sha256"] = sha256
			checksumMap["md5"] = md5
			checksumMap["path"] = artifactPath
			return checksumMap
		}
		log.Debug(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
	}

	// Strategy 2: Generate manifest-based checksum
	if sha1, sha256, md5, err := mf.calculateManifestChecksum(dep); err == nil {
		checksumMap["sha1"] = sha1
		checksumMap["sha256"] = sha256
		checksumMap["md5"] = md5
		checksumMap["path"] = "manifest"
		return checksumMap
	}

	// Strategy 3: Return empty checksums (graceful degradation)
	checksumMap["sha1"] = ""
	checksumMap["sha256"] = ""
	checksumMap["md5"] = ""
	checksumMap["path"] = ""

	return checksumMap
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

	// Build path to Maven local repository
	homeDir, _ := os.UserHomeDir()
	localRepo := filepath.Join(homeDir, ".m2", "repository")

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

	return fileDetails.Checksum.Sha1,
		fileDetails.Checksum.Sha256,
		fileDetails.Checksum.Md5,
		nil
}

// calculateManifestChecksum generates a deterministic checksum from dependency metadata
func (mf *MavenFlexPack) calculateManifestChecksum(dep DependencyInfo) (string, string, string, error) {
	// Create deterministic manifest content
	manifest := fmt.Sprintf("id:%s\nname:%s\nversion:%s\ntype:%s\nscopes:%s\n",
		dep.ID, dep.Name, dep.Version, dep.Type, strings.Join(dep.Scopes, ","))

	// Use crypto utility to calculate checksums
	tempFile, err := os.CreateTemp("", "maven-checksum-*.txt")
	if err != nil {
		return "", "", "", err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.WriteString(manifest); err != nil {
		return "", "", "", err
	}

	return mf.calculateFileChecksum(tempFile.Name())
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

// buildRequestedByMap builds the requested-by relationship map
func (mf *MavenFlexPack) buildRequestedByMap() {
	// Invert the dependency graph to create requested-by relationships
	for parent, children := range mf.dependencyGraph {
		for _, child := range children {
			if mf.requestedByMap[child] == nil {
				mf.requestedByMap[child] = []string{}
			}
			mf.requestedByMap[child] = append(mf.requestedByMap[child], parent)
		}
	}
}

// CollectBuildInfo collects complete build information for Maven project
func (mf *MavenFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	log.Info(fmt.Sprintf("Collecting Maven build info for %s #%s", buildName, buildNumber))

	// Parse dependencies
	mf.parseDependencies()

	// Calculate checksums
	checksums := mf.CalculateChecksum()

	// Build requested-by relationships
	requestedByMap := mf.CalculateRequestedBy()

	// Convert to entities format
	var dependencies []entities.Dependency
	for i, dep := range mf.dependencies {
		var checksumMap map[string]interface{}
		if i < len(checksums) {
			checksumMap = checksums[i]
		} else {
			checksumMap = map[string]interface{}{
				"sha1": "", "sha256": "", "md5": "",
			}
		}

		// Convert checksum map to entities.Checksum struct
		checksum := entities.Checksum{}
		if sha1, ok := checksumMap["sha1"].(string); ok {
			checksum.Sha1 = sha1
		}
		if sha256, ok := checksumMap["sha256"].(string); ok {
			checksum.Sha256 = sha256
		}
		if md5, ok := checksumMap["md5"].(string); ok {
			checksum.Md5 = md5
		}

		entity := entities.Dependency{
			Id:       dep.ID,
			Type:     dep.Type,
			Scopes:   dep.Scopes,
			Checksum: checksum,
		}

		// Add requested-by relationships
		if requesters, exists := requestedByMap[dep.ID]; exists && len(requesters) > 0 {
			entity.RequestedBy = [][]string{requesters}
		}

		dependencies = append(dependencies, entity)
	}

	// Create build info (following Poetry FlexPack pattern - no artifacts, only dependencies)
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
		Modules: []entities.Module{
			{
				Id:           fmt.Sprintf("%s:%s:%s", mf.groupId, mf.artifactId, mf.projectVersion),
				Type:         "maven",
				Repository:   mf.getDeploymentRepository(),
				Dependencies: dependencies,
				// No artifacts - let traditional Maven system handle them
			},
		},
	}

	log.Info(fmt.Sprintf("Collected build info with %d dependencies", len(dependencies)))
	return buildInfo, nil
}

// RunMavenInstallWithBuildInfo runs mvn install and collects build information
func RunMavenInstallWithBuildInfo(workingDir string, buildName, buildNumber string, includeTestDeps bool, extraArgs []string) error {
	log.Info("Running Maven install with build info collection...")

	// Create configuration
	config := MavenConfig{
		WorkingDirectory:        workingDir,
		IncludeTestDependencies: includeTestDeps,
	}

	// Create Maven FlexPack instance
	mavenFlex, err := NewMavenFlexPack(config)
	if err != nil {
		return fmt.Errorf("failed to create Maven instance: %w", err)
	}

	// Run mvn install command
	args := append([]string{"install"}, extraArgs...)
	if !includeTestDeps {
		args = append(args, "-DskipTests")
	}

	cmd := exec.Command(config.MavenExecutable, args...)
	cmd.Dir = workingDir
	log.Debug("Executing command:", cmd.String())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mvn install failed: %w\nOutput: %s", err, string(output))
	}

	log.Info("Maven install completed successfully")

	// Collect build information if build name and number are provided
	if buildName != "" && buildNumber != "" {
		log.Info("Collecting build information...")

		// Use the CollectBuildInfo method to get complete build info
		buildInfo, err := mavenFlex.CollectBuildInfo(buildName, buildNumber)
		if err != nil {
			return fmt.Errorf("failed to collect build info: %w", err)
		}

		// Save build info using build-info-go service for jfrog-cli compatibility
		err = saveMavenBuildInfoForJfrogCli(buildInfo)
		if err != nil {
			log.Warn("Failed to save build info for jfrog-cli compatibility: " + err.Error())
			// Don't fail the entire operation, but warn the user
		} else {
			log.Info("Build info saved for jfrog-cli compatibility")
		}

		log.Info("Build information collection completed")
		log.Debug(fmt.Sprintf("Build info: %+v", buildInfo))
	}

	return nil
}

// GetProjectDependencies returns all project dependencies with full details
func (mf *MavenFlexPack) GetProjectDependencies() ([]DependencyInfo, error) {
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}

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
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}

	return mf.dependencyGraph, nil
}

// getMavenVersion gets the Maven version for build info
func (mf *MavenFlexPack) getMavenVersion() string {
	cmd := exec.Command(mf.config.MavenExecutable, "--version")
	output, err := cmd.Output()
	if err != nil {
		log.Debug("Failed to get Maven version: " + err.Error())
		return "unknown"
	}

	version := strings.TrimSpace(string(output))
	// Maven version output format: "Apache Maven 3.8.8 (4c87b05d9aedce574290d1acc98575ed5eb6cd39)"
	lines := strings.Split(version, "\n")
	if len(lines) > 0 {
		firstLine := lines[0]
		if parts := strings.Fields(firstLine); len(parts) >= 3 {
			return parts[2] // Return the version number
		}
	}
	return "unknown"
}

// saveMavenBuildInfoForJfrogCli saves build info in a format compatible with jfrog-cli
func saveMavenBuildInfoForJfrogCli(buildInfo *entities.BuildInfo) error {
	log.Debug("Saving Maven build info for jfrog-cli compatibility")

	// Use build-info-go's build service to save build info
	buildInfoService := build.NewBuildInfoService()

	// Get or create build instance
	buildInstance, err := buildInfoService.GetOrCreateBuildWithProject(
		buildInfo.Name,
		buildInfo.Number,
		"", // project key - can be empty for now
	)
	if err != nil {
		return fmt.Errorf("failed to get or create build: %w", err)
	}

	// Save build info using the Build instance
	err = buildInstance.SaveBuildInfo(buildInfo)
	if err != nil {
		return fmt.Errorf("failed to save build info: %w", err)
	}

	log.Debug("Successfully saved Maven build info for jfrog-cli")
	return nil
}
