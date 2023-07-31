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

func TestGetYarnV2Dependencies(t *testing.T) {
	checkGetYarnDependencies(t, "v2", []string{"json@npm:9.0.6", "react@npm:18.2.0", "xml@npm:1.0.1"})
}

func TestBuildYarnV1Dependencies(t *testing.T) {
	checkGetYarnDependencies(t, "v1", []string{"json@9.0.6", "react@18.2.0", "xml@1.0.1"})
}

func TestGetYarnDependenciesUninstalled(t *testing.T) {
	checkGetYarnDependenciesUninstalled(t, "uninstalled-v2")
	checkGetYarnDependenciesUninstalled(t, "uninstalled-v3")

}

func checkGetYarnDependenciesUninstalled(t *testing.T, versionDir string) {
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}()
	testDataSource := filepath.Join("..", "testdata", "yarn", versionDir)
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)
	projectSrcPath := filepath.Join(testDataTarget, "project")
	pacInfo := PackageInfo{Name: "build-info-go-tests"}
	_, _, err = GetYarnDependencies(executablePath, projectSrcPath, &pacInfo, &utils.NullLog{})
	assert.Error(t, err)
}

func checkGetYarnDependencies(t *testing.T, versionDir string, expectedLocators []string) {
	// Copy the project directory to a temporary directory
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}()
	testDataSource := filepath.Join("..", "testdata", "yarn", versionDir)
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	// Collecting and creating arguments for the command
	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)
	projectSrcPath := filepath.Join(testDataTarget, "project")
	pacInfo := PackageInfo{
		Name:            "build-info-go-tests",
		Version:         "v1.0.0",
		Dependencies:    map[string]string{"react": "18.2.0", "xml": "1.0.1"},
		DevDependencies: map[string]string{"json": "9.0.6"},
	}
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
	assert.Len(t, dependenciesMap, 6)
	for dependencyName, depInfo := range dependenciesMap {
		splitDepName := strings.Split(dependencyName, "@")
		if len(splitDepName) != 2 {
			assert.Error(t, errors.New("Got an empty dependency name or in incorrect format ( expected: package-name@version ) "))
		}

		switch splitDepName[0] {
		case "react":
			assert.Equal(t, "18.2.0", depInfo.Details.Version)
			assert.NotNil(t, depInfo.Details.Dependencies)
			subDependencies := []string{"loose-envify"}
			for _, depPointer := range depInfo.Details.Dependencies {
				packageName := depPointer.Locator[:strings.Index(depPointer.Locator[1:], "@")+1]
				assert.Contains(t, subDependencies, packageName)
			}
		case "xml":
			assert.Equal(t, "1.0.1", depInfo.Details.Version)
			assert.Nil(t, depInfo.Details.Dependencies)
		case "json":
			assert.Equal(t, "9.0.6", depInfo.Details.Version)
			assert.Nil(t, depInfo.Details.Dependencies)
		case "loose-envify":
			assert.NotNil(t, depInfo.Details.Dependencies)
		case "js-tokens":
			assert.Nil(t, depInfo.Details.Dependencies)
		default:
			if dependencyName != root.Value {
				assert.Error(t, errors.New("Package "+dependencyName+" should not be inside the dependencyMap"))
			}
		}
	}
}
