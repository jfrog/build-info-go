package tests

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBuildInfoForMavenProject(t *testing.T) {
	service := build.NewBuildInfoService()
	mavenBuild, err := service.GetOrCreateBuild("build-info-maven-test", "1")
	assert.NoError(t, err)
	// Create maven project
	projectPath, err, cleanup := CopyProject(t)
	assert.NoError(t, err)
	defer cleanup()
	// Add maven project as module in build-info.
	mavenModule, err := mavenBuild.AddMavenModule(projectPath)
	assert.NoError(t, err)
	// Calculate build-info.
	err = mavenModule.CalcDependencies()
	assert.NoError(t, err)
	buildInfo, err := mavenBuild.ToBuildInfo()
	assert.NoError(t, err)
	// Check build-info results.
	assert.True(t, entities.IsEqualModuleSlices(buildInfo.Modules, getExpectedMavenBuildInfo(t, filepath.Join("testdata", "maven", "expected_maven_buildinfo.json")).Modules))
}

func CopyProject(t *testing.T) (string, error, func()) {
	projectRoot := filepath.Join("testdata", "maven", "project")
	path, err := utils.CreateTempDir()
	assert.NoError(t, err)

	return path, utils.CopyDir(projectRoot, path, true, nil), func() {
		assert.NoError(t, utils.RemoveTempDir(path))
	}
}

func getExpectedMavenBuildInfo(t *testing.T, filePath string) entities.BuildInfo {
	data, err := ioutil.ReadFile(filePath)
	assert.NoError(t, err)
	var buildinfo entities.BuildInfo
	assert.NoError(t, json.Unmarshal(data, &buildinfo))
	return buildinfo
}
