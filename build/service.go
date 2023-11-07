package build

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const BuildsTempPath = "jfrog/builds/"

type BuildInfoService struct {
	tempDirPath string
	logger      utils.Log
}

func NewBuildInfoService() *BuildInfoService {
	return &BuildInfoService{tempDirPath: filepath.Join(os.TempDir(), BuildsTempPath), logger: &utils.NullLog{}}
}

func (bis *BuildInfoService) SetTempDirPath(tempDirPath string) {
	bis.tempDirPath = tempDirPath
}

func (bis *BuildInfoService) SetLogger(logger utils.Log) {
	bis.logger = logger
}

// GetOrCreateBuild gets a build from cache, or creates a new one if it doesn't exist.
// It's important to invoke this function at the very beginning of the build, so that the start time property in the build-info will be accurate.
func (bis *BuildInfoService) GetOrCreateBuild(buildName, buildNumber string) (*Build, error) {
	return bis.GetOrCreateBuildWithProject(buildName, buildNumber, "")
}

// GetOrCreateBuildWithProject gets a build from cache, or creates a new one if it doesn't exist.
// It's important to invoke this function at the very beginning of the build, so that the start time property in the build-info will be accurate.
func (bis *BuildInfoService) GetOrCreateBuildWithProject(buildName, buildNumber, projectKey string) (build *Build, err error) {
	buildTime := time.Now()
	if len(buildName) > 0 && len(buildNumber) > 0 {
		if buildTime, err = getOrCreateBuildGeneralDetails(buildName, buildNumber, buildTime, projectKey, bis.tempDirPath, bis.logger); err != nil {
			return
		}
	}
	return NewBuild(buildName, buildNumber, buildTime, projectKey, bis.tempDirPath, bis.logger), nil
}

func getOrCreateBuildGeneralDetails(buildName, buildNumber string, buildTime time.Time, projectKey, buildsDirPath string, log utils.Log) (time.Time, error) {
	partialsBuildDir, err := utils.GetPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return buildTime, err
	}
	detailsFilePath := filepath.Join(partialsBuildDir, BuildInfoDetails)
	var exists bool
	exists, err = utils.IsFileExists(detailsFilePath, true)
	if err != nil {
		return buildTime, err
	}
	if exists {
		log.Debug("Reading build general details from: " + partialsBuildDir)
		var generalDetails *buildinfo.General
		generalDetails, err = ReadBuildInfoGeneralDetails(buildName, buildNumber, projectKey, buildsDirPath)
		return generalDetails.Timestamp, err
	}
	log.Debug("Saving build general details at: " + partialsBuildDir)
	meta := buildinfo.General{
		Timestamp: buildTime,
	}
	b, err := json.Marshal(&meta)
	if err != nil {
		return buildTime, err
	}
	var content bytes.Buffer
	err = json.Indent(&content, b, "", "  ")
	if err != nil {
		return buildTime, err
	}
	return buildTime, os.WriteFile(detailsFilePath, content.Bytes(), 0600)
}
