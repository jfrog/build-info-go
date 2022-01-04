package build

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

func TestDownloadDependencies(t *testing.T) {
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath))
		assert.NoError(t, utils.CleanOldDirs())
	}()

	// Download JAR and create classworlds.conf
	err = downloadMavenExtractor(tempDirPath, nil, &utils.NullLog{})
	assert.NoError(t, err)

	// Make sure the Maven build-info extractor JAR and the classwords.conf file exist.
	expectedJarPath := filepath.Join(tempDirPath, fmt.Sprintf(MavenExtractorFileName, MavenExtractorDependencyVersion))
	assert.FileExists(t, expectedJarPath)
	expectedClasswordsPath := filepath.Join(tempDirPath, "classworlds.conf")
	assert.FileExists(t, expectedClasswordsPath)
}

func TestGenerateBuildInfoForMavenProject(t *testing.T) {
	service := NewBuildInfoService()
	mavenBuild, err := service.GetOrCreateBuild("build-info-maven-test", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, mavenBuild.Clean())
	}()
	testdataDir, err := filepath.Abs(filepath.Join("testdata"))
	assert.NoError(t, err)
	// Create maven project
	projectPath, cleanup := CopyProject(t, testdataDir)
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
	assert.True(t, entities.IsEqualModuleSlices(buildInfo.Modules, getExpectedMavenBuildInfo(t, filepath.Join(testdataDir, "maven", "expected_maven_buildinfo.json")).Modules))
}

func CopyProject(t *testing.T, testdataDir string) (string, func()) {
	projectRoot := filepath.Join(testdataDir, "maven", "project")
	path, err := utils.CreateTempDir()
	assert.NoError(t, err)
	err = utils.CopyDir(projectRoot, path, true, nil)
	assert.NoError(t, err)

	return path, func() {
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
