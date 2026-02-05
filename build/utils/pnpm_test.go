package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfrog/gofrog/crypto"

	"github.com/stretchr/testify/require"

	"github.com/jfrog/build-info-go/tests"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var pnpmLogger = utils.NewDefaultLogger(utils.INFO)

func TestGetPnpmVersionAndExecPath(t *testing.T) {
	// This test requires pnpm to be installed
	version, execPath, err := GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}
	assert.NotNil(t, version)
	assert.NotEmpty(t, execPath)
	assert.True(t, version.AtLeast("6.0.0"), "pnpm version should be at least 6.0.0")
}

func TestGetPnpmVersion(t *testing.T) {
	_, execPath, err := GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}
	version, err := GetPnpmVersion(execPath, pnpmLogger)
	assert.NoError(t, err)
	assert.NotNil(t, version)
}

func TestRunPnpmCmd(t *testing.T) {
	_, execPath, err := GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}
	stdout, stderr, err := RunPnpmCmd(execPath, "", []string{"--version"}, pnpmLogger)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.NotEmpty(t, stdout)
}

func TestAppendPnpmCommand(t *testing.T) {
	testcases := []struct {
		args     []string
		command  string
		expected []string
	}{
		{[]string{"--json", "--long"}, "ls", []string{"ls", "--json", "--long"}},
		{[]string{}, "install", []string{"install"}},
		{[]string{"--prod"}, "install", []string{"install", "--prod"}},
	}
	for _, tc := range testcases {
		result := AppendPnpmCommand(tc.args, tc.command)
		assert.Equal(t, tc.expected, result)
	}
}

func TestAppendPnpmScopes(t *testing.T) {
	var scopes = []struct {
		a        []string
		b        []string
		expected []string
	}{
		{[]string{"item"}, []string{}, []string{"item"}},
		{[]string{"item"}, []string{""}, []string{"item"}},
		{[]string{}, []string{"item"}, []string{"item"}},
		{[]string{"item1"}, []string{"item2"}, []string{"item1", "item2"}},
		{[]string{"item"}, []string{"item"}, []string{"item"}},
		{[]string{"item1", "item2"}, []string{"item2"}, []string{"item1", "item2"}},
		{[]string{"item1"}, []string{"item2", "item1"}, []string{"item1", "item2"}},
		{[]string{"item1", "item1"}, []string{"item2"}, []string{"item1", "item2"}},
		{[]string{"item1"}, []string{"item2", "item2"}, []string{"item1", "item2"}},
	}
	for _, v := range scopes {
		result := appendPnpmScopes(v.a, v.b)
		assert.ElementsMatch(t, result, v.expected, "appendPnpmScopes(\"%s\",\"%s\") => '%s', want '%s'", v.a, v.b, result, v.expected)
	}
}

func TestFilterPnpmUniqueArgs(t *testing.T) {
	var testcases = []struct {
		argsToFilter   []string
		alreadyExists  []string
		expectedResult []string
	}{
		{
			argsToFilter:   []string{"install"},
			alreadyExists:  []string{},
			expectedResult: nil,
		},
		{
			argsToFilter:   []string{"install", "--flagA"},
			alreadyExists:  []string{"--flagA"},
			expectedResult: nil,
		},
		{
			argsToFilter:   []string{"install", "--flagA", "--flagB"},
			alreadyExists:  []string{"--flagA"},
			expectedResult: []string{"--flagB"},
		},
		{
			argsToFilter:   []string{"install", "--flagA", "--flagB"},
			alreadyExists:  []string{"--flagA", "--flagC"},
			expectedResult: []string{"--flagB"},
		},
	}

	for _, testcase := range testcases {
		assert.Equal(t, testcase.expectedResult, filterPnpmUniqueArgs(testcase.argsToFilter, testcase.alreadyExists))
	}
}

func TestParsePnpmDependencies(t *testing.T) {
	dependenciesJsonList, err := os.ReadFile(filepath.Join("..", "testdata", "pnpm", "dependenciesList.json"))
	if err != nil {
		t.Error(err)
	}

	expectedDependenciesList := []struct {
		Key        string
		pathToRoot [][]string
	}{
		{"underscore:1.13.6", [][]string{{"root"}}},
		{"minimist:1.2.8", [][]string{{"root"}}},
		{"@jfrog/test-package:1.0.0", [][]string{{"root"}}},
		{"nested-dep:2.0.0", [][]string{{"@jfrog/test-package:1.0.0", "root"}}},
	}
	dependencies := make(map[string]*pnpmDependencyInfo)
	err = parsePnpmDependencies(dependenciesJsonList, []string{"root"}, dependencies, utils.NewDefaultLogger(utils.INFO))
	assert.NoError(t, err)
	assert.Equal(t, len(expectedDependenciesList), len(dependencies))
	for _, eDependency := range expectedDependenciesList {
		found := false
		for aDependency, v := range dependencies {
			if aDependency == eDependency.Key && assert.ElementsMatch(t, v.RequestedBy, eDependency.pathToRoot) {
				found = true
				break
			}
		}
		assert.True(t, found, "The expected dependency:", eDependency, "is missing from the actual dependencies list:\n", dependencies)
	}
}

func TestPnpmLsDependencyScopes(t *testing.T) {
	testcases := []struct {
		name           string
		dep            *pnpmLsDependency
		expectedScopes []string
	}{
		{
			name:           "Production dependency",
			dep:            &pnpmLsDependency{Name: "lodash", Dev: false},
			expectedScopes: []string{"prod"},
		},
		{
			name:           "Dev dependency",
			dep:            &pnpmLsDependency{Name: "jest", Dev: true},
			expectedScopes: []string{"dev"},
		},
		{
			name:           "Scoped package production",
			dep:            &pnpmLsDependency{Name: "@types/node", Dev: false},
			expectedScopes: []string{"prod"},
		},
		{
			name:           "Scoped package dev",
			dep:            &pnpmLsDependency{Name: "@types/jest", Dev: true},
			expectedScopes: []string{"dev"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			scopes := tc.dep.getScopes()
			assert.Equal(t, tc.expectedScopes, scopes)
		})
	}
}

func TestPnpmLsDependencyId(t *testing.T) {
	dep := &pnpmLsDependency{Name: "lodash", Version: "4.17.21"}
	assert.Equal(t, "lodash:4.17.21", dep.id())

	scopedDep := &pnpmLsDependency{Name: "@types/node", Version: "18.0.0"}
	assert.Equal(t, "@types/node:18.0.0", scopedDep.id())
}

func TestParsePnpmDependenciesEdgeCases(t *testing.T) {
	testcases := []struct {
		name             string
		inputJson        string
		expectedId       string
		shouldBeSkipped  bool
		expectParseError bool
	}{
		{
			name:             "Git URL with hash in resolved",
			inputJson:        `{"my-git-pkg":{"resolved": "git+ssh://git@github.com/user/repo.git#abc123def"}}`,
			expectedId:       "my-git-pkg:abc123def",
			shouldBeSkipped:  false,
			expectParseError: false,
		},
		{
			name:      "Git URL without hash in resolved",
			inputJson: `{"my-pkg":{"resolved": "git+https://github.com/user/repo.git"}}`,
			expectedId: func() string {
				checksums, _ := crypto.CalcChecksums(strings.NewReader("git+https://github.com/user/repo.git"), crypto.SHA1)
				return "my-pkg:" + checksums[crypto.SHA1]
			}(),
			shouldBeSkipped:  false,
			expectParseError: false,
		},
		{
			name:      "Local file path in path field",
			inputJson: `{"my-local-pkg":{"path": "/local/path/to/pkg"}}`,
			expectedId: func() string {
				checksums, _ := crypto.CalcChecksums(strings.NewReader("/local/path/to/pkg"), crypto.SHA1)
				return "my-local-pkg:" + checksums[crypto.SHA1]
			}(),
			shouldBeSkipped:  false,
			expectParseError: false,
		},
		{
			name:             "Missing dependency",
			inputJson:        `{"bad-pkg":{"missing": true}}`,
			shouldBeSkipped:  true,
			expectParseError: false,
		},
		{
			name:             "No version and no resolved, not missing",
			inputJson:        `{"bad-pkg":{"dev": true}}`,
			shouldBeSkipped:  false,
			expectParseError: true,
		},
		{
			name:             "Regular dependency",
			inputJson:        `{"react":{"version": "18.2.0", "integrity": "sha512-..."}}`,
			expectedId:       "react:18.2.0",
			shouldBeSkipped:  false,
			expectParseError: false,
		},
		{
			name:             "Empty object should be skipped",
			inputJson:        `{"optional-pkg":{}}`,
			shouldBeSkipped:  true,
			expectParseError: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			depsMap := make(map[string]*pnpmDependencyInfo)
			err := parsePnpmDependencies([]byte(tc.inputJson), []string{"root"}, depsMap, &utils.NullLog{})

			if tc.expectParseError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			if tc.shouldBeSkipped {
				assert.Empty(t, depsMap, "Expected dependency to be skipped, but it was added")
			} else {
				assert.Len(t, depsMap, 1, "Expected exactly one dependency")
				depInfo, ok := depsMap[tc.expectedId]
				assert.True(t, ok, "Expected dependency ID '%s' not found in map", tc.expectedId)
				if ok {
					assert.Equal(t, tc.expectedId, depInfo.Id, "Dependency ID mismatch")
				}
			}
		})
	}
}

func TestExtractPnpmLsData(t *testing.T) {
	// Test with array format (pnpm ls --json output)
	arrayJson := `[{"name": "test", "version": "1.0.0", "dependencies": {"lodash": {"version": "4.17.21"}}}]`
	result := extractPnpmLsData([]byte(arrayJson), &utils.NullLog{})
	assert.NotNil(t, result)
	assert.Contains(t, string(result), "dependencies")

	// Test with empty array
	emptyArrayJson := `[]`
	result = extractPnpmLsData([]byte(emptyArrayJson), &utils.NullLog{})
	assert.Nil(t, result)

	// Test with object format (fallback)
	objectJson := `{"name": "test", "dependencies": {"lodash": {"version": "4.17.21"}}}`
	result = extractPnpmLsData([]byte(objectJson), &utils.NullLog{})
	assert.NotNil(t, result)

	// Test with empty data
	result = extractPnpmLsData([]byte{}, &utils.NullLog{})
	assert.Nil(t, result)
}

func TestCalculatePnpmDependenciesMapWithProhibitedInstallation(t *testing.T) {
	path, cleanup := tests.CreateTestProject(t, filepath.Join("..", "testdata", "pnpm", "noBuildProject"))
	defer cleanup()

	_, execPath, err := GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	dependencies, err := CalculatePnpmDependenciesMap(execPath, path, "jfrogtest",
		PnpmTreeDepListParam{Args: []string{}, IgnoreNodeModules: false, OverwritePackageLock: false}, pnpmLogger, true)

	assert.Nil(t, dependencies)
	assert.Error(t, err)
	var installForbiddenErr *utils.ErrProjectNotInstalled
	assert.True(t, errors.As(err, &installForbiddenErr))
}

func TestPnpmDependenciesListIntegration(t *testing.T) {
	_, execPath, err := GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)

	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	// Install dependencies
	_, _, err = RunPnpmCmd(execPath, projectPath, []string{"install"}, pnpmLogger)
	assert.NoError(t, err)

	// Calculate dependencies
	dependencies, err := CalculatePnpmDependenciesList(execPath, projectPath, "build-info-go-pnpm-tests",
		PnpmTreeDepListParam{Args: []string{}}, false, pnpmLogger)
	assert.NoError(t, err)

	// Should have at least the direct dependencies
	assert.Greater(t, len(dependencies), 0, "Error: dependencies are not found!")

	// Check for known dependencies
	foundUnderscore := false
	foundMinimist := false
	for _, dep := range dependencies {
		if strings.HasPrefix(dep.Id, "underscore:") {
			foundUnderscore = true
		}
		if strings.HasPrefix(dep.Id, "minimist:") {
			foundMinimist = true
		}
	}
	assert.True(t, foundUnderscore, "Expected to find underscore dependency")
	assert.True(t, foundMinimist, "Expected to find minimist dependency")
}

func TestPnpmDependenciesWithoutNodeModules(t *testing.T) {
	_, execPath, err := GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)

	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	// Install to create lock file, then remove node_modules
	_, _, err = RunPnpmCmd(execPath, projectPath, []string{"install"}, pnpmLogger)
	assert.NoError(t, err)

	// Remove node_modules
	require.NoError(t, utils.RemoveTempDir(filepath.Join(projectPath, "node_modules")))

	// Calculate dependencies should still work using lock file
	dependencies, err := CalculatePnpmDependenciesList(execPath, projectPath, "build-info-go-pnpm-tests",
		PnpmTreeDepListParam{Args: []string{}}, false, pnpmLogger)
	assert.NoError(t, err)
	assert.Greater(t, len(dependencies), 0, "Error: dependencies are not found!")
}

func TestPnpmTreeDepListParam(t *testing.T) {
	param := PnpmTreeDepListParam{
		Args:                 []string{"--prod"},
		InstallCommandArgs:   []string{"install", "--frozen-lockfile"},
		IgnoreNodeModules:    true,
		OverwritePackageLock: false,
	}

	assert.Equal(t, []string{"--prod"}, param.Args)
	assert.Equal(t, []string{"install", "--frozen-lockfile"}, param.InstallCommandArgs)
	assert.True(t, param.IgnoreNodeModules)
	assert.False(t, param.OverwritePackageLock)
}

func TestCheckIfPnpmLockFileShouldBeUpdated(t *testing.T) {
	tempDir, cleanup := tests.CreateTempDirWithCallbackAndAssert(t)
	defer cleanup()

	// Create package.json
	packageJsonPath := filepath.Join(tempDir, "package.json")
	assert.NoError(t, os.WriteFile(packageJsonPath, []byte(`{"name": "test"}`), 0644))

	// Test when lock file doesn't exist
	result := checkIfPnpmLockFileShouldBeUpdated(tempDir, &utils.NullLog{})
	assert.False(t, result)

	// Create pnpm-lock.yaml
	lockFilePath := filepath.Join(tempDir, "pnpm-lock.yaml")
	assert.NoError(t, os.WriteFile(lockFilePath, []byte("lockfileVersion: 5.4"), 0644))

	// Test when lock file is newer
	result = checkIfPnpmLockFileShouldBeUpdated(tempDir, &utils.NullLog{})
	assert.False(t, result)
}

func TestIsPnpmInstallRequired(t *testing.T) {
	tempDir, cleanup := tests.CreateTempDirWithCallbackAndAssert(t)
	defer cleanup()

	// Create package.json
	packageJsonPath := filepath.Join(tempDir, "package.json")
	assert.NoError(t, os.WriteFile(packageJsonPath, []byte(`{"name": "test"}`), 0644))

	// Test when lock file doesn't exist and skipInstall is false
	required, err := isPnpmInstallRequired(tempDir, PnpmTreeDepListParam{}, &utils.NullLog{}, false)
	assert.NoError(t, err)
	assert.True(t, required)

	// Test when lock file doesn't exist and skipInstall is true
	_, err = isPnpmInstallRequired(tempDir, PnpmTreeDepListParam{}, &utils.NullLog{}, true)
	assert.Error(t, err)
	var installForbiddenErr *utils.ErrProjectNotInstalled
	assert.True(t, errors.As(err, &installForbiddenErr))

	// Create pnpm-lock.yaml
	lockFilePath := filepath.Join(tempDir, "pnpm-lock.yaml")
	assert.NoError(t, os.WriteFile(lockFilePath, []byte("lockfileVersion: 5.4"), 0644))

	// Test when lock file exists
	required, err = isPnpmInstallRequired(tempDir, PnpmTreeDepListParam{}, &utils.NullLog{}, false)
	assert.NoError(t, err)
	assert.False(t, required)

	// Test when InstallCommandArgs are provided
	required, err = isPnpmInstallRequired(tempDir, PnpmTreeDepListParam{InstallCommandArgs: []string{"install"}}, &utils.NullLog{}, false)
	assert.NoError(t, err)
	assert.True(t, required)
}

func TestAppendPnpmDependency(t *testing.T) {
	dependencies := make(map[string]*pnpmDependencyInfo)

	dep1 := &pnpmLsDependency{
		Name:      "lodash",
		Version:   "4.17.21",
		Integrity: "sha512-test",
		Dev:       false,
	}

	// Add first dependency
	appendPnpmDependency(dependencies, dep1, []string{"root"})
	assert.Len(t, dependencies, 1)
	assert.Equal(t, "lodash:4.17.21", dependencies["lodash:4.17.21"].Id)
	assert.Equal(t, []string{"prod"}, dependencies["lodash:4.17.21"].Scopes)
	assert.Equal(t, [][]string{{"root"}}, dependencies["lodash:4.17.21"].RequestedBy)

	// Add same dependency from different path
	appendPnpmDependency(dependencies, dep1, []string{"other-pkg", "root"})
	assert.Len(t, dependencies, 1)
	assert.Equal(t, [][]string{{"root"}, {"other-pkg", "root"}}, dependencies["lodash:4.17.21"].RequestedBy)

	// Add different dependency
	dep2 := &pnpmLsDependency{
		Name:    "jest",
		Version: "29.0.0",
		Dev:     true,
	}
	appendPnpmDependency(dependencies, dep2, []string{"root"})
	assert.Len(t, dependencies, 2)
	assert.Equal(t, []string{"dev"}, dependencies["jest:29.0.0"].Scopes)
}

func TestParsePnpmLsDependency(t *testing.T) {
	jsonData := `{
		"version": "4.17.21",
		"resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz",
		"integrity": "sha512-v2kDEe57lecTulaDIuNTPy3Ry4gLGJ6Z1O3vE1krgXZNrsQ+LFTGHVxVjcXPs17LhbZVGedAJv8XZ1tvj5FvSg==",
		"dev": false,
		"optional": false
	}`

	dep, err := parsePnpmLsDependency([]byte(jsonData))
	assert.NoError(t, err)
	assert.Equal(t, "4.17.21", dep.Version)
	assert.Equal(t, "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz", dep.Resolved)
	assert.Equal(t, "sha512-v2kDEe57lecTulaDIuNTPy3Ry4gLGJ6Z1O3vE1krgXZNrsQ+LFTGHVxVjcXPs17LhbZVGedAJv8XZ1tvj5FvSg==", dep.Integrity)
	assert.False(t, dep.Dev)
	assert.False(t, dep.Optional)
}
