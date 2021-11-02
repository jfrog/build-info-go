package build

import (
	"github.com/jfrog/build-info-go/entities"
	buildutils "github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/stringutils"
	"os"
	"strings"
)

const BuildInfoEnvPrefix = "buildInfo.env."

type Build struct {
	buildName            string
	buildNumber          string
	projectKey           string
	tempDirPath          string
	logger               buildutils.Log
	includeFilter        buildutils.Filter
	excludeFilter        buildutils.Filter
	agentName            string
	agentVersion         string
	buildAgentVersion    string
	artifactoryPrincipal string
	buildUrl             string
}

func NewBuild(buildName, buildNumber, projectKey, tempDirPath string, logger buildutils.Log) *Build {
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
func (b *Build) SetArtifactoryPrincipal(artifactoryPrincipal string) {
	b.artifactoryPrincipal = artifactoryPrincipal
}

// This field is not saved in local cache. It is used only when creating a build-info using the ToBuildInfo() function.
func (b *Build) SetBuildUrl(buildUrl string) {
	b.buildUrl = buildUrl
}

// AddGoModule adds a Go module to this Build. Pass srcPath as an empty string if the root of the Go project is the working directory.
func (b *Build) AddGoModule(srcPath string) (*GoModule, error) {
	return newGoModule(srcPath, b)
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
	return buildutils.SavePartialBuildInfo(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath, partial, b.logger)
}

// IncludeEnv sets one or more wildcard patterns, indicating which environment variables should be picked up and added to the build-info.
func (b *Build) IncludeEnv(patterns ...string) error {
	if len(patterns) == 0 {
		b.includeFilter = nil
		return nil
	}

	b.includeFilter = func(tempMap map[string]string) (map[string]string, error) {
		result := make(map[string]string)
		for k, v := range tempMap {
			for _, filterPattern := range patterns {
				matched, err := stringutils.MatchWildcardPattern(strings.ToLower(filterPattern), strings.ToLower(strings.TrimPrefix(k, BuildInfoEnvPrefix)))
				if err != nil {
					return nil, err
				}
				if matched {
					result[k] = v
					break
				}
			}
		}
		return result, nil
	}

	return nil
}

// ExcludeEnv sets one or more wildcard patterns, indicating which environment variables to exclude from the build-info.
func (b *Build) ExcludeEnv(patterns ...string) error {
	if len(patterns) == 0 {
		b.excludeFilter = nil
		return nil
	}

	b.excludeFilter = func(tempMap map[string]string) (map[string]string, error) {
		result := make(map[string]string)
		for k, v := range tempMap {
			include := true
			for _, filterPattern := range patterns {
				matched, err := stringutils.MatchWildcardPattern(strings.ToLower(filterPattern), strings.ToLower(strings.TrimPrefix(k, BuildInfoEnvPrefix)))
				if err != nil {
					return nil, err
				}
				if matched {
					include = false
					break
				}
			}
			if include {
				result[k] = v
			}
		}
		return result, nil
	}

	return nil
}

func (b *Build) ToBuildInfo() (*entities.BuildInfo, error) {
	buildInfo, err := buildutils.CreateBuildInfoFromPartials(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath, b.includeFilter, b.excludeFilter)
	if err != nil {
		return nil, err
	}
	buildInfo.SetAgentName(b.agentName)
	buildInfo.SetAgentVersion(b.agentVersion)
	buildInfo.SetBuildAgentVersion(b.buildAgentVersion)
	buildInfo.ArtifactoryPrincipal = b.artifactoryPrincipal
	buildInfo.BuildUrl = b.buildUrl

	generatedBuildsInfo, err := buildutils.GetGeneratedBuildsInfo(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
	if err != nil {
		return nil, err
	}
	for _, v := range generatedBuildsInfo {
		buildInfo.Append(v)
	}

	return buildInfo, nil
}

func (b *Build) Clean() error {
	return buildutils.RemoveBuildDir(b.buildName, b.buildNumber, b.projectKey, b.tempDirPath)
}
