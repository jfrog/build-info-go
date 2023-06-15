package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	testdatautils "github.com/jfrog/build-info-go/build/testdata"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"os"
	"path/filepath"
	"testing"

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
	expectedJarPath := filepath.Join(tempDirPath, fmt.Sprintf(MavenExtractorFileName, MavenExtractorDependencyVersion()))
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

	testdataDir, err := filepath.Abs("testdata")
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
	if err != nil {
		// Maven Central sometimes cause that test to fail the maven compile command, so we try running it again to avoid flaky test
		err = mavenModule.CalcDependencies()
	}
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
	// Create mock mavenBuild
	service := NewBuildInfoService()
	mavenBuild, err := service.GetOrCreateBuild("", "")
	assert.NoError(t, err)
	mavenModule, err := mavenBuild.AddMavenModule("")
	assert.NoError(t, err)

	allTests := []struct {
		mavenVersionResultFirstLine  string
		mavenVersionResultSecondLine string
		mavenVersionResultThirdLine  string
	}{
		{"Maven home: /Program Files/Maven/apache-maven-3.9.1", "Home: /test/is/not/good", "Mvn Home:= /test/is/not/good"},
		{"Home: /test/is/not/good", "Maven home: /Program Files/Maven/apache-maven-3.9.1", "Mvn Home:= /test/is/not/good"},
		{"Mvn Home:= /test/is/not/good", "Home: /test/is/not/good", "Maven home: /Program Files/Maven/apache-maven-3.9.1"},
	}

	for _, test := range allTests {
		var mavenVersionFullResult []string
		mavenVersionFullResult = append(mavenVersionFullResult, test.mavenVersionResultFirstLine, test.mavenVersionResultSecondLine, test.mavenVersionResultThirdLine)
		data1 := bytes.Buffer{}
		for _, i := range mavenVersionFullResult {
			data1.WriteString(i)
			data1.WriteString("\n")
		}
		mavenHome, err := mavenModule.extractMavenPath(data1)
		assert.NoError(t, err)
		if utils.IsWindows() {
			assert.Equal(t, "D:\\Program Files\\Maven\\apache-maven-3.9.1", mavenHome)
		} else {
			assert.Equal(t, "/Program Files/Maven/apache-maven-3.9.1", mavenHome)
		}
	}
}

func TestGetExecutableName(t *testing.T) {
	// Add maven project as module in build-info.
	mavenModule := MavenModule{extractorDetails: &extractorDetails{useWrapper: true}}
	mvnHome, err := mavenModule.getExecutableName()
	assert.NoError(t, err)
	if !utils.IsWindows() {
		assert.Equal(t, "./mvnw", mvnHome)
	} else {
		result, err := filepath.Abs("mvnw.cmd")
		assert.NoError(t, err)
		assert.Equal(t, result, mvnHome)
	}
}
