package flexpack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

var agentVersion = "1.0.0"

const defaultGradleCommandTimeout = 5 * time.Minute

type moduleMetadata struct {
	Group    string
	Artifact string
	Version  string
}

type GradleFlexPack struct {
	config            flexpack.GradleConfig
	ctx               context.Context
	projectName       string
	projectVersion    string
	groupId           string
	artifactId        string
	buildGradlePath   string
	wasPublishCommand bool
	modulesMap        map[string]moduleMetadata
	modulesList       []string
	deployedArtifacts map[string][]entities.Artifact
}

func NewGradleFlexPack(config flexpack.GradleConfig) (*GradleFlexPack, error) {
	return NewGradleFlexPackWithContext(context.Background(), config)
}

func NewGradleFlexPackWithContext(ctx context.Context, config flexpack.GradleConfig) (*GradleFlexPack, error) {
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
		config: config,
		ctx:    ctx,
	}

	if gf.config.GradleExecutable == "" {
		execPath, err := GetGradleExecutablePath(gf.config.WorkingDirectory)
		if err != nil {
			log.Warn(fmt.Sprintf("Gradle executable not found (checked wrapper in %s, PATH=%s); using 'gradle' as fallback", gf.config.WorkingDirectory, os.Getenv("PATH")))
			gf.config.GradleExecutable = "gradle"
		} else {
			gf.config.GradleExecutable = execPath
		}
	}

	if err := gf.loadBuildGradle(); err != nil {
		return nil, fmt.Errorf("failed to load build.gradle")
	}
	gf.scanAllModules()
	return gf, nil
}

func (gf *GradleFlexPack) SetWasPublishCommand(wasPublish bool) {
	gf.wasPublishCommand = wasPublish
}

func (gf *GradleFlexPack) loadBuildGradle() error {
	buildGradleData, path, err := gf.getBuildFileContent("")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Debug("build.gradle not found, continuing without metadata")
			return nil
		}
		return fmt.Errorf("failed to read build.gradle: %w", err)
	}
	gf.buildGradlePath = path
	content := string(buildGradleData)
	gf.groupId, gf.artifactId, gf.projectVersion = gf.parseBuildGradleMetadata(content)

	// Always check settings.gradle for rootProject.name as authoritative source
	settingsContent, err := gf.readSettingsFile()
	if err != nil {
		log.Debug("Failed to read settings file: " + err.Error())
	}

	if settingsContent != "" {
		rootProjectMatch := nameRegex.FindStringSubmatch(settingsContent)
		if len(rootProjectMatch) > 1 {
			// rootProject.name is the authoritative source for artifact ID
			gf.artifactId = rootProjectMatch[1]
		} else if gf.artifactId == "" || gf.artifactId == "unspecified" {
			// Only fall back to directory name if no explicit name found
			gf.artifactId = filepath.Base(gf.config.WorkingDirectory)
		}
	} else if gf.artifactId == "" || gf.artifactId == "unspecified" {
		// No settings file and no valid artifact ID from build.gradle
		gf.artifactId = filepath.Base(gf.config.WorkingDirectory)
	}

	gf.projectName = fmt.Sprintf("%s:%s", gf.groupId, gf.artifactId)
	return nil
}

func (gf *GradleFlexPack) scanAllModules() {
	gf.modulesMap = make(map[string]moduleMetadata)

	modules, err := gf.getModules()
	if err != nil {
		log.Warn("failed to get modules list from settings.gradle")
		// Fallback: at least add the root module
		gf.modulesMap[""] = moduleMetadata{
			Group:    gf.groupId,
			Artifact: gf.artifactId,
			Version:  gf.projectVersion,
		}
		gf.modulesList = []string{""}
		return
	}

	gf.modulesList = []string{}
	for _, moduleName := range modules {
		if info, ok := gf.resolveModuleInfo(moduleName); ok {
			gf.modulesList = append(gf.modulesList, moduleName)
			gf.modulesMap[moduleName] = info
		}
	}
}

func (gf *GradleFlexPack) resolveModuleInfo(moduleName string) (moduleMetadata, bool) {
	if moduleName != "" {
		subPath := strings.ReplaceAll(moduleName, ":", string(filepath.Separator))
		modulePath := filepath.Join(gf.config.WorkingDirectory, subPath)
		if !isSubPath(gf.config.WorkingDirectory, modulePath) {
			log.Debug(fmt.Sprintf("Skipping module %s: path traversal detected", moduleName))
			return moduleMetadata{}, false
		}
	}
	var finalGroup, finalArtifact, finalVersion string

	if moduleName == "" {
		// Root module: use already-resolved values from loadBuildGradle
		finalGroup = gf.groupId
		finalArtifact = gf.artifactId
		finalVersion = gf.projectVersion
	} else {
		g, a, v := gf.getModuleMetadata(moduleName)

		finalGroup = gf.groupId
		if g != "" && g != "unspecified" {
			finalGroup = g
		}

		finalVersion = gf.projectVersion
		if v != "" && v != "unspecified" {
			finalVersion = v
		}

		if a != "" {
			finalArtifact = a
		} else {
			finalArtifact = strings.ReplaceAll(moduleName, ":", "-")
		}
	}

	return moduleMetadata{
		Group:    finalGroup,
		Artifact: finalArtifact,
		Version:  finalVersion,
	}, true
}

func (gf *GradleFlexPack) getModules() ([]string, error) {
	content, err := gf.readSettingsFile()
	if err != nil {
		return []string{""}, err
	}
	if content == "" {
		return []string{""}, nil
	}

	return gf.parseSettingsGradleModules(content), nil
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
		Modules: []entities.Module{},
	}

	if gf.wasPublishCommand {
		if artifacts, err := gf.getGradleDeployedArtifacts(); err == nil {
			gf.deployedArtifacts = artifacts
		} else {
			log.Warn("could not retrieve deployed artifacts; continuing without deployment details")
			log.Debug("failed to get deployed artifacts: " + err.Error())
		}
	}

	// Context allows callers to cancel long-running operations (e.g., timeout, user interrupt).
	for _, moduleName := range gf.modulesList {
		if err := gf.ctx.Err(); err != nil {
			return buildInfo, err
		}
		module := gf.processModule(moduleName)
		if artifacts, ok := gf.deployedArtifacts[moduleName]; ok {
			module.Artifacts = artifacts
		}
		buildInfo.Modules = append(buildInfo.Modules, module)
	}
	return buildInfo, nil
}

// We might need to process modules parallelly for better performance
func (gf *GradleFlexPack) processModule(moduleName string) entities.Module {
	groupId := gf.groupId
	version := gf.projectVersion
	artifactId := gf.artifactId
	var properties map[string]string

	if meta, ok := gf.modulesMap[moduleName]; ok {
		groupId = meta.Group
		version = meta.Version
		artifactId = meta.Artifact
	}

	if moduleName != "" {
		properties = map[string]string{"moduleName": moduleName}
	}

	// Ensure we have valid module metadata - use defaults if empty
	if groupId == "" {
		groupId = "unspecified"
	}
	if artifactId == "" {
		artifactId = "unspecified"
	}
	if version == "" {
		version = "unspecified"
	}

	deps, depGraph := gf.parseModuleDependencies(moduleName)
	requestedByMap := gf.buildRequestedByMap(depGraph)
	dependencies := gf.createDependencyEntities(deps, requestedByMap)

	return entities.Module{
		Id:           fmt.Sprintf("%s:%s:%s", groupId, artifactId, version),
		Properties:   properties,
		Type:         entities.Gradle,
		Dependencies: dependencies,
	}
}

func (gf *GradleFlexPack) parseModuleDependencies(moduleName string) ([]flexpack.DependencyInfo, map[string][]string) {
	depGraph := make(map[string][]string)
	// Primary method: Use Gradle CLI to get resolved dependencies
	deps, parsedGraph := gf.parseWithGradleDependencies(moduleName)
	if parsedGraph != nil {
		depGraph = parsedGraph
	}

	// Fallback: If CLI parsing didn't find any dependencies, try parsing build.gradle directly
	if len(deps) == 0 {
		log.Debug("CLI-based dependency parsing found no dependencies, falling back to build.gradle parsing")
		deps = gf.parseFromBuildGradle(moduleName)
	}

	return deps, depGraph
}

func (gf *GradleFlexPack) createDependencyEntities(deps []flexpack.DependencyInfo, requestedByMap map[string][]string) []entities.Dependency {
	sort.Slice(deps, func(i, j int) bool { return deps[i].ID < deps[j].ID })
	var dependencies []entities.Dependency
	for _, dep := range deps {
		checksumMap := gf.calculateChecksumWithFallback(dep)

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

		if checksum.Sha1 == "" && checksum.Sha256 == "" && checksum.Md5 == "" {
			artifactPath, _ := checksumMap["path"].(string)
			if artifactPath == "" {
				log.Warn(fmt.Sprintf("Skipping dependency %s: could not find artifact to calculate checksums", dep.ID))
			} else {
				log.Warn(fmt.Sprintf("Skipping dependency %s: artifact found but contained no checksums (%s)", dep.ID, artifactPath))
			}
			continue
		}

		// Sort scopes for deterministic output
		sort.Strings(dep.Scopes)

		entity := entities.Dependency{
			Id:       dep.ID,
			Type:     dep.Type,
			Scopes:   dep.Scopes,
			Checksum: checksum,
		}
		if entity.Type == "" {
			entity.Type = "jar"
		}

		if requesters, exists := requestedByMap[dep.ID]; exists && len(requesters) > 0 {
			entity.RequestedBy = [][]string{requesters}
		}
		dependencies = append(dependencies, entity)
	}
	return dependencies
}

func (gf *GradleFlexPack) buildRequestedByMap(depGraph map[string][]string) map[string][]string {
	requestedByMap := make(map[string][]string)
	for parent, children := range depGraph {
		for _, child := range children {
			if requestedByMap[child] == nil {
				requestedByMap[child] = []string{}
			}
			exists := false
			for _, req := range requestedByMap[child] {
				if req == parent {
					exists = true
					break
				}
			}
			if !exists {
				requestedByMap[child] = append(requestedByMap[child], parent)
			}
		}
	}
	for child, parents := range requestedByMap {
		sort.Strings(parents)
		requestedByMap[child] = parents
	}
	return requestedByMap
}

func (gf *GradleFlexPack) getModuleMetadata(moduleName string) (groupId, artifactId, version string) {
	contentBytes, _, err := gf.getBuildFileContent(moduleName)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to read build.gradle for module %s: %s", moduleName, err.Error()))
		return "", "", ""
	}
	return gf.parseBuildGradleMetadata(string(contentBytes))
}
