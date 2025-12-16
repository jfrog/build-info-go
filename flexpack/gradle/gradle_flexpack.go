package flexpack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

var agentVersion = "1.0.0"

const defaultGradleCommandTimeout = 1 * time.Minute

type moduleMetadata struct {
	Group    string
	Artifact string
	Version  string
}

type GradleFlexPack struct {
	config            flexpack.GradleConfig
	ctx               context.Context
	dependencies      []flexpack.DependencyInfo
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
		config:          config,
		ctx:             ctx,
		dependencies:    []flexpack.DependencyInfo{},
		dependencyGraph: make(map[string][]string),
		requestedByMap:  make(map[string][]string),
	}

	if gf.config.GradleExecutable == "" {
		execPath, err := GetGradleExecutablePath(gf.config.WorkingDirectory)
		if err != nil {
			log.Warn("Gradle executable not found in PATH, using 'gradle' as fallback")
			gf.config.GradleExecutable = "gradle"
		} else {
			gf.config.GradleExecutable = execPath
		}
	}

	if err := gf.loadBuildGradle(); err != nil {
		log.Warn("Failed to load build.gradle: " + err.Error())
	}
	gf.scanAllModules()
	return gf, nil
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
		if !gf.validatePathWithinWorkingDir(modulePath) {
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
		module := gf.processModule(moduleName)
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
			log.Warn(fmt.Sprintf("Gradle version %s may not be fully compatible; minimum recommended is 5.0", version))
		}
		return version
	}
	return "unknown"
}

// We might need to process modules parallelly for better performance
func (gf *GradleFlexPack) processModule(moduleName string) entities.Module {
	groupId := gf.groupId
	version := gf.projectVersion
	artifactId := gf.artifactId

	if meta, ok := gf.modulesMap[moduleName]; ok {
		groupId = meta.Group
		version = meta.Version
		artifactId = meta.Artifact
	}

	gf.dependencies = []flexpack.DependencyInfo{}
	gf.dependencyGraph = make(map[string][]string)
	gf.requestedByMap = make(map[string][]string)

	gf.parseModuleDependencies(moduleName)
	requestedByMap := gf.CalculateRequestedBy()
	dependencies := gf.createDependencyEntities(requestedByMap)

	props := make(map[string]string)
	subPath := strings.ReplaceAll(moduleName, ":", string(filepath.Separator))
	subPath = strings.TrimPrefix(subPath, string(filepath.Separator))
	props["module_path"] = subPath

	return entities.Module{
		Id:           fmt.Sprintf("%s:%s:%s", groupId, artifactId, version),
		Type:         entities.Gradle,
		Properties:   props,
		Dependencies: dependencies,
	}
}

func (gf *GradleFlexPack) parseModuleDependencies(moduleName string) {
	// Primary method: Use Gradle CLI to get resolved dependencies
	gf.parseWithGradleDependencies(moduleName)

	// Fallback: If CLI parsing didn't find any dependencies, try parsing build.gradle directly
	if len(gf.dependencies) == 0 {
		log.Debug("CLI-based dependency parsing found no dependencies, falling back to build.gradle parsing")
		if gf.parseFromBuildGradle(moduleName) {
			log.Debug("Successfully parsed dependencies from build.gradle file")
		}
	}
}

func (gf *GradleFlexPack) createDependencyEntities(requestedByMap map[string][]string) []entities.Dependency {
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
	return dependencies
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
			exists := false
			for _, req := range gf.requestedByMap[child] {
				if req == parent {
					exists = true
					break
				}
			}
			if !exists {
				gf.requestedByMap[child] = append(gf.requestedByMap[child], parent)
			}
		}
	}
}

func (gf *GradleFlexPack) getModuleMetadata(moduleName string) (groupId, artifactId, version string) {
	contentBytes, _, err := gf.getBuildFileContent(moduleName)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to read build.gradle for module %s: %s", moduleName, err.Error()))
		return "", "", ""
	}
	return gf.parseBuildGradleMetadata(string(contentBytes))
}
