package build

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
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
func (bis *BuildInfoService) GetOrCreateBuildWithProject(buildName, buildNumber, projectKey string) (*Build, error) {
	if len(buildName) > 0 && len(buildNumber) > 0 {
		err := saveBuildGeneralDetails(buildName, buildNumber, projectKey, bis.tempDirPath, bis.logger)
		if err != nil {
			return nil, err
		}
	}
	return NewBuild(buildName, buildNumber, projectKey, bis.tempDirPath, bis.logger), nil
}

func saveBuildGeneralDetails(buildName, buildNumber, projectKey, buildsDirPath string, log utils.Log) error {
	partialsBuildDir, err := utils.GetPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return err
	}
	log.Debug("Saving build general details at: " + partialsBuildDir)
	detailsFilePath := filepath.Join(partialsBuildDir, BuildInfoDetails)
	var exists bool
	exists, err = utils.IsFileExists(detailsFilePath, true)
	if err != nil || exists {
		return err
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
	return ioutil.WriteFile(detailsFilePath, []byte(content.String()), 0600)
}
