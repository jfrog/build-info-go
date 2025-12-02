package flexpack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

type GradleFlexPack struct {
	config          GradleConfig
	dependencies    []DependencyInfo
	dependencyGraph map[string][]string
	projectName     string
	projectVersion  string
	groupId         string
	artifactId      string
	requestedByMap  map[string][]string
	buildGradlePath string
}

type gradleDepNode struct {
	Group    string
	Module   string
	Version  string
	Type     string
	Reason   string
	Children []gradleDepNode
}

type gradleNodePtr struct {
	Group    string
	Module   string
	Version  string
	Type     string
	Children []*gradleNodePtr
}

func NewGradleFlexPack(config GradleConfig) (*GradleFlexPack, error) {
	gf := &GradleFlexPack{
		config:          config,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}

	if gf.config.GradleExecutable == "" {
		gf.config.GradleExecutable = gf.getGradleExecutablePath()
	}

	// Load build.gradle to get project info
	if err := gf.loadBuildGradle(); err != nil {
		log.Warn("Failed to load build.gradle: " + err.Error())
		// Continue anyway - we can still parse dependencies
	}

	return gf, nil
}

// loadBuildGradle loads and parses basic info from build.gradle
func (gf *GradleFlexPack) loadBuildGradle() error {
	buildGradlePath := filepath.Join(gf.config.WorkingDirectory, "build.gradle")
	buildGradleKtsPath := filepath.Join(gf.config.WorkingDirectory, "build.gradle.kts")

	// Try build.gradle first, then build.gradle.kts
	var buildGradleData []byte
	var err error
	if _, statErr := os.Stat(buildGradlePath); statErr == nil {
		gf.buildGradlePath = buildGradlePath
		buildGradleData, err = os.ReadFile(buildGradlePath)
	} else if _, statErr := os.Stat(buildGradleKtsPath); statErr == nil {
		gf.buildGradlePath = buildGradleKtsPath
		buildGradleData, err = os.ReadFile(buildGradleKtsPath)
	} else {
		return fmt.Errorf("neither build.gradle nor build.gradle.kts found")
	}

	if err != nil {
		return fmt.Errorf("failed to read build.gradle: %w", err)
	}

	content := string(buildGradleData)

	// Extract group (groupId)
	groupMatch := regexp.MustCompile(`group\s*[=:]\s*['"]([^'"]+)['"]`).FindStringSubmatch(content)
	if len(groupMatch) > 1 {
		gf.groupId = groupMatch[1]
	} else {
		gf.groupId = "unspecified"
	}

	// Extract name (artifactId) - can be rootProject.name or just name
	nameMatch := regexp.MustCompile(`(?:rootProject\.)?name\s*[=:]\s*['"]([^'"]+)['"]`).FindStringSubmatch(content)
	if len(nameMatch) > 1 {
		gf.artifactId = nameMatch[1]
	} else {
		// Try to get from settings.gradle
		settingsPath := filepath.Join(gf.config.WorkingDirectory, "settings.gradle")
		if settingsData, err := os.ReadFile(settingsPath); err == nil {
			settingsContent := string(settingsData)
			rootProjectMatch := regexp.MustCompile(`rootProject\.name\s*[=:]\s*['"]([^'"]+)['"]`).FindStringSubmatch(settingsContent)
			if len(rootProjectMatch) > 1 {
				gf.artifactId = rootProjectMatch[1]
			} else {
				gf.artifactId = filepath.Base(gf.config.WorkingDirectory)
			}
		} else {
			gf.artifactId = filepath.Base(gf.config.WorkingDirectory)
		}
	}

	// Extract version
	versionMatch := regexp.MustCompile(`version\s*[=:]\s*['"]([^'"]+)['"]`).FindStringSubmatch(content)
	if len(versionMatch) > 1 {
		gf.projectVersion = versionMatch[1]
	} else {
		gf.projectVersion = "unspecified"
	}

	gf.projectName = fmt.Sprintf("%s:%s", gf.groupId, gf.artifactId)

	return nil
}

func (gf *GradleFlexPack) getGradleExecutablePath() string {
	// Check for Gradle wrapper first
	wrapperPath := filepath.Join(gf.config.WorkingDirectory, "gradlew")
	if _, err := os.Stat(wrapperPath); err == nil {
		return wrapperPath
	}

	// Check for Windows wrapper
	wrapperPathBat := filepath.Join(gf.config.WorkingDirectory, "gradlew.bat")
	if _, err := os.Stat(wrapperPathBat); err == nil {
		return wrapperPathBat
	}

	// Default to system Gradle
	gradleExec, err := exec.LookPath("gradle")
	if err != nil {
		log.Warn("Gradle executable not found in PATH, using 'gradle' as fallback")
		return "gradle"
	}

	return gradleExec
}

func (gf *GradleFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if len(gf.dependencies) == 0 {
		gf.parseDependencies()
	}
	requestedByMap := gf.CalculateRequestedBy()
	var dependencies []entities.Dependency
	for _, dep := range gf.dependencies {
		checksumMap := gf.calculateChecksumWithFallback(dep)

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

		if requesters, exists := requestedByMap[dep.ID]; exists && len(requesters) > 0 {
			entity.RequestedBy = [][]string{requesters}
		}
		dependencies = append(dependencies, entity)
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
			Name:    "Gradle",
			Version: gf.getGradleVersion(),
		},
		Modules: []entities.Module{},
	}

	module := entities.Module{
		Id:           fmt.Sprintf("%s:%s:%s", gf.groupId, gf.artifactId, gf.projectVersion),
		Type:         "gradle",
		Dependencies: dependencies,
	}
	buildInfo.Modules = append(buildInfo.Modules, module)

	return buildInfo, nil
}

func (gf *GradleFlexPack) parseDependencies() {
	if err := gf.parseWithGradleDependencies(); err == nil {
		return
	} else {
		log.Warn("Gradle dependencies parsing failed, falling back to build.gradle parsing: " + err.Error())
	}
	gf.parseFromBuildGradle()
}

func (gf *GradleFlexPack) parseWithGradleDependencies() error {
	configs := []string{"compileClasspath", "runtimeClasspath", "testCompileClasspath", "testRuntimeClasspath"}
	allDeps := make(map[string]DependencyInfo)

	for _, config := range configs {
		if !gf.config.IncludeTestDependencies && (config == "testCompileClasspath" || config == "testRuntimeClasspath") {
			continue
		}

		args := []string{"dependencies", "--configuration", config, "--quiet"}
		cmd := exec.Command(gf.config.GradleExecutable, args...)
		cmd.Dir = gf.config.WorkingDirectory

		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to get dependencies for configuration %s: %s", config, string(output)))
			continue
		}

		dependencies, err := gf.parseGradleDependencyTree(string(output))
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to parse text output for configuration %s: %s", config, err.Error()))
			continue
		}

		scopes := gf.mapGradleConfigurationToScopes(config)
		for _, dep := range dependencies {
			gf.processGradleDependency(dep, "", scopes, allDeps)
		}
	}

	// Convert map to slice
	for _, dep := range allDeps {
		gf.dependencies = append(gf.dependencies, dep)
	}

	log.Debug(fmt.Sprintf("Collected %d dependencies", len(gf.dependencies)))
	return nil
}

func (gf *GradleFlexPack) parseGradleDependencyTree(output string) ([]gradleDepNode, error) {
	lines := strings.Split(output, "\n")
	var roots []*gradleNodePtr
	var stack []*gradleNodePtr

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check for tree markers
		idx := strings.IndexAny(line, `+\`)
		if idx == -1 || !strings.Contains(line, "--- ") {
			continue
		}

		// Indent level: each level adds 5 chars "|    " or "     "
		depth := idx / 5

		// skip "--- "
		content := line[idx+4:]
		node := gf.parseGradleLine(content)
		if node == nil {
			continue
		}

		ptrNode := &gradleNodePtr{
			Group:   node.Group,
			Module:  node.Module,
			Version: node.Version,
			Type:    node.Type,
		}

		if depth == 0 {
			roots = append(roots, ptrNode)
			stack = make([]*gradleNodePtr, depth+1)
			stack[depth] = ptrNode
		} else {
			// Ensure stack is big enough
			if len(stack) <= depth {
				newStack := make([]*gradleNodePtr, depth+1)
				copy(newStack, stack)
				stack = newStack
			}
			parent := stack[depth-1]
			if parent != nil {
				parent.Children = append(parent.Children, ptrNode)
				stack[depth] = ptrNode
			} else {
				// Fallback for malformed trees or unexpected structure
				roots = append(roots, ptrNode)
				stack[depth] = ptrNode
			}
		}
	}
	return gf.convertNodes(roots), nil
}

// we will skip the inter-module/project dependencies for now
func (gf *GradleFlexPack) parseGradleLine(content string) *gradleDepNode {
	content = strings.TrimSpace(content)
	content = strings.TrimSuffix(content, " (*)")

	// Handle " -> version" resolution
	// Example: group:module:1.0 -> 1.1
	if strings.Contains(content, " -> ") {
		parts := strings.Split(content, " -> ")
		if len(parts) == 2 {
			original := parts[0]
			newVersion := parts[1]

			depParts := strings.Split(original, ":")
			if len(depParts) >= 2 {
				return &gradleDepNode{
					Group:   depParts[0],
					Module:  depParts[1],
					Version: newVersion,
					Type:    "jar",
				}
			}
		}
	}

	// Standard format: group:module:version
	parts := strings.Split(content, ":")
	if len(parts) >= 3 {
		return &gradleDepNode{
			Group:   parts[0],
			Module:  parts[1],
			Version: parts[2],
			Type:    "jar",
		}
	}

	// Project dependency: project :module
	if strings.HasPrefix(content, "project ") {
		// Extract module name from "project :path:to:module"
		path := strings.TrimPrefix(content, "project ")
		path = strings.TrimPrefix(path, ":")

		// Handle paths like "libs:mylib" -> module is "mylib"
		parts := strings.Split(path, ":")
		moduleName := parts[len(parts)-1]

		// Use the root project's group and version as defaults for the subproject
		// This assumes subprojects share the version/group of the root, which is common but not guaranteed.
		return &gradleDepNode{
			Group:   gf.groupId,
			Module:  moduleName,
			Version: gf.projectVersion,
			Type:    "jar",
		}
	}
	return nil
}

func (gf *GradleFlexPack) convertNodes(ptrNodes []*gradleNodePtr) []gradleDepNode {
	var nodes []gradleDepNode
	for _, ptr := range ptrNodes {
		node := gradleDepNode{
			Group:   ptr.Group,
			Module:  ptr.Module,
			Version: ptr.Version,
			Type:    ptr.Type,
		}
		node.Children = gf.convertNodes(ptr.Children)
		nodes = append(nodes, node)
	}
	return nodes
}

func (gf *GradleFlexPack) mapGradleConfigurationToScopes(config string) []string {
	configLower := strings.ToLower(config)

	switch {
	case strings.Contains(configLower, "compileclasspath") || strings.Contains(configLower, "compileonly") || configLower == "api" || configLower == "compile":
		return []string{"compile"}
	case strings.Contains(configLower, "runtimeclasspath") || strings.Contains(configLower, "runtimeonly") || configLower == "runtime":
		return []string{"runtime"}
	case strings.Contains(configLower, "testcompileclasspath") || strings.Contains(configLower, "testcompileonly") || strings.Contains(configLower, "testimplementation"):
		return []string{"test"}
	case strings.Contains(configLower, "testruntimeclasspath") || strings.Contains(configLower, "testruntimeonly"):
		return []string{"test"}
	case strings.Contains(configLower, "provided"):
		return []string{"provided"}
	default:
		return []string{"compile"}
	}
}

// processGradleDependency processes a Gradle dependency and its children recursively
func (gf *GradleFlexPack) processGradleDependency(dep gradleDepNode, parent string, scopes []string, allDeps map[string]DependencyInfo) {
	if dep.Group == "" || dep.Module == "" || dep.Version == "" {
		return
	}

	dependencyId := fmt.Sprintf("%s:%s:%s", dep.Group, dep.Module, dep.Version)

	// Skip if already processed
	if _, exists := allDeps[dependencyId]; exists {
		// Merge scopes if needed
		existingDep := allDeps[dependencyId]
		existingScopes := make(map[string]bool)
		for _, s := range existingDep.Scopes {
			existingScopes[s] = true
		}
		for _, s := range scopes {
			if !existingScopes[s] {
				existingDep.Scopes = append(existingDep.Scopes, s)
			}
		}
		allDeps[dependencyId] = existingDep
	} else {
		depType := "jar"
		if dep.Type != "" {
			depType = dep.Type
		}

		depInfo := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", dep.Group, dep.Module),
			Version: dep.Version,
			Type:    depType,
			Scopes:  scopes,
		}
		allDeps[dependencyId] = depInfo
	}

	// Build dependency graph
	if parent != "" {
		if gf.dependencyGraph[parent] == nil {
			gf.dependencyGraph[parent] = []string{}
		}
		gf.dependencyGraph[parent] = append(gf.dependencyGraph[parent], dependencyId)
	}

	// Process children recursively
	for _, child := range dep.Children {
		gf.processGradleDependency(child, dependencyId, scopes, allDeps)
	}
}

func (gf *GradleFlexPack) parseFromBuildGradle() {
	if gf.buildGradlePath == "" {
		return
	}

	data, err := os.ReadFile(gf.buildGradlePath)
	if err != nil {
		log.Warn("Failed to read build.gradle for dependency parsing: " + err.Error())
		return
	}

	content := string(data)

	// Extract dependencies block - this is a simplified parser
	// Match: dependencies { ... }
	depsBlockRegex := regexp.MustCompile(`dependencies\s*\{([^}]*(?:\{[^}]*\}[^}]*)*)\}`)
	depsBlock := depsBlockRegex.FindStringSubmatch(content)

	if len(depsBlock) < 2 {
		log.Debug("No dependencies block found in build.gradle")
		return
	}

	depsContent := depsBlock[1]

	// Match dependency declarations: implementation, compileOnly, testImplementation, etc.
	// Pattern: (implementation|compileOnly|testImplementation|...)('group:artifact:version')
	depRegex := regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime)\s*\(['"]([^'"]+)['"]\)`)
	matches := depRegex.FindAllStringSubmatch(depsContent, -1)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		configType := match[1]
		depString := match[2]

		// Parse dependency string (group:artifact:version)
		parts := strings.Split(depString, ":")
		if len(parts) < 3 {
			continue
		}

		groupId := parts[0]
		artifactId := parts[1]
		version := parts[2]

		dependencyId := fmt.Sprintf("%s:%s:%s", groupId, artifactId, version)
		scopes := gf.mapGradleConfigurationToScopes(configType)

		depInfo := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", groupId, artifactId),
			Version: version,
			Type:    "jar",
			Scopes:  scopes,
		}

		gf.dependencies = append(gf.dependencies, depInfo)
	}
}

// CalculateRequestedBy determines which dependencies requested a particular package
func (gf *GradleFlexPack) CalculateRequestedBy() map[string][]string {
	if len(gf.requestedByMap) == 0 {
		gf.buildRequestedByMap()
	}
	return gf.requestedByMap
}

// buildRequestedByMap builds the requested-by relationship map
func (gf *GradleFlexPack) buildRequestedByMap() {
	for parent, children := range gf.dependencyGraph {
		for _, child := range children {
			if gf.requestedByMap[child] == nil {
				gf.requestedByMap[child] = []string{}
			}
			gf.requestedByMap[child] = append(gf.requestedByMap[child], parent)
		}
	}
}

// calculateChecksumWithFallback calculates checksums with multiple fallback strategies
func (gf *GradleFlexPack) calculateChecksumWithFallback(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  gf.validateAndNormalizeScopes(dep.Scopes),
	}

	// Strategy 1: Try to find artifact in Gradle cache
	if artifactPath := gf.findGradleArtifact(dep); artifactPath != "" {
		if sha1, sha256, md5, err := gf.calculateFileChecksum(artifactPath); err == nil {
			checksumMap["sha1"] = sha1
			checksumMap["sha256"] = sha256
			checksumMap["md5"] = md5
			checksumMap["path"] = artifactPath
			return checksumMap
		}
		log.Debug(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
	}
	return checksumMap
}

// validateAndNormalizeScopes ensures scopes are valid and normalized
func (gf *GradleFlexPack) validateAndNormalizeScopes(scopes []string) []string {
	validScopes := map[string]bool{
		"compile":  true,
		"runtime":  true,
		"test":     true,
		"provided": true,
		"system":   true,
	}

	var normalized []string
	for _, scope := range scopes {
		if validScopes[scope] {
			normalized = append(normalized, scope)
		}
	}

	if len(normalized) == 0 {
		normalized = []string{"compile"}
	}
	return normalized
}

// findGradleArtifact locates a Gradle artifact in the local cache
func (gf *GradleFlexPack) findGradleArtifact(dep DependencyInfo) string {
	// Parse dependency name to get group and module
	parts := strings.Split(dep.Name, ":")
	if len(parts) != 2 {
		return ""
	}

	group := parts[0]
	module := parts[1]

	// Build path to Gradle cache
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Debug("Failed to get user home directory: " + err.Error())
		return ""
	}

	// Gradle cache structure: ~/.gradle/caches/modules-2/files-2.1/group/module/version/hash/filename
	cacheBase := filepath.Join(homeDir, ".gradle", "caches", "modules-2", "files-2.1")
	groupPath := strings.ReplaceAll(group, ".", string(filepath.Separator))
	modulePath := filepath.Join(cacheBase, groupPath, module, dep.Version)

	// Check if directory exists
	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		return ""
	}

	// Look for the artifact file in subdirectories (Gradle uses hash-based subdirectories)
	entries, err := os.ReadDir(modulePath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			hashDir := filepath.Join(modulePath, entry.Name())
			jarFile := filepath.Join(hashDir, fmt.Sprintf("%s-%s.%s", module, dep.Version, dep.Type))
			if _, err := os.Stat(jarFile); err == nil {
				return jarFile
			}
			jarFileAlt := filepath.Join(hashDir, fmt.Sprintf("%s.%s", module, dep.Type))
			if _, err := os.Stat(jarFileAlt); err == nil {
				return jarFileAlt
			}
		}
	}
	return ""
}

// calculateFileChecksum calculates checksums for a file
func (gf *GradleFlexPack) calculateFileChecksum(filePath string) (string, string, string, error) {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return "", "", "", err
	}

	if fileDetails == nil {
		return "", "", "", fmt.Errorf("fileDetails is nil for file: %s", filePath)
	}

	return fileDetails.Checksum.Sha1,
		fileDetails.Checksum.Sha256,
		fileDetails.Checksum.Md5,
		nil
}

// getGradleVersion gets the Gradle version
func (gf *GradleFlexPack) getGradleVersion() string {
	cmd := exec.Command(gf.config.GradleExecutable, "--version")
	cmd.Dir = gf.config.WorkingDirectory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown"
	}

	// Parse version from output: "Gradle 7.5.1"
	versionRegex := regexp.MustCompile(`Gradle\s+(\d+\.\d+(?:\.\d+)?)`)
	matches := versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		return matches[1]
	}

	return "unknown"
}
