package build

import (
	buildutils "github.com/jfrog/build-info-go/utils"
	"os"
	"path/filepath"
)

const BuildsTempPath = "jfrog/builds/"

type BuildInfoService struct {
	tempDirPath string
	logger      buildutils.Log
}

func NewBuildInfoService() *BuildInfoService {
	return &BuildInfoService{tempDirPath: filepath.Join(os.TempDir(), BuildsTempPath), logger: &buildutils.NullLog{}}
}

func (bis *BuildInfoService) SetTempDirPath(tempDirPath string) {
	bis.tempDirPath = tempDirPath
}

func (bis *BuildInfoService) SetLogger(logger buildutils.Log) {
	bis.logger = logger
}

func (bis *BuildInfoService) GetOrCreateBuild(buildName, buildNumber string) (*Build, error) {
	return bis.GetOrCreateBuildWithProject(buildName, buildNumber, "")
}

func (bis *BuildInfoService) GetOrCreateBuildWithProject(buildName, buildNumber, projectKey string) (*Build, error) {
	err := buildutils.SaveBuildGeneralDetails(buildName, buildNumber, projectKey, bis.tempDirPath, bis.logger)
	if err != nil {
		return nil, err
	}
	return NewBuild(buildName, buildNumber, projectKey, bis.tempDirPath, bis.logger), nil
}
