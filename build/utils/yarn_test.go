package utils

import (
	"errors"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetYarnDependencyKeyFromLocator(t *testing.T) {
	testCases := []struct {
		yarnDepLocator string
		expectedDepKey string
	}{
		{"camelcase@npm:6.2.0", "camelcase@npm:6.2.0"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight@npm:7.14.0"},
		{"fsevents@patch:fsevents@npm%3A2.3.2#builtin<compat/fsevents>::version=2.3.2&hash=11e9ea", "fsevents@patch:fsevents@npm%3A2.3.2#builtin<compat/fsevents>::version=2.3.2&hash=11e9ea"},
		{"follow-redirects@virtual:c192f6b3b32cd5d11a443145a3883a70c04cbd7c813b53085dbaf50263735f1162f10fdbddd53c24e162ec3bc#npm:1.14.1", "follow-redirects@npm:1.14.1"},
	}

	for _, testCase := range testCases {
		assert.Equal(t, testCase.expectedDepKey, GetYarnDependencyKeyFromLocator(testCase.yarnDepLocator))
	}
}

func TestGetYarnDependenciesV2(t *testing.T) {
	// This test creates and tests a yarn project with yarn version of 3.1.1
	CheckGetYarnDependencies(t, "v2", []string{"json@npm:9.0.6", "react@npm:18.2.0", "xml@npm:1.0.1"})
}

func TestBuildYarnDependenciesV1(t *testing.T) {
	// This test creates and tests a yarn project with yarn version of 1.22.19
	CheckGetYarnDependencies(t, "v1", []string{"json@9.0.6", "react@18.2.0", "xml@1.0.1"})
}

func CheckGetYarnDependencies(t *testing.T, versionDir string, expectedLocators []string) {
	// Copy the project directory to a temporary directory
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}()
	testDataSource := filepath.Join("..", "testdata", "yarn", versionDir)
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	// collecting and creating arguments for command
	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)
	projectSrcPath := filepath.Join(testDataTarget, "project")
	pacInfo := PackageInfo{Name: "build-info-go-tests", Version: "v1.0.0", Dependencies: make(map[string]string), DevDependencies: make(map[string]string)}
	pacInfo.Dependencies["react"] = "18.2.0"
	pacInfo.Dependencies["xml"] = "1.0.1"
	pacInfo.DevDependencies["json"] = "9.0.6"
	dependenciesMap, root, err := GetYarnDependencies(executablePath, projectSrcPath, &pacInfo, &utils.NullLog{})

	// general checks
	assert.NoError(t, err)
	assert.NotNil(t, root)

	// checking root
	assert.True(t, strings.HasPrefix(root.Value, "build-info-go-tests"))
	assert.Equal(t, "v1.0.0", root.Details.Version)
	for _, dependency := range root.Details.Dependencies {
		assert.Contains(t, expectedLocators, dependency.Locator)
	}

	// checking dependencyMap
	// NOTICE: the test uses fixed package versions. if for some reason dependencies in those packages (with those versions) changes
	// (i.e. new dependencies added) make sure to fix the checks below
	assert.Len(t, dependenciesMap, 6)
	for key, val := range dependenciesMap {
		if strings.HasPrefix(key, "react") {
			assert.Equal(t, val.Details.Version, "18.2.0")
			assert.True(t, val.Details.Dependencies != nil)
			subDependencies := []string{"loose-envify"}
			for _, depPointer := range val.Details.Dependencies {
				packageName := depPointer.Locator[:strings.Index(depPointer.Locator[1:], "@")+1]
				assert.Contains(t, subDependencies, packageName)
			}
		} else if strings.HasPrefix(key, "xml") {
			assert.Equal(t, val.Details.Version, "1.0.1")
			assert.True(t, val.Details.Dependencies == nil)
		} else if strings.HasPrefix(key, "json") {
			assert.Equal(t, val.Details.Version, "9.0.6")
			assert.True(t, val.Details.Dependencies == nil)
		} else if strings.HasPrefix(key, "loose-envify") {
			assert.True(t, val.Details.Dependencies != nil)
		} else if strings.HasPrefix(key, "js-tokens") {
			assert.True(t, val.Details.Dependencies == nil)
		} else {
			if key != root.Value {
				assert.Error(t, errors.New("Package"+key+"should not be inside the dependencyMap"))
			}
		}
	}
}
