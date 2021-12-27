package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const BuildInfoDetails = "details"

type Build struct {
	buildName         string
	buildNumber       string
	projectKey        string
	tempDirPath       string
	logger            utils.Log
	agentName         string
	agentVersion      string
	buildAgentVersion string
	principal         string
	buildUrl          string
}

func NewBuild(buildName, buildNumber, projectKey, tempDirPath string, logger utils.Log) *Build {
	return &Build{
		buildName:   buildName,
		buildNumber: buildNumber,
		projectKey:  projectKey,
		tempDirPath: tempDirPath,
		logger:      logger,
	}
}

// This field is not saved in local cache. It is used only when creating a build-info using the ToBuildInfo() function.
func (b *Build) SetAgentName(agentName string) {
	b.agentName = agentName
}

// This field is not saved in local cache. It is used only when creating a build-info using the ToBuildInfo() function.
func (b *Build) SetAgentVersion(agentVersion string) {
	b.agentVersion = agentVersion
}

// This field is not saved in local cache. It is used only when creating a build-info using the ToBuildInfo() function.
func (b *Build) SetBuildAgentVersion(buildAgentVersion string) {
	b.buildAgentVersion = buildAgentVersion
}

// This field is not saved in local cache. It is used only when creating a build-info using the ToBuildInfo() function.
func (b *Build) SetPrincipal(principal string) {
	b.principal = principal
}

// This field is not saved in local cache. It is used only when creating a build-info using the ToBuildInfo() function.
func (b *Build) SetBuildUrl(buildUrl string) {
	b.buildUrl = buildUrl
}

// AddGoModule adds a Go module to this Build. Pass srcPath as an empty string if the root of the Go project is the working directory.
func (b *Build) AddGoModule(srcPath string) (*GoModule, error) {
	return newGoModule(srcPath, b)
}

// AddMavenModule adds a Maven module to this Build. Pass srcPath as an empty string if the root of the Maven project is the working directory.
func (b *Build) AddMavenModule(srcPath string) (*MavenModule, error) {
	return newMavenModule(b, srcPath)
}

// AddGradleModule adds a Gradle module to this Build. Pass srcPath as an empty string if the root of the Gradle project is the working directory.
func (b *Build) AddGradleModule(srcPath string) (*GradleModule, error) {
	return newGradleModule(b, srcPath)
}

func (b *Build) AddNpmModule(srcPath string) (*NpmModule, error) {
	return newNpmModule(srcPath, b)
}

func (b *Build) CollectEnv() error {
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		if len(pair[0]) != 0 {
			envMap["buildInfo.env."+pair[0]] = pair[1]
		}
	}
	partial := &entities.Partial{Env: envMap}
	return b.SavePartialBuildInfo(partial)
}

func (b *Build) Clean() error {
	tempDirPath, err := utils.GetBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return err
	}
	exists, err := utils.IsDirExists(tempDirPath)
	if err != nil {
		return err
	}
	if exists {
		return os.RemoveAll(tempDirPath)
	}
	return nil
}

func (b *Build) ToBuildInfo() (*entities.BuildInfo, error) {
	buildInfo, err := b.createBuildInfoFromPartials()
	if err != nil {
		return nil, err
	}
	buildInfo.SetAgentName(b.agentName)
	buildInfo.SetAgentVersion(b.agentVersion)
	buildInfo.SetBuildAgentVersion(b.buildAgentVersion)
	buildInfo.Principal = b.principal
	buildInfo.BuildUrl = b.buildUrl

	generatedBuildsInfo, err := b.getGeneratedBuildsInfo()
	if err != nil {
		return nil, err
	}
	for _, v := range generatedBuildsInfo {
		buildInfo.Append(v)
	}

	return buildInfo, nil
}

func (b *Build) getGeneratedBuildsInfo() ([]*entities.BuildInfo, error) {
	buildDir, err := utils.GetBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return nil, err
	}
	buildFiles, err := utils.ListFiles(buildDir)
	if err != nil {
		return nil, err
	}

	var generatedBuildsInfo []*entities.BuildInfo
	for _, buildFile := range buildFiles {
		dir, err := utils.IsDirExists(buildFile)
		if err != nil {
			return nil, err
		}
		if dir {
			continue
		}
		content, err := ioutil.ReadFile(buildFile)
		if err != nil {
			return nil, err
		}
		if len(content) == 0 {
			continue
		}
		buildInfo := new(entities.BuildInfo)
		err = json.Unmarshal(content, &buildInfo)
		if err != nil {
			return nil, err
		}
		generatedBuildsInfo = append(generatedBuildsInfo, buildInfo)
	}
	return generatedBuildsInfo, nil
}

func (b *Build) SaveBuildInfo(buildInfo *entities.BuildInfo) (err error) {
	buildJson, err := json.Marshal(buildInfo)
	if err != nil {
		return
	}
	var content bytes.Buffer
	err = json.Indent(&content, buildJson, "", "  ")
	if err != nil {
		return
	}
	dirPath, err := utils.GetBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return
	}
	b.logger.Debug("Creating temp build file at: " + dirPath)
	tempFile, err := utils.CreateTempBuildFile(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath, b.logger)
	if err != nil {
		return
	}
	defer func() {
		e := tempFile.Close()
		if err == nil {
			err = e
		}
	}()
	_, err = tempFile.Write([]byte(content.String()))
	return
}

// SavePartialBuildInfo saves the given partial in the builds directory.
// The partial's Timestamp field is set inside this function.
func (b *Build) SavePartialBuildInfo(partial *entities.Partial) (err error) {
	partial.Timestamp = time.Now().UnixNano() / int64(time.Millisecond)
	partialJson, err := json.Marshal(&partial)
	if err != nil {
		return
	}
	var content bytes.Buffer
	err = json.Indent(&content, partialJson, "", "  ")
	if err != nil {
		return
	}
	dirPath, err := utils.GetPartialsBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return
	}
	b.logger.Debug("Creating temp build file at:", dirPath)
	tempFile, err := ioutil.TempFile(dirPath, "temp")
	if err != nil {
		return
	}
	defer func() {
		e := tempFile.Close()
		if err == nil {
			err = e
		}
	}()
	_, err = tempFile.Write([]byte(content.String()))
	return
}

func (b *Build) createBuildInfoFromPartials() (*entities.BuildInfo, error) {
	partials, err := b.readPartialBuildInfoFiles()
	if err != nil {
		return nil, err
	}
	sort.Sort(partials)

	buildInfo := entities.New()
	buildInfo.Name = b.buildName
	buildInfo.Number = b.buildNumber
	buildGeneralDetails, err := b.readBuildInfoGeneralDetails()
	if err != nil {
		return nil, err
	}
	buildInfo.Started = buildGeneralDetails.Timestamp.Format(entities.TimeFormat)
	modules, env, vcsList, issues, err := extractBuildInfoData(partials)
	if err != nil {
		return nil, err
	}
	if len(env) != 0 {
		buildInfo.Properties = env
	}

	for _, vcs := range vcsList {
		buildInfo.VcsList = append(buildInfo.VcsList, vcs)
	}

	// Check for Tracker as it must be set
	if issues.Tracker != nil && issues.Tracker.Name != "" {
		buildInfo.Issues = &issues
	}
	for _, module := range modules {
		if module.Id == "" {
			module.Id = b.buildName
		}
		buildInfo.Modules = append(buildInfo.Modules, module)
	}
	return buildInfo, nil
}

func (b *Build) readPartialBuildInfoFiles() (entities.Partials, error) {
	var partials entities.Partials
	partialsBuildDir, err := utils.GetPartialsBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return nil, err
	}
	buildFiles, err := utils.ListFiles(partialsBuildDir)
	if err != nil {
		return nil, err
	}
	for _, buildFile := range buildFiles {
		dir, err := utils.IsDirExists(buildFile)
		if err != nil {
			return nil, err
		}
		if dir || strings.HasSuffix(buildFile, BuildInfoDetails) {
			continue
		}
		content, err := ioutil.ReadFile(buildFile)
		if err != nil {
			return nil, err
		}
		partial := new(entities.Partial)
		err = json.Unmarshal(content, &partial)
		if err != nil {
			return nil, err
		}
		partials = append(partials, partial)
	}

	return partials, nil
}

func (b *Build) readBuildInfoGeneralDetails() (*entities.General, error) {
	partialsBuildDir, err := utils.GetPartialsBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return nil, err
	}
	generalDetailsFilePath := filepath.Join(partialsBuildDir, BuildInfoDetails)
	fileExists, err := utils.IsFileExists(generalDetailsFilePath)
	if err != nil {
		return nil, err
	}
	if fileExists == false {
		var buildString string
		if b.projectKey != "" {
			buildString = fmt.Sprintf("build-name: <%s>, build-number: <%s> and project: <%s>", b.buildName, b.buildNumber, b.projectKey)
		} else {
			buildString = fmt.Sprintf("build-name: <%s> and build-number: <%s>", b.buildName, b.buildNumber)
		}
		return nil, errors.New("Failed to construct the build-info to be published. " +
			"This may be because there were no previous commands, which collected build-info for " + buildString)
	}
	content, err := ioutil.ReadFile(generalDetailsFilePath)
	if err != nil {
		return nil, err
	}
	details := new(entities.General)
	err = json.Unmarshal(content, &details)
	if err != nil {
		return nil, err
	}
	return details, nil
}

type partialModule struct {
	moduleType   entities.ModuleType
	artifacts    map[string]entities.Artifact
	dependencies map[string]entities.Dependency
	checksum     *entities.Checksum
}

func extractBuildInfoData(partials entities.Partials) ([]entities.Module, entities.Env, []entities.Vcs, entities.Issues, error) {
	var vcs []entities.Vcs
	var issues entities.Issues
	env := make(map[string]string)
	partialModules := make(map[string]*partialModule)
	issuesMap := make(map[string]*entities.AffectedIssue)
	for _, partial := range partials {
		moduleId := partial.ModuleId
		if partialModules[moduleId] == nil {
			partialModules[moduleId] = &partialModule{moduleType: partial.ModuleType}
		}
		switch {
		case partial.Artifacts != nil:
			for _, artifact := range partial.Artifacts {
				addArtifactToPartialModule(artifact, moduleId, partialModules)
			}
		case partial.Dependencies != nil:
			for _, dependency := range partial.Dependencies {
				addDependencyToPartialModule(dependency, moduleId, partialModules)
			}
		case partial.VcsList != nil:
			for _, partialVcs := range partial.VcsList {
				vcs = append(vcs, partialVcs)
			}
			if partial.Issues == nil {
				continue
			}
			// Collect issues.
			issues.Tracker = partial.Issues.Tracker
			issues.AggregateBuildIssues = partial.Issues.AggregateBuildIssues
			issues.AggregationBuildStatus = partial.Issues.AggregationBuildStatus
			// If affected issues exist, add them to issues map
			if partial.Issues.AffectedIssues != nil {
				for i, issue := range partial.Issues.AffectedIssues {
					issuesMap[issue.Key] = &partial.Issues.AffectedIssues[i]
				}
			}
		case partial.Env != nil:
			for k, v := range partial.Env {
				env[k] = v
			}
		case partial.ModuleType == entities.Build:
			partialModules[moduleId].checksum = partial.Checksum
		}
	}
	return partialModulesToModules(partialModules), env, vcs, issuesMapToArray(issues, issuesMap), nil
}

func partialModulesToModules(partialModules map[string]*partialModule) []entities.Module {
	var modules []entities.Module
	for moduleId, singlePartialModule := range partialModules {
		moduleArtifacts := artifactsMapToList(singlePartialModule.artifacts)
		moduleDependencies := dependenciesMapToList(singlePartialModule.dependencies)
		modules = append(modules, *createModule(moduleId, singlePartialModule.moduleType, singlePartialModule.checksum, moduleArtifacts, moduleDependencies))
	}
	return modules
}

func issuesMapToArray(issues entities.Issues, issuesMap map[string]*entities.AffectedIssue) entities.Issues {
	for _, issue := range issuesMap {
		issues.AffectedIssues = append(issues.AffectedIssues, *issue)
	}
	return issues
}

func addArtifactToPartialModule(artifact entities.Artifact, moduleId string, partialModules map[string]*partialModule) {
	// init map if needed
	if partialModules[moduleId].artifacts == nil {
		partialModules[moduleId].artifacts = make(map[string]entities.Artifact)
	}
	key := fmt.Sprintf("%s-%s-%s", artifact.Name, artifact.Sha1, artifact.Md5)
	partialModules[moduleId].artifacts[key] = artifact
}

func addDependencyToPartialModule(dependency entities.Dependency, moduleId string, partialModules map[string]*partialModule) {
	// init map if needed
	if partialModules[moduleId].dependencies == nil {
		partialModules[moduleId].dependencies = make(map[string]entities.Dependency)
	}
	key := fmt.Sprintf("%s-%s-%s-%s", dependency.Id, dependency.Sha1, dependency.Md5, dependency.Scopes)
	partialModules[moduleId].dependencies[key] = dependency
}

func artifactsMapToList(artifactsMap map[string]entities.Artifact) []entities.Artifact {
	var artifacts []entities.Artifact
	for _, artifact := range artifactsMap {
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}

func dependenciesMapToList(dependenciesMap map[string]entities.Dependency) []entities.Dependency {
	var dependencies []entities.Dependency
	for _, dependency := range dependenciesMap {
		dependencies = append(dependencies, dependency)
	}
	return dependencies
}

func createModule(moduleId string, moduleType entities.ModuleType, checksum *entities.Checksum, artifacts []entities.Artifact, dependencies []entities.Dependency) *entities.Module {
	module := createDefaultModule(moduleId)
	module.Type = moduleType
	module.Checksum = checksum
	if artifacts != nil && len(artifacts) > 0 {
		module.Artifacts = append(module.Artifacts, artifacts...)
	}
	if dependencies != nil && len(dependencies) > 0 {
		module.Dependencies = append(module.Dependencies, dependencies...)
	}
	return module
}

func createDefaultModule(moduleId string) *entities.Module {
	return &entities.Module{
		Id:           moduleId,
		Properties:   map[string][]string{},
		Artifacts:    []entities.Artifact{},
		Dependencies: []entities.Dependency{},
	}
}

func createEmptyBuildInfoFile(containingBuild *Build) (string, error) {
	buildDir, err := utils.CreateTempBuildFile(containingBuild.buildName, containingBuild.buildNumber, containingBuild.projectKey, containingBuild.tempDirPath, containingBuild.logger)
	if err != nil {
		return "", err
	}
	if err := buildDir.Close(); err != nil {
		return "", err
	}
	// If this is a Windows machine, there is a need to modify the path for the build info file to match Java syntax with double \\
	return utils.DoubleWinPathSeparator(buildDir.Name()), nil
}
