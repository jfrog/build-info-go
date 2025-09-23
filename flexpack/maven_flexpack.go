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
	result.WriteString(fmt.Sprintf("Project: %s:%s:%s\n", mf.groupId, mf.artifactId, mf.projectVersion))
	result.WriteString("Dependencies:\n")
	for _, dep := range mf.dependencies {
		result.WriteString(fmt.Sprintf("  - %s:%s [%s]\n", dep.Name, dep.Version, dep.Type))
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
		log.Debug("Maven dependency:tree parsing failed, falling back to POM parsing: " + err.Error())
	}
	mf.parseFromPOM()
}

// parseWithMavenDependencyTree uses mvn dependency:tree to get complete dependency information
func (mf *MavenFlexPack) parseWithMavenDependencyTree() error {
	args := []string{"dependency:tree", "-DoutputType=text", "-Dverbose"}
	if mf.config.SkipTests {
		args = append(args, "-DskipTests")
	}

	cmd := exec.Command(mf.config.MavenExecutable, args...)
	cmd.Dir = mf.config.WorkingDirectory

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
	seenDependencies := make(map[string]bool)
	depRegex := regexp.MustCompile(`\[INFO\]\s*[+\\|\s-]*\s*([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+)\s*$`)

	for _, line := range lines {
		if strings.Contains(line, "omitted for duplicate") {
			continue
		}
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

		if groupId == "" || artifactId == "" || version == "" {
			continue
		}

		dependencyId := fmt.Sprintf("%s:%s:%s", groupId, artifactId, version)

		if seenDependencies[dependencyId] {
			continue
		}
		seenDependencies[dependencyId] = true

		level := mf.calculateTreeLevelFromLine(line)
		if level <= len(parentStack) {
			parentStack = parentStack[:level]
		}
		if level > 0 && len(parentStack) > 0 {
			currentParent = parentStack[len(parentStack)-1]
		} else {
			currentParent = ""
		}

		// Check if this is a test dependency (for filtering purposes)
		isTestDependency := strings.ToLower(scope) == "test"
		if !mf.config.IncludeTestDependencies && isTestDependency {
			continue
		}

		dep := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", groupId, artifactId),
			Version: version,
			Type:    mf.mapPackagingToType(packaging),
			Scopes:  mf.mapMavenScopeToScopes(scope), // Use actual Maven scope from dependency tree
		}

		mf.dependencies = append(mf.dependencies, dep)
		if currentParent != "" {
			if mf.dependencyGraph[currentParent] == nil {
				mf.dependencyGraph[currentParent] = []string{}
			}
			mf.dependencyGraph[currentParent] = append(mf.dependencyGraph[currentParent], dependencyId)
		}
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

// calculateTreeLevelFromLine calculates the indentation level from the full dependency tree line
func (mf *MavenFlexPack) calculateTreeLevelFromLine(line string) int {
	// Find the position after [INFO] and count the tree depth
	infoPos := strings.Index(line, "[INFO]")
	if infoPos == -1 {
		return 0
	}

	// Check bounds before slicing to avoid out-of-bounds error
	if infoPos+6 >= len(line) {
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

// wasDeployGoalExecuted checks if the deploy goal was part of the Maven command
func (mf *MavenFlexPack) wasDeployGoalExecuted() bool {
	// Check os.Args for Maven goals
	for _, arg := range os.Args {
		if strings.Contains(arg, "deploy") {
			return true
		}
	}
	return false
}

// getDeploymentRepository extracts repository information natively from Maven POM and settings
// Returns empty string if deploy goal was not executed (e.g., for verify, install without deploy)
func (mf *MavenFlexPack) getDeploymentRepository() string {
	// If deploy goal was not executed, don't set repository
	if !mf.wasDeployGoalExecuted() {
		return ""
	}
	// Parse distributionManagement from pom.xml (native Maven approach)
	pomPath := filepath.Join(mf.config.WorkingDirectory, "pom.xml")
	content, err := os.ReadFile(pomPath)
	if err != nil {
		log.Debug("Could not read pom.xml for deployment repository extraction: " + err.Error())
		return ""
	}

	pomContent := string(content)

	// Extract repository ID from distributionManagement section
	repoId := mf.extractRepositoryFromDistributionManagement(pomContent)
	if repoId != "" {
		log.Debug("Found deployment repository in pom.xml distributionManagement: " + repoId)
		return repoId
	}

	// Try to extract from Maven settings.xml (native Maven approach)
	settingsRepo := mf.extractRepositoryFromSettings()
	if settingsRepo != "" {
		log.Debug("Found deployment repository in Maven settings.xml: " + settingsRepo)
		return settingsRepo
	}

	log.Debug("No deployment repository found in native Maven configuration")
	return ""
}

// extractRepositoryFromDistributionManagement parses distributionManagement section from pom.xml
func (mf *MavenFlexPack) extractRepositoryFromDistributionManagement(pomContent string) string {
	// Look for distributionManagement section
	distMgmtRegex := regexp.MustCompile(`(?s)<distributionManagement>(.*?)</distributionManagement>`)
	distMgmtMatch := distMgmtRegex.FindStringSubmatch(pomContent)
	if len(distMgmtMatch) < 2 {
		return ""
	}

	distMgmtSection := distMgmtMatch[1]

	// Determine if this is a SNAPSHOT version to choose appropriate repository
	isSnapshot := strings.Contains(strings.ToUpper(mf.projectVersion), "SNAPSHOT")

	var repoPattern string
	if isSnapshot {
		// Look for snapshotRepository first for SNAPSHOT versions
		repoPattern = `(?s)<snapshotRepository>(.*?)</snapshotRepository>`
	} else {
		// Look for repository for release versions
		repoPattern = `(?s)<repository>(.*?)</repository>`
	}

	repoRegex := regexp.MustCompile(repoPattern)
	repoMatch := repoRegex.FindStringSubmatch(distMgmtSection)
	if len(repoMatch) < 2 {
		// Fallback: try the other repository type
		if isSnapshot {
			repoPattern = `(?s)<repository>(.*?)</repository>`
		} else {
			repoPattern = `(?s)<snapshotRepository>(.*?)</snapshotRepository>`
		}
		repoRegex = regexp.MustCompile(repoPattern)
		repoMatch = repoRegex.FindStringSubmatch(distMgmtSection)
		if len(repoMatch) < 2 {
			return ""
		}
	}

	repoSection := repoMatch[1]

	// Extract repository ID
	idRegex := regexp.MustCompile(`<id>(.*?)</id>`)
	idMatch := idRegex.FindStringSubmatch(repoSection)
	if len(idMatch) >= 2 {
		repoId := strings.TrimSpace(idMatch[1])
		if repoId != "" {
			return repoId
		}
	}

	return ""
}

// extractRepositoryFromSettings parses deployment repository from Maven settings.xml
func (mf *MavenFlexPack) extractRepositoryFromSettings() string {
	// Try multiple common locations for settings.xml
	settingsPaths := []string{
		filepath.Join(mf.config.WorkingDirectory, "settings.xml"),
		filepath.Join(os.Getenv("HOME"), ".m2", "settings.xml"),
		"/usr/local/share/maven/conf/settings.xml",
		"/opt/maven/conf/settings.xml",
	}

	for _, settingsPath := range settingsPaths {
		if content, err := os.ReadFile(settingsPath); err == nil {
			settingsContent := string(content)

			// Look for activeProfiles and profiles with repository configuration
			// This is a simplified approach - full Maven settings parsing would be more complex
			if strings.Contains(settingsContent, "<repository>") || strings.Contains(settingsContent, "<repositories>") {
				// Extract first repository ID found (simplified)
				idRegex := regexp.MustCompile(`<repository>.*?<id>(.*?)</id>`)
				idMatch := idRegex.FindStringSubmatch(settingsContent)
				if len(idMatch) >= 2 {
					repoId := strings.TrimSpace(idMatch[1])
					if repoId != "" {
						log.Debug("Found repository in settings.xml: " + settingsPath)
						return repoId
					}
				}
			}
		}
	}

	return ""
}

// mapMavenScopeToScopes maps Maven dependency scope to build-info scopes
func (mf *MavenFlexPack) mapMavenScopeToScopes(scope string) []string {
	// Handle empty scope (Maven default is compile)
	if scope == "" {
		scope = "compile"
	}

	// Maven scopes map directly to build-info scopes
	switch strings.ToLower(scope) {
	case "compile":
		return []string{"compile"}
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
		// Unknown scope, default to compile
		return []string{"compile"}
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
		log.Warn(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
	}

	// Strategy 2: Future enhancement - could call Artifactory API to get real checksums
	// Example: GET /api/storage/{repo}/{path}?checksums=sha1,sha256,md5
	// This would provide authentic checksums from the repository

	// Strategy 3: Warn and return nil to exclude dependency with missing checksums
	log.Warn(fmt.Sprintf("Failed to calculate checksums for dependency: %s:%s", dep.Name, dep.Version))
	return nil
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

	// Build path to Maven local repository (Windows and Unix compatible)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Debug("Failed to get user home directory: " + err.Error())
		return ""
	}
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
	if len(mf.dependencies) == 0 {
		mf.parseDependencies()
	}
	requestedByMap := mf.CalculateRequestedBy()
	var dependencies []entities.Dependency
	for _, dep := range mf.dependencies {
		// Try to calculate checksum for this specific dependency
		checksumMap := mf.calculateChecksumWithFallback(dep)
		
		// Convert checksum map to entities.Checksum struct
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
		// If checksumMap is nil, checksum will have empty values, which is fine

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

	// Create build info using existing factory method (following Poetry FlexPack pattern)
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
		Modules: []entities.Module{},
	}

	// Add Maven module
	module := entities.Module{
		Id:           fmt.Sprintf("%s:%s:%s", mf.groupId, mf.artifactId, mf.projectVersion),
		Type:         "maven",
		Repository:   mf.getDeploymentRepository(),
		Dependencies: dependencies,
	}
	buildInfo.Modules = append(buildInfo.Modules, module)

	return buildInfo, nil
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
			log.Info("Build info saved for jfrog-cli compatibility")
		}

		log.Debug(fmt.Sprintf("Build info: %+v", buildInfo))
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
