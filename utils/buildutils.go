package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const BuildInfoDetails = "details"

func getBuildDir(buildName, buildNumber, projectKey, buildsDirPath string) (string, error) {
	encodedDirName := base64.StdEncoding.EncodeToString([]byte(buildName + "_" + buildNumber + "_" + projectKey))
	buildsDir := filepath.Join(buildsDirPath, encodedDirName)
	err := os.MkdirAll(buildsDir, 0777)
	if err != nil {
		return "", err
	}
	return buildsDir, nil
}

func getPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath string) (string, error) {
	buildDir, err := getBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return "", err
	}
	buildDir = filepath.Join(buildDir, "partials")
	err = os.MkdirAll(buildDir, 0777)
	if err != nil {
		return "", err
	}
	return buildDir, nil
}

func SaveBuildInfo(buildName, buildNumber, projectKey, buildsDirPath string, buildInfo *buildinfo.BuildInfo, log Log) error {
	b, err := json.Marshal(buildInfo)
	if err != nil {
		return err
	}
	var content bytes.Buffer
	err = json.Indent(&content, b, "", "  ")
	if err != nil {
		return err
	}
	dirPath, err := getBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return err
	}
	log.Debug("Creating temp build file at: " + dirPath)
	tempFile, err := ioutil.TempFile(dirPath, "temp")
	if err != nil {
		return err
	}
	defer tempFile.Close()
	_, err = tempFile.Write([]byte(content.String()))
	return err
}

func SaveBuildGeneralDetails(buildName, buildNumber, projectKey, buildsDirPath string, log Log) error {
	partialsBuildDir, err := getPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return err
	}
	log.Debug("Saving build general details at: " + partialsBuildDir)
	detailsFilePath := filepath.Join(partialsBuildDir, BuildInfoDetails)
	var exists bool
	exists, err = isFileExists(detailsFilePath)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	meta := buildinfo.General{
		Timestamp: time.Now(),
	}
	b, err := json.Marshal(&meta)
	if err != nil {
		return err
	}
	var content bytes.Buffer
	err = json.Indent(&content, b, "", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(detailsFilePath, []byte(content.String()), 0600)
	return err
}

// SavePartialBuildInfo saves the given partial in the builds directory.
// The partial's Timestamp field is filled inside this function.
func SavePartialBuildInfo(buildName, buildNumber, projectKey, buildsDirPath string, partial *buildinfo.Partial, log Log) error {
	partial.Timestamp = time.Now().UnixNano() / int64(time.Millisecond)
	b, err := json.Marshal(&partial)
	if err != nil {
		return err
	}
	var content bytes.Buffer
	err = json.Indent(&content, b, "", "  ")
	if err != nil {
		return err
	}
	dirPath, err := getPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return err
	}
	log.Debug("Creating temp build file at:", dirPath)
	tempFile, err := ioutil.TempFile(dirPath, "temp")
	if err != nil {
		return err
	}
	defer tempFile.Close()
	_, err = tempFile.Write([]byte(content.String()))
	return err
}

func GetGeneratedBuildsInfo(buildName, buildNumber, projectKey, buildsDirPath string) ([]*buildinfo.BuildInfo, error) {
	buildDir, err := getBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return nil, err
	}
	buildFiles, err := listFiles(buildDir)
	if err != nil {
		return nil, err
	}

	var generatedBuildsInfo []*buildinfo.BuildInfo
	for _, buildFile := range buildFiles {
		dir, err := isDirExists(buildFile)
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
		buildInfo := new(buildinfo.BuildInfo)
		err = json.Unmarshal(content, &buildInfo)
		if err != nil {
			return nil, err
		}
		generatedBuildsInfo = append(generatedBuildsInfo, buildInfo)
	}
	return generatedBuildsInfo, nil
}

func CreateBuildInfoFromPartials(buildName, buildNumber, projectKey, buildsDirPath string, includeFilter, excludeFilter Filter) (*buildinfo.BuildInfo, error) {
	partials, err := readPartialBuildInfoFiles(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return nil, err
	}
	sort.Sort(partials)

	buildInfo := buildinfo.New()
	buildInfo.Name = buildName
	buildInfo.Number = buildNumber
	buildGeneralDetails, err := readBuildInfoGeneralDetails(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return nil, err
	}
	buildInfo.Started = buildGeneralDetails.Timestamp.Format(buildinfo.TimeFormat)
	modules, env, vcsList, issues, err := extractBuildInfoData(partials, includeFilter, excludeFilter)
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
			module.Id = buildName
		}
		buildInfo.Modules = append(buildInfo.Modules, module)
	}
	return buildInfo, nil
}

func readPartialBuildInfoFiles(buildName, buildNumber, projectKey, buildsDirPath string) (buildinfo.Partials, error) {
	var partials buildinfo.Partials
	partialsBuildDir, err := getPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return nil, err
	}
	buildFiles, err := listFiles(partialsBuildDir)
	if err != nil {
		return nil, err
	}
	for _, buildFile := range buildFiles {
		dir, err := isDirExists(buildFile)
		if err != nil {
			return nil, err
		}
		if dir {
			continue
		}
		if strings.HasSuffix(buildFile, BuildInfoDetails) {
			continue
		}
		content, err := ioutil.ReadFile(buildFile)
		if err != nil {
			return nil, err
		}
		partial := new(buildinfo.Partial)
		err = json.Unmarshal(content, &partial)
		if err != nil {
			return nil, err
		}
		partials = append(partials, partial)
	}

	return partials, nil
}

func readBuildInfoGeneralDetails(buildName, buildNumber, projectKey, buildsDirPath string) (*buildinfo.General, error) {
	partialsBuildDir, err := getPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return nil, err
	}
	generalDetailsFilePath := filepath.Join(partialsBuildDir, BuildInfoDetails)
	fileExists, err := isFileExists(generalDetailsFilePath)
	if err != nil {
		return nil, err
	}
	if fileExists == false {
		var buildString string
		if projectKey != "" {
			buildString = fmt.Sprintf("build-name: <%s>, build-number: <%s> and project: <%s>", buildName, buildNumber, projectKey)
		} else {
			buildString = fmt.Sprintf("build-name: <%s> and build-number: <%s>", buildName, buildNumber)
		}
		return nil, errors.New("Failed to construct the build-info to be published. " +
			"This may be because there were no previous commands, which collected build-info for " + buildString)
	}
	content, err := ioutil.ReadFile(generalDetailsFilePath)
	if err != nil {
		return nil, err
	}
	details := new(buildinfo.General)
	err = json.Unmarshal(content, &details)
	if err != nil {
		return nil, err
	}
	return details, nil
}

func RemoveBuildDir(buildName, buildNumber, projectKey, buildsDirPath string) error {
	tempDirPath, err := getBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return err
	}
	exists, err := isDirExists(tempDirPath)
	if err != nil {
		return err
	}
	if exists {
		return os.RemoveAll(tempDirPath)
	}
	return nil
}

type partialModule struct {
	moduleType   buildinfo.ModuleType
	artifacts    map[string]buildinfo.Artifact
	dependencies map[string]buildinfo.Dependency
	checksum     *buildinfo.Checksum
}

type Filter func(map[string]string) (map[string]string, error)

func extractBuildInfoData(partials buildinfo.Partials, includeFilter, excludeFilter Filter) ([]buildinfo.Module, buildinfo.Env, []buildinfo.Vcs, buildinfo.Issues, error) {
	var vcs []buildinfo.Vcs
	var issues buildinfo.Issues
	env := make(map[string]string)
	partialModules := make(map[string]*partialModule)
	issuesMap := make(map[string]*buildinfo.AffectedIssue)
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
			var err error
			filteredEnv := partial.Env
			if includeFilter != nil {
				filteredEnv, err = includeFilter(filteredEnv)
				if err != nil {
					return partialModulesToModules(partialModules), env, vcs, issues, err
				}
			}
			if excludeFilter != nil {
				filteredEnv, err = excludeFilter(filteredEnv)
				if err != nil {
					return partialModulesToModules(partialModules), env, vcs, issues, err
				}
			}
			for k, v := range filteredEnv {
				env[k] = v
			}
		case partial.ModuleType == buildinfo.Build:
			partialModules[moduleId].checksum = partial.Checksum
		}
	}
	return partialModulesToModules(partialModules), env, vcs, issuesMapToArray(issues, issuesMap), nil
}

func partialModulesToModules(partialModules map[string]*partialModule) []buildinfo.Module {
	var modules []buildinfo.Module
	for moduleId, singlePartialModule := range partialModules {
		moduleArtifacts := artifactsMapToList(singlePartialModule.artifacts)
		moduleDependencies := dependenciesMapToList(singlePartialModule.dependencies)
		modules = append(modules, *createModule(moduleId, singlePartialModule.moduleType, singlePartialModule.checksum, moduleArtifacts, moduleDependencies))
	}
	return modules
}

func issuesMapToArray(issues buildinfo.Issues, issuesMap map[string]*buildinfo.AffectedIssue) buildinfo.Issues {
	for _, issue := range issuesMap {
		issues.AffectedIssues = append(issues.AffectedIssues, *issue)
	}
	return issues
}

func addDependencyToPartialModule(dependency buildinfo.Dependency, moduleId string, partialModules map[string]*partialModule) {
	// init map if needed
	if partialModules[moduleId].dependencies == nil {
		partialModules[moduleId].dependencies = make(map[string]buildinfo.Dependency)
	}
	key := fmt.Sprintf("%s-%s-%s-%s", dependency.Id, dependency.Sha1, dependency.Md5, dependency.Scopes)
	partialModules[moduleId].dependencies[key] = dependency
}

func addArtifactToPartialModule(artifact buildinfo.Artifact, moduleId string, partialModules map[string]*partialModule) {
	// init map if needed
	if partialModules[moduleId].artifacts == nil {
		partialModules[moduleId].artifacts = make(map[string]buildinfo.Artifact)
	}
	key := fmt.Sprintf("%s-%s-%s", artifact.Name, artifact.Sha1, artifact.Md5)
	partialModules[moduleId].artifacts[key] = artifact
}

func artifactsMapToList(artifactsMap map[string]buildinfo.Artifact) []buildinfo.Artifact {
	var artifacts []buildinfo.Artifact
	for _, artifact := range artifactsMap {
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}

func dependenciesMapToList(dependenciesMap map[string]buildinfo.Dependency) []buildinfo.Dependency {
	var dependencies []buildinfo.Dependency
	for _, dependency := range dependenciesMap {
		dependencies = append(dependencies, dependency)
	}
	return dependencies
}

func createModule(moduleId string, moduleType buildinfo.ModuleType, checksum *buildinfo.Checksum, artifacts []buildinfo.Artifact, dependencies []buildinfo.Dependency) *buildinfo.Module {
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

func createDefaultModule(moduleId string) *buildinfo.Module {
	return &buildinfo.Module{
		Id:           moduleId,
		Properties:   map[string][]string{},
		Artifacts:    []buildinfo.Artifact{},
		Dependencies: []buildinfo.Dependency{},
	}
}
