package build

import (
	"github.com/jfrog/build-info-go/entities"
	buildutils "github.com/jfrog/build-info-go/utils"
	"os"
	"strings"
)

type Build struct {
	buildName         string
	buildNumber       string
	projectKey        string
	tempDirPath       string
	logger            buildutils.Log
	agentName         string
	agentVersion      string
	buildAgentVersion string
	principal         string
	buildUrl          string
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

func (b *Build) BuildName() string {
	return b.buildName
}

func (b *Build) BuildNumber() string {
	return b.buildNumber
}

func (b *Build) ProjectKey() string {
	return b.projectKey
}

func (b *Build) TempDirPath() string {
	return b.tempDirPath
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

func (b *Build) ToBuildInfo() (*entities.BuildInfo, error) {
	buildInfo, err := buildutils.CreateBuildInfoFromPartials(b)
	if err != nil {
		return nil, err
	}
	buildInfo.SetAgentName(b.agentName)
	buildInfo.SetAgentVersion(b.agentVersion)
	buildInfo.SetBuildAgentVersion(b.buildAgentVersion)
	buildInfo.Principal = b.principal
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
