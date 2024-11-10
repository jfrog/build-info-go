package utils

import (
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"sort"
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
	dependenciesMap, root, err := GetYarnDependencies(executablePath, projectSrcPath, &pacInfo, &utils.NullLog{}, false)
	assert.NoError(t, err)
	assert.NotNil(t, root)

	// Checking dependencyMap
	assert.Len(t, dependenciesMap, 6)
	for dependencyName, depInfo := range dependenciesMap {
		var packageCleanName, packageVersion string
		if dependencyName != root.Value {
			packageCleanName, packageVersion, err = splitNameAndVersion(dependencyName)
			assert.NoError(t, err)
			if packageCleanName == "" || packageVersion == "" {
				t.Error("got an empty dependency name/version or in incorrect format (expected: package-name@version) ")
			}
		} else {
			packageCleanName = root.Value
		}

		switch packageCleanName {
		case "react":
			assert.Equal(t, "18.2.0", depInfo.Details.Version)
			assert.NotNil(t, depInfo.Details.Dependencies)
			subDependencies := []string{"loose-envify"}
			for _, depPointer := range depInfo.Details.Dependencies {
				packageName, _, err := splitNameAndVersion(depPointer.Locator)
				assert.NoError(t, err)
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
			assert.Len(t, depInfo.Details.Dependencies, 1)
		case "js-tokens":
			assert.Nil(t, depInfo.Details.Dependencies)
		case root.Value:
			assert.True(t, strings.HasPrefix(root.Value, "build-info-go-tests"))
			assert.Equal(t, "v1.0.0", root.Details.Version)
			for _, dependency := range root.Details.Dependencies {
				assert.Contains(t, expectedLocators, dependency.Locator)
			}
		default:
			t.Error("package '" + dependencyName + "' should not be inside the dependencies map")
		}
	}
}

// This test checks the error handling of buildYarnV1DependencyMap with a response string that is missing a dependency, when allow-partial-results is set to true.
// responseStr, which is an output of 'yarn list' should contain every dependency (direct and indirect) of the project at the first level, with the direct children of each dependency.
// Sometimes the first level is lacking a dependency that appears as a child, or the child dependency is not found at the first level of the map, hence an error should be thrown.
// When apply-partial-results is set to true we expect to provide a partial map instead of dropping the entire flow and return an error in such a case.
func TestBuildYarnV1DependencyMapWithLackingDependencyInResponseString(t *testing.T) {
	packageInfo := &PackageInfo{
		Name:         "test-project",
		Version:      "1.0.0",
		Dependencies: map[string]string{"minimist": "1.2.5", "yarn-inner": "file:./yarn-inner"},
	}

	// This responseStr simulates should trigger an error since it is missing 'tough-cookie' at the "trees" first level, but this dependency appears as a child for another dependency (and hence should have been in the "trees" level as well)
	responseStr := "{\"type\":\"tree\",\"data\":{\"type\":\"list\",\"trees\":[{\"name\":\"minimist@1.2.5\",\"children\":[],\"hint\":null,\"color\":\"bold\",\"depth\":0},{\"name\":\"yarn-inner@1.0.0\",\"children\":[{\"name\":\"tough-cookie@2.5.0\",\"color\":\"dim\",\"shadow\":true}],\"hint\":null,\"color\":\"bold\",\"depth\":0}]}}"

	expectedRoot := YarnDependency{
		Value: "test-project",
		Details: YarnDepDetails{
			Version: "1.0.0",
			Dependencies: []YarnDependencyPointer{
				{
					Descriptor: "",
					Locator:    "minimist@1.2.5",
				},
				{
					Descriptor: "",
					Locator:    "yarn-inner@1.0.0",
				},
			},
		},
	}

	expectedDependenciesMap := map[string]*YarnDependency{
		"minimist@1.2.5": {
			Value: "minimist@1.2.5",
			Details: YarnDepDetails{
				Version:      "1.2.5",
				Dependencies: nil,
			},
		},
		"yarn-inner@1.0.0": {
			Value: "yarn-inner@1.0.0",
			Details: YarnDepDetails{
				Version:      "1.0.0",
				Dependencies: nil,
			},
		},
		"test-project": {
			Value: "test-project",
			Details: YarnDepDetails{
				Version: "1.0.0",
				Dependencies: []YarnDependencyPointer{
					{
						Descriptor: "",
						Locator:    "minimist@1.2.5",
					},
					{
						Descriptor: "",
						Locator:    "yarn-inner@1.0.0",
					},
				},
			},
		},
	}

	dependenciesMap, root, err := buildYarnV1DependencyMap(packageInfo, responseStr, true, &utils.NullLog{})
	assert.NoError(t, err)
	// Verifying root
	assert.NotNil(t, root)
	assert.Equal(t, expectedRoot.Value, root.Value)
	assert.Len(t, root.Details.Dependencies, len(expectedRoot.Details.Dependencies))
	sort.Slice(root.Details.Dependencies, func(i, j int) bool {
		return root.Details.Dependencies[i].Locator < root.Details.Dependencies[j].Locator
	})
	assert.EqualValues(t, expectedRoot.Details.Dependencies, root.Details.Dependencies)

	// Verifying dependencies map
	assert.Equal(t, len(expectedDependenciesMap), len(dependenciesMap))
	for expectedKey, expectedValue := range expectedDependenciesMap {
		value := dependenciesMap[expectedKey]
		assert.NotNil(t, value)
		assert.EqualValues(t, expectedValue.Value, value.Value)
		assert.EqualValues(t, expectedValue.Details.Version, value.Details.Version)
		if expectedValue.Details.Dependencies != nil {
			sort.Slice(value.Details.Dependencies, func(i, j int) bool {
				return value.Details.Dependencies[i].Locator < value.Details.Dependencies[j].Locator
			})
			assert.EqualValues(t, expectedValue.Details.Dependencies, value.Details.Dependencies)
		}
	}
}

func TestYarnDependency_Name(t *testing.T) {
	testCases := []struct {
		packageFullName     string
		packageExpectedName string
	}{
		{"json@1.2.3", "json"},
		{"@babel/highlight@7.14.0", "@babel/highlight"},
		{"json@npm:1.2.3", "json"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight"},
	}
	for _, testCase := range testCases {
		yarnDep := YarnDependency{Value: testCase.packageFullName}
		assert.Equal(t, testCase.packageExpectedName, yarnDep.Name())
	}
}

func TestSplitNameAndVersion(t *testing.T) {
	testCases := []struct {
		packageFullName string
		expectedName    string
		expectedVersion string
	}{
		{"json@1.2.3", "json", "1.2.3"},
		{"@babel/highlight@7.14.0", "@babel/highlight", "7.14.0"},
		{"json@npm:1.2.3", "json", "1.2.3"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight", "7.14.0"},
	}
	for _, testCase := range testCases {
		packageCleanName, packageVersion, err := splitNameAndVersion(testCase.packageFullName)
		assert.NoError(t, err)
		assert.Equal(t, testCase.expectedName, packageCleanName)
		assert.Equal(t, testCase.expectedVersion, packageVersion)
	}

	incorrectFormatPackageName := "json:1.2.3"
	_, _, err := splitNameAndVersion(incorrectFormatPackageName)
	assert.Error(t, err)
}
