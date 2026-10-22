package utils

import (
	"fmt"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
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
		// Yarn Berry patch: protocol combined with a virtual package produces a locator with two '#' delimiters.
		// Only the virtual hash segment (up to the first '#') must be stripped; the rest is the actual map key.
		{"mobx-react@virtual:abc123def456#patch:mobx-react@npm:6.3.1#./patches/mobx-react.patch", "mobx-react@patch:mobx-react@npm:6.3.1#./patches/mobx-react.patch"},
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
		depName, err := yarnDep.Name()
		assert.NoError(t, err)
		assert.Equal(t, testCase.packageExpectedName, depName)
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

func TestIsYarnWorkspaceProject(t *testing.T) {
	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)

	testDataPath := filepath.Join("..", "testdata", "yarn")

	// v2 project is a plain (non-workspace) project — expects false.
	nonWorkspacePath := filepath.Join(testDataPath, "v2", "project")
	isWorkspace, err := IsYarnWorkspaceProject(executablePath, nonWorkspacePath)
	assert.NoError(t, err)
	assert.False(t, isWorkspace, "plain yarn project should not be detected as workspace")
}

func TestExtractLocatorProtocol(t *testing.T) {
	testCases := []struct {
		locator          string
		expectedProtocol string
	}{
		{"xml@npm:1.0.1", "npm"},
		{"pkg-a@workspace:packages/pkg-a", "workspace"},
		{"@scope/pkg@workspace:.", "workspace"},
		{"local-lib@link:./local-lib", "link"},
		{"filelib@file:./filelib.tgz", "file"},
		{"portallib@portal:./portallib", "portal"},
		{"left-pad@patch:left-pad@npm%3A1.3.0#./patch.patch", "patch"},
		{"left-pad@https://github.com/stevemao/left-pad.git#commit=abc", "https"},
		{"left-pad@git+https://github.com/stevemao/left-pad.git#commit=abc", "git+https"},
	}
	for _, tc := range testCases {
		assert.Equal(t, tc.expectedProtocol, extractLocatorProtocol(tc.locator), "locator: %s", tc.locator)
	}
}

func TestIsWorkspaceLocator(t *testing.T) {
	assert.True(t, IsWorkspaceLocator("pkg-a@workspace:packages/pkg-a"))
	assert.True(t, IsWorkspaceLocator("@scope/pkg@workspace:."))
	assert.True(t, IsWorkspaceLocator("root@workspace:."))
	assert.False(t, IsWorkspaceLocator("xml@npm:1.0.1"))
	assert.False(t, IsWorkspaceLocator("local-lib@link:./local-lib"))
	assert.False(t, IsWorkspaceLocator(""))
	assert.False(t, IsWorkspaceLocator("x"))
}

func TestIsNonRegistryLocator(t *testing.T) {
	registryCases := []string{
		"xml@npm:1.0.1",
		"@babel/highlight@npm:7.14.0",
	}
	for _, l := range registryCases {
		assert.False(t, IsNonRegistryLocator(l), "expected registry: %s", l)
	}

	nonRegistryCases := []string{
		"pkg-a@workspace:packages/pkg-a",
		"local-lib@link:./local-lib",
		"filelib@file:./filelib.tgz",
		"portallib@portal:./portallib",
		"left-pad@patch:left-pad@npm%3A1.3.0#./p.patch",
		"left-pad@git+https://github.com/stevemao/left-pad.git#commit=abc",
		"pkg@git+ssh://github.com/org/repo",
		"left-pad@https://github.com/stevemao/left-pad.git#commit=abc",
	}
	for _, l := range nonRegistryCases {
		assert.True(t, IsNonRegistryLocator(l), "expected non-registry: %s", l)
	}
}

func TestAddMissingYarnDependencyEntries(t *testing.T) {
	const patchLocator = "lodash@patch:lodash@npm%3A4.17.21#./.yarn/patches/lodash.patch::version=4.17.21&hash=abc123"
	const missingPatchLocator = "loose-envify@patch:loose-envify@npm%3A1.4.0#./missing.patch::version=1.4.0&hash=def456"
	dependenciesMap := map[string]*YarnDependency{
		"root@workspace:.": {Value: "root@workspace:.", Details: YarnDepDetails{Version: "1.0.0", Dependencies: []YarnDependencyPointer{
			// Resolves to "react@npm:18.2.0" through GetYarnDependencyKeyFromLocator: kept as is.
			{Locator: "react@virtual:abc123#npm:18.2.0"},
			// Has its own entry: kept as is.
			{Locator: patchLocator},
			// No entry of its own (e.g. a private package 'yarn info' couldn't resolve): an entry is added.
			{Locator: "@internal/private@npm:2.0.0"},
			// No entry and no version in the locator: removed.
			{Locator: "tool@git+https://example.com/tool.git#commit=abc"},
		}}},
		"react@npm:18.2.0": {Value: "react@npm:18.2.0", Details: YarnDepDetails{Version: "18.2.0", Dependencies: []YarnDependencyPointer{
			// No entry of its own: an entry is added with the version from the patch locator.
			{Locator: missingPatchLocator},
			// Missing child shared with another parent: added once.
			{Locator: "@internal/private@npm:2.0.0"},
		}}},
		patchLocator: {Value: patchLocator, Details: YarnDepDetails{Version: "4.17.21"}},
		"nil-entry":  nil,
	}

	addMissingYarnDependencyEntries(dependenciesMap, utils.NewDefaultLogger(utils.INFO))

	assert.Equal(t, []YarnDependencyPointer{{Locator: "react@virtual:abc123#npm:18.2.0"}, {Locator: patchLocator}, {Locator: "@internal/private@npm:2.0.0"}},
		dependenciesMap["root@workspace:."].Details.Dependencies)
	assert.Equal(t, &YarnDependency{Value: "@internal/private@npm:2.0.0", Details: YarnDepDetails{Version: "2.0.0"}}, dependenciesMap["@internal/private@npm:2.0.0"])
	assert.Equal(t, &YarnDependency{Value: missingPatchLocator, Details: YarnDepDetails{Version: "1.4.0"}}, dependenciesMap[missingPatchLocator])
	assert.Len(t, dependenciesMap, 6)
	// Every child pointer resolves to an entry, so callers can look children up without a nil check.
	for _, dependency := range dependenciesMap {
		if dependency == nil {
			continue
		}
		for _, child := range dependency.Details.Dependencies {
			assert.NotNil(t, dependenciesMap[GetYarnDependencyKeyFromLocator(child.Locator)], child.Locator)
		}
	}
}

func TestYarnLocatorVersion(t *testing.T) {
	cases := map[string]string{
		"express@npm:3.0.1":            "3.0.1",
		"@scope/name@npm:1.2.3-beta.1": "1.2.3-beta.1",
		"lodash@patch:lodash@npm%3A4.17.21#./p.patch::version=4.17.21&hash=abc": "4.17.21",
		"root@workspace:.":                   "",
		"tool@git+https://example.com/t.git": "",
		"local@file:../local":                "",
		"":                                   "",
	}
	for locator, expected := range cases {
		assert.Equal(t, expected, yarnLocatorVersion(locator), locator)
	}
}

// TestGetYarnDependenciesWithVersionAddsMissingEntries runs the Yarn 2+ path against a fake yarn whose
// 'yarn info' output lists a child ('@internal/private@npm:2.0.0') that has no entry of its own.
func TestGetYarnDependenciesWithVersionAddsMissingEntries(t *testing.T) {
	yarnPath := writeFakeYarn(t, yarnInfoWithMissingChild, 0)
	packageInfo := &PackageInfo{Name: "root", Version: "1.0.0"}
	dependenciesMap, root, err := GetYarnDependenciesWithVersion(yarnPath, t.TempDir(), packageInfo, "4.1.0", utils.NewDefaultLogger(utils.INFO), false)
	require.NoError(t, err)
	require.NotNil(t, root)
	assert.Len(t, root.Details.Dependencies, 2)
	assert.Len(t, dependenciesMap, 3)
	assert.Equal(t, "2.0.0", dependenciesMap["@internal/private@npm:2.0.0"].Details.Version)
}

// TestGetYarnDependenciesWithVersionFailsWhenYarnInfoFails verifies that the partial output of a failed
// 'yarn info' is not used, unless partial results are allowed.
func TestGetYarnDependenciesWithVersionFailsWhenYarnInfoFails(t *testing.T) {
	yarnPath := writeFakeYarn(t, yarnInfoWithMissingChild, 1)
	packageInfo := &PackageInfo{Name: "root", Version: "1.0.0"}

	dependenciesMap, root, err := GetYarnDependenciesWithVersion(yarnPath, t.TempDir(), packageInfo, "4.1.0", utils.NewDefaultLogger(utils.INFO), false)
	assert.ErrorContains(t, err, "'yarn info' failed")
	assert.Nil(t, dependenciesMap)
	assert.Nil(t, root)

	dependenciesMap, root, err = GetYarnDependenciesWithVersion(yarnPath, t.TempDir(), packageInfo, "4.1.0", utils.NewDefaultLogger(utils.INFO), true)
	require.NoError(t, err)
	require.NotNil(t, root)
	assert.Len(t, dependenciesMap, 3)
}

const yarnInfoWithMissingChild = `{"value":"root@workspace:.","children":{"Version":"0.0.0-use.local","Dependencies":[{"descriptor":"express@npm:^3.0.1","locator":"express@npm:3.0.1"},{"descriptor":"@internal/private@npm:^2.0.0","locator":"@internal/private@npm:2.0.0"}]}}
{"value":"express@npm:3.0.1","children":{"Version":"3.0.1"}}`

// writeFakeYarn writes a fake yarn executable that prints output and exits with exitCode.
func writeFakeYarn(t *testing.T, output string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as a fake yarn binary")
	}
	yarnPath := filepath.Join(t.TempDir(), "yarn")
	script := fmt.Sprintf("#!/bin/sh\ncat <<'EOF'\n%s\nEOF\nexit %d\n", output, exitCode)
	require.NoError(t, os.WriteFile(yarnPath, []byte(script), 0700)) // #nosec G306 -- test stub must be executable
	return yarnPath
}
