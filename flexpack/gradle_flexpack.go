package flexpack

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

var agentVersion = "1.0.0"

const defaultGradleCommandTimeout = 1 * time.Minute

//go:embed init-artifact-extractor.gradle
var initScriptContent string

var (
	groupRegex         = regexp.MustCompile(`group\s*[=:]\s*['"]([^'"]+)['"]`)
	nameRegex          = regexp.MustCompile(`(?:rootProject\.)?name\s*[=:]\s*['"]([^'"]+)['"]`)
	versionRegex       = regexp.MustCompile(`version\s*[=:]\s*['"]([^'"]+)['"]`)
	rootProjectRegex   = regexp.MustCompile(`rootProject\.name\s*[=:]\s*['"]([^'"]+)['"]`)
	includeRegex       = regexp.MustCompile(`['"]([^'"]+)['"]`)
	depRegex           = regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime|annotationProcessor|kapt|ksp)\s*[\(\s]['"]([^'"]+)['"]`)
	depMapRegex        = regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime|annotationProcessor|kapt|ksp)\s*(?:\(|\s)\s*group\s*[:=]\s*['"]([^'"]+)['"]\s*,\s*name\s*[:=]\s*['"]([^'"]+)['"](?:,\s*version\s*[:=]\s*['"]([^'"]+)['"])?`)
	depProjectRegex    = regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime|annotationProcessor|kapt|ksp)\s*(?:\(|\s)\s*project\s*(?:(?:\(\s*path\s*:\s*)|\(?\s*)['"]([^'"]+)['"]`)
	gradleVersionRegex = regexp.MustCompile(`Gradle\s+(\d+\.\d+(?:\.\d+)?)`)
)

type deployedArtifactJSON struct {
	ModuleName string `json:"module_name"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Sha1       string `json:"sha1"`
	Sha256     string `json:"sha256"`
	Md5        string `json:"md5"`
}

type moduleMetadata struct {
	Group    string
	Artifact string
	Version  string
}

type GradleFlexPack struct {
	config            GradleConfig
	ctx               context.Context
	dependencies      []DependencyInfo
	dependencyGraph   map[string][]string
	projectName       string
	projectVersion    string
	groupId           string
	artifactId        string
	requestedByMap    map[string][]string
	buildGradlePath   string
	WasPublishCommand bool
	modulesMap        map[string]moduleMetadata
	modulesList       []string
	deployedArtifacts map[string][]entities.Artifact
}

type gradleDepNode struct {
	Group      string
	Module     string
	Version    string
	Classifier string
	Type       string
	Reason     string
	Children   []gradleDepNode
}

type gradleNodePtr struct {
	Group      string
	Module     string
	Version    string
	Classifier string
	Type       string
	Children   []*gradleNodePtr
}

func NewGradleFlexPack(config GradleConfig) (*GradleFlexPack, error) {
	return NewGradleFlexPackWithContext(context.Background(), config)
}

func NewGradleFlexPackWithContext(ctx context.Context, config GradleConfig) (*GradleFlexPack, error) {
	if config.WorkingDirectory == "" {
		return nil, fmt.Errorf("working directory cannot be empty")
	}
	if _, err := os.Stat(config.WorkingDirectory); os.IsNotExist(err) {
		return nil, fmt.Errorf("working directory does not exist: %s", config.WorkingDirectory)
	}

	if config.CommandTimeout == 0 {
		config.CommandTimeout = defaultGradleCommandTimeout
	}

	gf := &GradleFlexPack{
		config:          config,
		ctx:             ctx,
		dependencies:    []DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}

	if gf.config.GradleExecutable == "" {
		gf.config.GradleExecutable = gf.getGradleExecutablePath()
	}

	if err := gf.loadBuildGradle(); err != nil {
		log.Warn("Failed to load build.gradle: " + err.Error())
	}
	gf.scanAllModules()
	return gf, nil
}

func (gf *GradleFlexPack) getGradleExecutablePath() string {
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

func (gf *GradleFlexPack) loadBuildGradle() error {
	buildGradlePath := filepath.Join(gf.config.WorkingDirectory, "build.gradle")
	buildGradleKtsPath := filepath.Join(gf.config.WorkingDirectory, "build.gradle.kts")

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
	gf.groupId, gf.artifactId, gf.projectVersion = gf.parseBuildGradleMetadata(content)

	// Refine artifactId if needed (from settings)
	if gf.artifactId == "" || gf.artifactId == "unspecified" {
		settingsPath := filepath.Join(gf.config.WorkingDirectory, "settings.gradle")
		if settingsData, err := os.ReadFile(settingsPath); err == nil {
			settingsContent := string(settingsData)
			rootProjectMatch := rootProjectRegex.FindStringSubmatch(settingsContent)
			if len(rootProjectMatch) > 1 {
				gf.artifactId = rootProjectMatch[1]
			} else {
				gf.artifactId = filepath.Base(gf.config.WorkingDirectory)
			}
		} else {
			gf.artifactId = filepath.Base(gf.config.WorkingDirectory)
		}
	}
	gf.projectName = fmt.Sprintf("%s:%s", gf.groupId, gf.artifactId)
	return nil
}

func (gf *GradleFlexPack) parseBuildGradleMetadata(content string) (groupId, artifactId, version string) {
	// Extract group (groupId)
	groupMatch := groupRegex.FindStringSubmatch(content)
	if len(groupMatch) > 1 {
		groupId = groupMatch[1]
	} else {
		groupId = "unspecified"
	}

	// Extract name (artifactId) - can be rootProject.name or just name
	nameMatch := nameRegex.FindStringSubmatch(content)
	if len(nameMatch) > 1 {
		artifactId = nameMatch[1]
	}

	// Extract version
	versionMatch := versionRegex.FindStringSubmatch(content)
	if len(versionMatch) > 1 {
		version = versionMatch[1]
	} else {
		version = "unspecified"
	}
	return
}

func (gf *GradleFlexPack) scanAllModules() {
	gf.modulesMap = make(map[string]moduleMetadata)

	modules, err := gf.getModules()
	if err != nil {
		log.Warn("Failed to get modules list from settings.gradle: " + err.Error())
		// Fallback: at least add the root module
		gf.modulesMap[""] = moduleMetadata{
			Group:    gf.groupId,
			Artifact: gf.artifactId,
			Version:  gf.projectVersion,
		}
		gf.modulesList = []string{""}
		return
	}

	gf.modulesList = modules
	for _, moduleName := range modules {
		g, a, v := gf.getModuleMetadata(moduleName)
		finalGroup := gf.groupId
		if g != "" && g != "unspecified" {
			finalGroup = g
		}

		finalVersion := gf.projectVersion
		if v != "" && v != "unspecified" {
			finalVersion = v
		}

		finalArtifact := gf.artifactId
		if a != "" {
			finalArtifact = a
		} else if moduleName != "" {
			parts := strings.Split(moduleName, ":")
			finalArtifact = parts[len(parts)-1]
		}

		gf.modulesMap[moduleName] = moduleMetadata{
			Group:    finalGroup,
			Artifact: finalArtifact,
			Version:  finalVersion,
		}
	}
}

func (gf *GradleFlexPack) getModules() ([]string, error) {
	settingsPath := filepath.Join(gf.config.WorkingDirectory, "settings.gradle")
	settingsKtsPath := filepath.Join(gf.config.WorkingDirectory, "settings.gradle.kts")

	var content []byte
	var err error
	if _, err = os.Stat(settingsPath); err == nil {
		content, err = os.ReadFile(settingsPath)
	} else if _, err = os.Stat(settingsKtsPath); err == nil {
		content, err = os.ReadFile(settingsKtsPath)
	} else {
		// If no settings file, assume single root module
		return []string{""}, nil
	}

	if err != nil {
		return []string{""}, err
	}

	var modules []string
	// Root module
	modules = append(modules, "")
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "include") && !strings.HasPrefix(trimmed, "includeBuild") {
			matches := includeRegex.FindAllStringSubmatch(trimmed, -1)
			for _, match := range matches {
				if len(match) > 1 {
					// Clean up potential leading colons sometimes used in include ':app'
					moduleName := strings.TrimPrefix(match[1], ":")
					if moduleName != "" {
						modules = append(modules, moduleName)
					}
				}
			}
		}
	}
	return modules, nil
}

// supports minimum version 5.0 for gradle artifacts extractor compatibility
func (gf *GradleFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	buildInfo := &entities.BuildInfo{
		Name:    buildName,
		Number:  buildNumber,
		Started: time.Now().Format(entities.TimeFormat),
		Agent: &entities.Agent{
			Name:    "build-info-go",
			Version: agentVersion,
		},
		BuildAgent: &entities.Agent{
			Name:    "Gradle",
			Version: gf.getGradleVersion(),
		},
		Modules: []entities.Module{},
	}

	if gf.WasPublishCommand {
		if artifacts, err := gf.getGradleDeployedArtifacts(); err == nil {
			gf.deployedArtifacts = artifacts
		} else {
			log.Warn("Failed to get deployed artifacts: " + err.Error())
		}
	}

	for _, moduleName := range gf.modulesList {
		if gf.ctx.Err() != nil {
			return nil, fmt.Errorf("context cancelled: %w", gf.ctx.Err())
		}
		module, err := gf.processModule(moduleName)
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to process module %s: %s", moduleName, err.Error()))
			continue
		}
		if artifacts, ok := gf.deployedArtifacts[moduleName]; ok {
			module.Artifacts = artifacts
		}
		buildInfo.Modules = append(buildInfo.Modules, module)
	}
	return buildInfo, nil
}

func (gf *GradleFlexPack) getGradleVersion() string {
	output, err := gf.runGradleCommand("--version")
	if err != nil {
		log.Debug("Failed to get Gradle version: " + err.Error())
		return "unknown"
	}
	matches := gradleVersionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		version := matches[1]
		if !gf.isGradleVersionCompatible(version) {
			log.Warn(fmt.Sprintf("Gradle version %s may not be fully compatible; minimum recommended is 6.0", version))
		}
		return version
	}
	return "unknown"
}

func (gf *GradleFlexPack) runGradleCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(gf.ctx, gf.config.CommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, gf.config.GradleExecutable, args...)
	cmd.Dir = gf.config.WorkingDirectory

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("gradle command timed out after %v: %s", gf.config.CommandTimeout, strings.Join(args, " "))
	}
	if err != nil {
		return output, fmt.Errorf("gradle command failed: %w", err)
	}
	return output, nil
}

func (gf *GradleFlexPack) isGradleVersionCompatible(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 1 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Debug("Failed to parse Gradle major version: " + err.Error())
		return false
	}
	return major >= 5
}

func (gf *GradleFlexPack) getGradleDeployedArtifacts() (map[string][]entities.Artifact, error) {
	initScriptFile, err := os.CreateTemp("", "init-artifact-extractor-*.gradle")
	if err != nil {
		return nil, fmt.Errorf("failed to create init script: %w", err)
	}
	initScriptPath := initScriptFile.Name()
	defer initScriptFile.Close()
	defer func() {
		if err := os.Remove(initScriptPath); err != nil {
			log.Debug("Failed to remove init script: " + err.Error())
		}
	}()

	if _, err := initScriptFile.WriteString(initScriptContent); err != nil {
		return nil, fmt.Errorf("failed to write init script: %w", err)
	}

	tasks := []string{"publishToMavenLocal", "generateCiManifest", "-I", initScriptPath}
	if output, err := gf.runGradleCommand(tasks...); err != nil {
		return nil, fmt.Errorf("gradle command failed: %s - %w", string(output), err)
	}

	// Read manifest
	manifestPath := filepath.Join(gf.config.WorkingDirectory, "build", "ci-artifacts-manifest.json")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	// Delete manifest file
	if err := os.Remove(manifestPath); err != nil {
		log.Warn("Failed to delete manifest file: " + err.Error())
	}

	var artifacts []deployedArtifactJSON
	if err := json.Unmarshal(content, &artifacts); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	result := make(map[string][]entities.Artifact)
	for _, art := range artifacts {
		// Normalize module name (strip leading colon from :app)
		moduleName := strings.TrimPrefix(art.ModuleName, ":")
		// Handle root project if it returns just "::"
		if moduleName == ":" {
			moduleName = ""
		}

		entityArtifact := entities.Artifact{
			Name: art.Name,
			Type: art.Type,
			Path: art.Path,
			Checksum: entities.Checksum{
				Sha1:   art.Sha1,
				Sha256: art.Sha256,
				Md5:    art.Md5,
			},
		}
		result[moduleName] = append(result[moduleName], entityArtifact)
	}
	return result, nil
}

// We might need to process modules parallelly for better performance
func (gf *GradleFlexPack) processModule(moduleName string) (entities.Module, error) {
	// Determine module identity early using pre-calculated map
	groupId := gf.groupId
	version := gf.projectVersion
	artifactId := gf.artifactId

	if meta, ok := gf.modulesMap[moduleName]; ok {
		groupId = meta.Group
		version = meta.Version
		artifactId = meta.Artifact
	}

	gf.dependencies = []DependencyInfo{}
	gf.dependencyGraph = make(map[string][]string)
	gf.requestedByMap = make(map[string][]string)

	gf.parseModuleDependencies(moduleName)
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

			if checksum.Sha1 == "" && checksum.Sha256 == "" {
				log.Warn(fmt.Sprintf("Skipping dependency %s: checksums map existed but contained no hashes", dep.ID))
				continue
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

	return entities.Module{
		Id:           fmt.Sprintf("%s:%s:%s", groupId, artifactId, version),
		Type:         entities.Gradle,
		Dependencies: dependencies,
	}, nil
}

func (gf *GradleFlexPack) parseModuleDependencies(moduleName string) {
	if err := gf.parseWithGradleDependencies(moduleName); err == nil {
		return
	} else {
		log.Warn(fmt.Sprintf("Gradle dependencies parsing failed for module %s, falling back to build.gradle parsing: %s", moduleName, err.Error()))
	}
	gf.parseFromBuildGradle(moduleName)
}

func (gf *GradleFlexPack) parseWithGradleDependencies(moduleName string) error {
	configs := []string{"compileClasspath", "runtimeClasspath", "testCompileClasspath", "testRuntimeClasspath"}
	allDeps := make(map[string]DependencyInfo)

	for _, config := range configs {
		if !gf.config.IncludeTestDependencies && (config == "testCompileClasspath" || config == "testRuntimeClasspath") {
			continue
		}

		taskPrefix := ""
		if moduleName != "" {
			taskPrefix = ":" + moduleName + ":"
		}
		args := []string{taskPrefix + "dependencies", "--configuration", config, "--quiet"}
		output, err := gf.runGradleCommand(args...)
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
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		depth, content := gf.extractDepthAndContent(line)
		if content == "" {
			continue
		}

		node := gf.parseGradleLine(content)
		if node == nil {
			continue
		}

		ptrNode := &gradleNodePtr{
			Group:      node.Group,
			Module:     node.Module,
			Version:    node.Version,
			Classifier: node.Classifier,
			Type:       node.Type,
		}

		if depth == 0 {
			roots = append(roots, ptrNode)
			stack = []*gradleNodePtr{ptrNode}
		} else {
			parent := gf.findParentForDepth(stack, depth)
			if parent != nil {
				parent.Children = append(parent.Children, ptrNode)
			} else {
				// Fallback: treat as root
				log.Debug(fmt.Sprintf("No parent found for dependency at depth %d: %s", depth, content))
				roots = append(roots, ptrNode)
			}
			// Ensure stack size
			if len(stack) <= depth {
				stack = append(stack, make([]*gradleNodePtr, depth-len(stack)+1)...)
			}
			stack[depth] = ptrNode
		}
	}
	return gf.convertNodes(roots), nil
}

func (gf *GradleFlexPack) extractDepthAndContent(line string) (int, string) {
	markerIdx := strings.Index(line, "+--- ")
	if markerIdx == -1 {
		markerIdx = strings.Index(line, "\\--- ")
	}
	if markerIdx == -1 {
		return 0, ""
	}
	depth := gf.calculateTreeDepth(line)
	content := line[markerIdx+5:]
	return depth, content
}

func (gf *GradleFlexPack) calculateTreeDepth(line string) int {
	depth := 0
	i := 0
	for i < len(line) {
		remaining := line[i:]
		if strings.HasPrefix(remaining, "|    ") || strings.HasPrefix(remaining, "     ") {
			depth++
			i += 5
			continue
		}

		if strings.HasPrefix(remaining, "+--- ") || strings.HasPrefix(remaining, "\\--- ") {
			break
		}
		break
	}
	return depth
}

func (gf *GradleFlexPack) parseGradleLine(content string) *gradleDepNode {
	content = strings.TrimSpace(content)
	// Remove constraint markers like (*), (c), (n)
	content = strings.TrimSuffix(content, " (*)")
	content = strings.TrimSuffix(content, " (c)")
	content = strings.TrimSuffix(content, " (n)")

	// Extract type if specified with @type suffix (e.g., @aar, @jar)
	depType := "jar"
	if atIdx := strings.LastIndex(content, "@"); atIdx != -1 {
		depType = content[atIdx+1:]
		content = content[:atIdx]
	}

	// Handle " -> version" resolution (version conflict resolution)
	// Example: group:module:1.0 -> 1.1 or group:module -> 1.1
	var resolvedVersion string
	if arrowIdx := strings.Index(content, " -> "); arrowIdx != -1 {
		resolvedVersion = strings.TrimSpace(content[arrowIdx+4:])
		content = strings.TrimSpace(content[:arrowIdx])
	}

	// Project dependency: project :module
	if strings.HasPrefix(content, "project ") {
		// Extract module name from "project :path:to:module"
		path := strings.TrimPrefix(content, "project ")
		path = strings.TrimPrefix(path, ":")

		// Handle paths like "libs:mylib" -> module is "mylib"
		pathParts := strings.Split(path, ":")
		moduleName := pathParts[len(pathParts)-1]

		group := gf.groupId
		version := gf.projectVersion

		if meta, ok := gf.modulesMap[path]; ok {
			group = meta.Group
			version = meta.Version
			moduleName = meta.Artifact
		}

		return &gradleDepNode{
			Group:   group,
			Module:  moduleName,
			Version: version,
			Type:    depType,
		}
	}

	// format: group:module:version[:classifier]
	parts := strings.Split(content, ":")
	if len(parts) < 2 {
		return nil
	}

	node := &gradleDepNode{
		Group:  parts[0],
		Module: parts[1],
		Type:   depType,
	}

	switch len(parts) {
	case 2:
		if resolvedVersion != "" {
			node.Version = resolvedVersion
		} else {
			return nil
		}
	case 3:
		node.Version = parts[2]
	case 4:
		node.Version = parts[2]
		node.Classifier = parts[3]
	default:
		node.Version = parts[2]
		node.Classifier = parts[3]
	}

	if resolvedVersion != "" {
		node.Version = resolvedVersion
	}
	return node
}

func (gf *GradleFlexPack) findParentForDepth(stack []*gradleNodePtr, depth int) *gradleNodePtr {
	for parentDepth := depth - 1; parentDepth >= 0; parentDepth-- {
		if parentDepth < len(stack) && stack[parentDepth] != nil {
			return stack[parentDepth]
		}
	}
	return nil
}

func (gf *GradleFlexPack) convertNodes(ptrNodes []*gradleNodePtr) []gradleDepNode {
	var nodes []gradleDepNode
	for _, ptr := range ptrNodes {
		node := gradleDepNode{
			Group:      ptr.Group,
			Module:     ptr.Module,
			Version:    ptr.Version,
			Classifier: ptr.Classifier,
			Type:       ptr.Type,
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

func (gf *GradleFlexPack) processGradleDependency(dep gradleDepNode, parent string, scopes []string, allDeps map[string]DependencyInfo) {
	if dep.Group == "" || dep.Module == "" || dep.Version == "" {
		return
	}

	var dependencyId string
	if dep.Classifier != "" {
		dependencyId = fmt.Sprintf("%s:%s:%s:%s", dep.Group, dep.Module, dep.Version, dep.Classifier)
	} else {
		dependencyId = fmt.Sprintf("%s:%s:%s", dep.Group, dep.Module, dep.Version)
	}

	if _, exists := allDeps[dependencyId]; exists {
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
	for _, child := range dep.Children {
		gf.processGradleDependency(child, dependencyId, scopes, allDeps)
	}
}

func (gf *GradleFlexPack) parseFromBuildGradle(moduleName string) {
	path := gf.buildGradlePath
	if moduleName != "" {
		// moduleName is "a:b" -> "a/b"
		subPath := strings.ReplaceAll(moduleName, ":", string(filepath.Separator))

		buildGradlePath := filepath.Join(gf.config.WorkingDirectory, subPath, "build.gradle")
		buildGradleKtsPath := filepath.Join(gf.config.WorkingDirectory, subPath, "build.gradle.kts")

		if !gf.validatePathWithinWorkingDir(buildGradlePath) {
			log.Debug(fmt.Sprintf("Path traversal attempt detected for module %s (build.gradle)", moduleName))
			return
		}
		if !gf.validatePathWithinWorkingDir(buildGradleKtsPath) {
			log.Debug(fmt.Sprintf("Path traversal attempt detected for module %s (build.gradle.kts)", moduleName))
			return
		}

		if _, err := os.Stat(buildGradlePath); err == nil {
			path = buildGradlePath
		} else if _, err := os.Stat(buildGradleKtsPath); err == nil {
			path = buildGradleKtsPath
		} else {
			log.Warn("Could not find build.gradle or build.gradle.kts for module " + moduleName)
			return
		}
	} else if gf.buildGradlePath == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn("Failed to read build.gradle for dependency parsing: " + err.Error())
		return
	}

	content := string(data)
	depsContent := gf.extractDependenciesBlock(content)
	if depsContent == "" {
		log.Debug("No dependencies block found in build.gradle")
		return
	}
	allDeps := make(map[string]DependencyInfo)

	// 1. String notation: "group:artifact:version"
	matches := depRegex.FindAllStringSubmatch(depsContent, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		configType := match[1]
		depString := match[2]

		// Parse dependency string (group:artifact:version[:classifier])
		parts := strings.Split(depString, ":")
		if len(parts) < 3 {
			continue
		}

		groupId := parts[0]
		artifactId := parts[1]
		version := parts[2]
		classifier := ""
		if len(parts) >= 4 {
			classifier = parts[3]
		}

		gf.addDependency(configType, groupId, artifactId, version, classifier, allDeps)
	}

	// 2. Map notation: group: '...', name: '...', version: '...'
	mapMatches := depMapRegex.FindAllStringSubmatch(depsContent, -1)
	for _, match := range mapMatches {
		if len(match) < 4 {
			continue
		}
		configType := match[1]
		groupId := match[2]
		artifactId := match[3]
		version := ""
		if len(match) >= 5 {
			version = match[4]
		}
		gf.addDependency(configType, groupId, artifactId, version, "", allDeps)
	}

	// 3. Project notation: project(':path')
	projMatches := depProjectRegex.FindAllStringSubmatch(depsContent, -1)
	for _, match := range projMatches {
		if len(match) < 3 {
			continue
		}
		configType := match[1]
		// e.g. :utils or :libs:utils
		projectPath := match[2]
		projectPath = strings.TrimPrefix(projectPath, ":")

		// Default to project structure if not found
		parts := strings.Split(projectPath, ":")
		artifactId := parts[len(parts)-1]
		groupId := gf.groupId
		version := gf.projectVersion

		if meta, ok := gf.modulesMap[projectPath]; ok {
			groupId = meta.Group
			version = meta.Version
			artifactId = meta.Artifact
		}

		gf.addDependency(configType, groupId, artifactId, version, "", allDeps)
	}
	for _, dep := range allDeps {
		gf.dependencies = append(gf.dependencies, dep)
	}
}

func (gf *GradleFlexPack) validatePathWithinWorkingDir(resolvedPath string) bool {
	cleanWorkingDir := filepath.Clean(gf.config.WorkingDirectory)
	cleanResolvedPath := filepath.Clean(resolvedPath)

	absWorkingDir, err := filepath.Abs(cleanWorkingDir)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get absolute path for working directory: %s", err.Error()))
		return false
	}
	absResolvedPath, err := filepath.Abs(cleanResolvedPath)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get absolute path for resolved path: %s", err.Error()))
		return false
	}
	absWorkingDir = filepath.Clean(absWorkingDir)
	absResolvedPath = filepath.Clean(absResolvedPath)
	if absResolvedPath == absWorkingDir {
		return true
	}
	separator := string(filepath.Separator)
	expectedPrefix := absWorkingDir + separator
	if !strings.HasPrefix(absResolvedPath, expectedPrefix) {
		return false
	}

	return true
}

func (gf *GradleFlexPack) extractDependenciesBlock(content string) string {
	// Find "dependencies {" or "dependencies{" pattern
	idx := strings.Index(content, "dependencies")
	if idx == -1 {
		return ""
	}
	remaining := content[idx+len("dependencies"):]
	braceIdx := strings.Index(remaining, "{")
	if braceIdx == -1 {
		return ""
	}

	start := idx + len("dependencies") + braceIdx + 1
	if start >= len(content) {
		return ""
	}

	braceCount := 1
	end := start
	inLineComment := false
	inBlockComment := false
	inString := false
	stringChar := byte(0)

	for i := start; i < len(content) && braceCount > 0; i++ {
		char := content[i]

		if inLineComment {
			if char == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if char == '*' && i+1 < len(content) && content[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			if char == '\\' {
				i++
				continue
			}
			if char == stringChar {
				inString = false
			}
			continue
		}

		if char == '/' {
			if i+1 < len(content) {
				if content[i+1] == '/' {
					inLineComment = true
					i++
					continue
				} else if content[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}
		}

		if char == '"' || char == '\'' {
			inString = true
			stringChar = char
			continue
		}

		switch char {
		case '{':
			braceCount++
		case '}':
			braceCount--
		}
		end = i
	}

	if braceCount != 0 {
		log.Debug("Unbalanced braces in dependencies block")
		return ""
	}
	return content[start:end]
}

func (gf *GradleFlexPack) addDependency(configType, groupId, artifactId, version, classifier string, allDeps map[string]DependencyInfo) {
	var dependencyId string
	if classifier != "" {
		dependencyId = fmt.Sprintf("%s:%s:%s:%s", groupId, artifactId, version, classifier)
	} else {
		dependencyId = fmt.Sprintf("%s:%s:%s", groupId, artifactId, version)
	}
	scopes := gf.mapGradleConfigurationToScopes(configType)

	if existing, ok := allDeps[dependencyId]; ok {
		existingScopes := make(map[string]bool)
		for _, s := range existing.Scopes {
			existingScopes[s] = true
		}
		for _, s := range scopes {
			if !existingScopes[s] {
				existing.Scopes = append(existing.Scopes, s)
			}
		}
		allDeps[dependencyId] = existing
	} else {
		depInfo := DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", groupId, artifactId),
			Version: version,
			Type:    "jar",
			Scopes:  scopes,
		}
		allDeps[dependencyId] = depInfo
	}
}

func (gf *GradleFlexPack) CalculateRequestedBy() map[string][]string {
	if len(gf.requestedByMap) == 0 {
		gf.buildRequestedByMap()
	}
	return gf.requestedByMap
}

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

// We will only try to find in local build only if it is a publish command
func (gf *GradleFlexPack) calculateChecksumWithFallback(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  gf.validateAndNormalizeScopes(dep.Scopes),
	}

	// 1. Try to find in deployed artifacts (local build)
	if len(gf.deployedArtifacts) > 0 {
		parts := strings.Split(dep.Name, ":")
		if len(parts) == 2 {
			artifactId := parts[1]
			expectedName := fmt.Sprintf("%s-%s.%s", artifactId, dep.Version, dep.Type)
			for _, artifacts := range gf.deployedArtifacts {
				for _, art := range artifacts {
					if art.Name == expectedName {
						checksumMap["sha1"] = art.Sha1
						checksumMap["sha256"] = art.Sha256
						checksumMap["md5"] = art.Md5
						checksumMap["path"] = art.Path
						return checksumMap
					}
				}
			}
		}
	}

	// 2. Fallback to Gradle cache
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

	// Failed to find checksums
	return nil
}

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

func (gf *GradleFlexPack) findGradleArtifact(dep DependencyInfo) string {
	parts := strings.Split(dep.Name, ":")
	if len(parts) != 2 {
		return ""
	}

	group := parts[0]
	module := parts[1]

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Debug("Failed to get user home directory: " + err.Error())
		return ""
	}
	gradleUserHome := os.Getenv("GRADLE_USER_HOME")
	if gradleUserHome == "" {
		gradleUserHome = filepath.Join(homeDir, ".gradle")
	}

	// Gradle cache structure: ~/.gradle/caches/modules-2/files-2.1/group/module/version/hash/filename
	cacheBase := filepath.Join(gradleUserHome, "caches", "modules-2", "files-2.1")
	modulePath := filepath.Join(cacheBase, group, module, dep.Version)

	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		return ""
	}
	entries, err := os.ReadDir(modulePath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			hashDir := filepath.Join(modulePath, entry.Name())
			//module-version.type
			jarFile := filepath.Join(hashDir, fmt.Sprintf("%s-%s.%s", module, dep.Version, dep.Type))
			if _, err := os.Stat(jarFile); err == nil {
				return jarFile
			}
			// module.type
			jarFileAlt := filepath.Join(hashDir, fmt.Sprintf("%s.%s", module, dep.Type))
			if _, err := os.Stat(jarFileAlt); err == nil {
				return jarFileAlt
			}
		}
	}
	return ""
}

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

func (gf *GradleFlexPack) getModuleMetadata(moduleName string) (groupId, artifactId, version string) {
	subPath := strings.ReplaceAll(moduleName, ":", string(filepath.Separator))
	buildGradlePath := filepath.Join(gf.config.WorkingDirectory, subPath, "build.gradle")
	buildGradleKtsPath := filepath.Join(gf.config.WorkingDirectory, subPath, "build.gradle.kts")

	if !gf.validatePathWithinWorkingDir(buildGradlePath) {
		log.Debug(fmt.Sprintf("Path traversal attempt detected for module %s (build.gradle)", moduleName))
		return "", "", ""
	}
	if !gf.validatePathWithinWorkingDir(buildGradleKtsPath) {
		log.Debug(fmt.Sprintf("Path traversal attempt detected for module %s (build.gradle.kts)", moduleName))
		return "", "", ""
	}

	var content []byte
	var err error
	if _, statErr := os.Stat(buildGradlePath); statErr == nil {
		content, err = os.ReadFile(buildGradlePath)
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to read build.gradle for module %s: %s", moduleName, err.Error()))
			return "", "", ""
		}
	} else if _, statErr := os.Stat(buildGradleKtsPath); statErr == nil {
		content, err = os.ReadFile(buildGradleKtsPath)
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to read build.gradle.kts for module %s: %s", moduleName, err.Error()))
			return "", "", ""
		}
	} else {
		return "", "", ""
	}
	return gf.parseBuildGradleMetadata(string(content))
}
