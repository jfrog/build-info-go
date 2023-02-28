package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	testdatautils "github.com/jfrog/build-info-go/build/testdata"
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
	projectPath := filepath.Join(testdataDir, "maven", "project")
	tmpProjectPath, cleanup := testdatautils.CreateTestProject(t, projectPath)
	defer cleanup()
	// Add maven project as module in build-info.
	mavenModule, err := mavenBuild.AddMavenModule(tmpProjectPath)
	assert.NoError(t, err)
	mavenModule.SetMavenGoals("compile", "--no-transfer-progress")
	// Calculate build-info.
	err = mavenModule.CalcDependencies()
	if assert.NoError(t, err) {
		buildInfo, err := mavenBuild.ToBuildInfo()
		assert.NoError(t, err)
		// Check build-info results.
		expectedModules := getExpectedMavenBuildInfo(t, filepath.Join(testdataDir, "maven", "expected_maven_buildinfo.json")).Modules
		match, err := entities.IsEqualModuleSlices(buildInfo.Modules, expectedModules)
		assert.NoError(t, err)
		if !match {
			testdatautils.PrintBuildInfoMismatch(t, expectedModules, buildInfo.Modules)
		}
	}
}

func getExpectedMavenBuildInfo(t *testing.T, filePath string) entities.BuildInfo {
	data, err := os.ReadFile(filePath)
	assert.NoError(t, err)
	var buildinfo entities.BuildInfo
	assert.NoError(t, json.Unmarshal(data, &buildinfo))
	return buildinfo
}

func TestExtractMavenPath(t *testing.T) {
	service := NewBuildInfoService()
	mavenBuild, err := service.GetOrCreateBuild("build-info-maven-test", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, mavenBuild.Clean())
	}()
	testdataDir, err := filepath.Abs(filepath.Join("testdata"))
	assert.NoError(t, err)
	// Create maven project
	projectPath := filepath.Join(testdataDir, "maven", "project")
	tmpProjectPath, cleanup := testdatautils.CreateTestProject(t, projectPath)
	defer cleanup()
	// Add maven project as module in build-info.
	mavenModule, err := mavenBuild.AddMavenModule(tmpProjectPath)
	assert.NoError(t, err)

	s1 := "Maven home: /test/is/good"
	s2 := "Home: /test/is/not/good"
	s3 := "Mvn Home:= /test/is/not/good"
	var output []string
	output = append(output, s1, s2, s3)
	mavenHome, err := mavenModule.ExtractMavenPath(output)
	assert.NoError(t, err)
	if utils.IsWindows() {
		assert.Equal(t, "/test/is/good/r", mavenHome)
	} else {
		assert.Equal(t, "/test/is/good", mavenHome)
	}
	output = nil
	output = append(output, s2, s1, s3)
	mavenHome, err = mavenModule.ExtractMavenPath(output)
	assert.NoError(t, err)
	if utils.IsWindows() {
		assert.Equal(t, "/test/is/good/r", mavenHome)
	} else {
		assert.Equal(t, "/test/is/good", mavenHome)
	}
	output = nil
	output = append(output, s2, s3)
	mavenHome, err = mavenModule.ExtractMavenPath(output)
	if utils.IsWindows() {
		assert.Equal(t, "/r", mavenHome)
	} else {
		assert.Equal(t, "", mavenHome)
	}
	assert.NoError(t, err)
}
