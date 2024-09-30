package utils

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/require"

	"github.com/jfrog/build-info-go/tests"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var logger = utils.NewDefaultLogger(utils.INFO)

func TestReadPackageInfo(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	if err != nil {
		assert.NoError(t, err)
		return
	}

	testcases := []struct {
		json string
		pi   *PackageInfo
	}{
		{`{ "name": "build-info-go-tests", "version": "1.0.0", "description": "test package"}`,
			&PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: ""}},
		{`{ "name": "@jfrog/build-info-go-tests", "version": "1.0.0", "description": "test package"}`,
			&PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: "@jfrog"}},
		{`{}`, &PackageInfo{}},
	}
	for _, test := range testcases {
		t.Run(test.json, func(t *testing.T) {
			packInfo, err := ReadPackageInfo([]byte(test.json), npmVersion)
			assert.NoError(t, err)
			assert.Equal(t, test.pi, packInfo)
		})
	}
}

func TestReadPackageInfoFromPackageJsonIfExists(t *testing.T) {
	// Prepare tests data
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreateNpmTest(t, path, "project1", false, npmVersion)
	defer cleanup()

	// Prepare test cases
	testCases := []struct {
		testName             string
		packageJsonDirectory string
		expectedPackageInfo  *PackageInfo
	}{
		{"Happy flow", projectPath, &PackageInfo{Name: "build-info-go-tests", Version: "1.0.0"}},
		{"No package.json in path", path, &PackageInfo{Name: "", Version: ""}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			// Read package info
			packageInfo, err := ReadPackageInfoFromPackageJsonIfExists(testCase.packageJsonDirectory, npmVersion)
			assert.NoError(t, err)

			// Remove "v" prefix, if exist
			removeVersionPrefixes(packageInfo)

			// Check results
			assert.Equal(t, testCase.expectedPackageInfo.Name, packageInfo.Name)
			assert.Equal(t, testCase.expectedPackageInfo.Version, strings.TrimPrefix(packageInfo.Version, "v"))
		})
	}
}

func TestReadPackageInfoFromPackageJsonIfExistErr(t *testing.T) {
	// Prepare test data
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	tempDir, createTempDirCallback := tests.CreateTempDirWithCallbackAndAssert(t)
	assert.NoError(t, err)
	defer createTempDirCallback()

	// Create bad package.json file and expect error
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, "package.json"), []byte("non json file"), 0600))
	_, err = ReadPackageInfoFromPackageJsonIfExists(tempDir, npmVersion)
	assert.IsType(t, &json.SyntaxError{}, err)
}

func TestGetDeployPath(t *testing.T) {
	testcases := []struct {
		expectedPath string
		pi           *PackageInfo
	}{
		{`build-info-go-tests/-/build-info-go-tests-1.0.0.tgz`, &PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: ""}},
		{`@jfrog/build-info-go-tests/-/@jfrog/build-info-go-tests-1.0.0.tgz`, &PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: "@jfrog"}},
	}
	for _, test := range testcases {
		t.Run(test.expectedPath, func(t *testing.T) {
			assert.Equal(t, test.expectedPath, test.pi.GetDeployPath())
		})
	}
}

func TestParseDependencies(t *testing.T) {
	dependenciesJsonList, err := os.ReadFile(filepath.Join("..", "testdata", "npm", "dependenciesList.json"))
	if err != nil {
		t.Error(err)
	}

	expectedDependenciesList := []struct {
		Key        string
		pathToRoot [][]string
	}{
		{"underscore:1.4.4", [][]string{{"binary-search-tree:0.2.4", "nedb:1.0.2", "root"}}},
		{"@jfrog/npm_scoped:1.0.0", [][]string{{"root"}}},
		{"xml:1.0.1", [][]string{{"root"}}},
		{"xpm:0.1.1", [][]string{{"@jfrog/npm_scoped:1.0.0", "root"}}},
		{"binary-search-tree:0.2.4", [][]string{{"nedb:1.0.2", "root"}}},
		{"nedb:1.0.2", [][]string{{"root"}}},
		{"@ilg/es6-promisifier:0.1.9", [][]string{{"@ilg/cli-start-options:0.1.19", "xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
		{"wscript-avoider:3.0.2", [][]string{{"@ilg/cli-start-options:0.1.19", "xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
		{"yaml:0.2.3", [][]string{{"root"}}},
		{"@ilg/cli-start-options:0.1.19", [][]string{{"xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
		{"async:0.2.10", [][]string{{"nedb:1.0.2", "root"}}},
		{"find:0.2.7", [][]string{{"root"}}},
		{"jquery:3.2.0", [][]string{{"root"}}},
		{"nub:1.0.0", [][]string{{"find:0.2.7", "root"}, {"root"}}},
		{"shopify-liquid:1.d7.9", [][]string{{"xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
	}
	dependencies := make(map[string]*dependencyInfo)
	err = parseDependencies(dependenciesJsonList, []string{"root"}, dependencies, npmLsDependencyParser, utils.NewDefaultLogger(utils.INFO))
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

func TestAppendScopes(t *testing.T) {
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
		{[]string{"item1", "item2"}, []string{"item2", "item1", "item2"}, []string{"item1", "item2"}},
		{[]string{"item1", "item1"}, []string{"item1", "item1", "item1"}, []string{"item1"}},
	}
	for _, v := range scopes {
		result := appendScopes(v.a, v.b)
		assert.ElementsMatch(t, result, v.expected, "appendScopes(\"%s\",\"%s\") => '%s', want '%s'", v.a, v.b, result, v.expected)
	}
}

func TestBundledDependenciesList(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)

	projectPath, cleanup := tests.CreateNpmTest(t, path, "project1", false, npmVersion)
	defer cleanup()
	cacachePath := filepath.Join(projectPath, "tmpcache")
	npmArgs := []string{"--cache=" + cacachePath}

	validateDependencies(t, projectPath, npmArgs)
}

// This test runs with npm v6. It collects build-info for npm project that has conflicts in peer dependencies.
// A scenario like this can result in unexpected parsing results of the npm ls output,
// such as 'legacyNpmLsDependency.PeerMissing ' may be changed to a different type.
func TestConflictsDependenciesList(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	if npmVersion.AtLeast("7.0.0") {
		t.Skip("Running on npm v6 only, skipping...")
	}
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)

	projectPath, cleanup := tests.CreateNpmTest(t, path, "project5", true, npmVersion)
	defer cleanup()
	cacachePath := filepath.Join(projectPath, "tmpcache")
	npmArgs := []string{"--cache=" + cacachePath}

	validateDependencies(t, projectPath, npmArgs)
}

// This case happens when the package-lock.json with property '"lockfileVersion": 1,' gets updated to version '"lockfileVersion": 2,' (from npm v6 to npm v7/v8).
// Seems like the compatibility upgrades may result in dependencies losing their integrity.
// We try to get the integrity from the cache index.
func TestDependencyWithNoIntegrity(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)

	// Create the second npm project which has a transitive dependency without integrity (ansi-regex:5.0.0).
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreateNpmTest(t, path, "project2", true, npmVersion)
	defer cleanup()

	// Run npm CI to create this special case where the 'ansi-regex:5.0.0' is missing the integrity.
	npmArgs := []string{"--cache=" + filepath.Join(projectPath, "tmpcache")}
	_, _, err = RunNpmCmd("npm", projectPath, AppendNpmCommand(npmArgs, "ci"), logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "jfrogtest", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	assert.Greaterf(t, len(dependencies), 0, "Error: dependencies are not found!")
}

// This test case verifies that CalculateDependenciesMap correctly handles the exclusion of 'node_modules'
// and updates 'package-lock.json' as required, based on the 'IgnoreNodeModules' and 'OverwritePackageLock' parameters.
func TestDependencyPackageLockOnly(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	require.NoError(t, err)
	if !npmVersion.AtLeast("7.0.0") {
		t.Skip("Running on npm v7 and above only, skipping...")
	}
	path, cleanup := tests.CreateTestProject(t, filepath.Join("..", "testdata/npm/project6"))
	defer cleanup()
	assert.NoError(t, utils.MoveFile(filepath.Join(path, "package-lock_test.json"), filepath.Join(path, "package-lock.json")))
	// sleep so the package.json modified time will be bigger than the package-lock.json, this make sure it will recalculate lock file.
	require.NoError(t, os.Chtimes(filepath.Join(path, "package.json"), time.Now(), time.Now().Add(time.Millisecond*20)))

	// Calculate dependencies.
	dependencies, err := CalculateDependenciesMap("npm", path, "jfrogtest",
		NpmTreeDepListParam{Args: []string{}, IgnoreNodeModules: true, OverwritePackageLock: true}, logger, false)
	assert.NoError(t, err)
	var expectedRes = getExpectedRespForTestDependencyPackageLockOnly()
	assert.Equal(t, expectedRes, dependencies)
}

func TestCalculateDependenciesMapWithProhibitedInstallation(t *testing.T) {
	path, cleanup := tests.CreateTestProject(t, filepath.Join("..", "testdata", "npm", "noBuildProject"))
	defer cleanup()

	dependencies, err := CalculateDependenciesMap("npm", path, "jfrogtest",
		NpmTreeDepListParam{Args: []string{}, IgnoreNodeModules: false, OverwritePackageLock: false}, logger, true)

	assert.Nil(t, dependencies)
	assert.Error(t, err)
	var installForbiddenErr *utils.ErrProjectNotInstalled
	assert.True(t, errors.As(err, &installForbiddenErr))
}

func getExpectedRespForTestDependencyPackageLockOnly() map[string]*dependencyInfo {
	return map[string]*dependencyInfo{
		"underscore:1.13.6": {
			Dependency: entities.Dependency{
				Id:          "underscore:1.13.6",
				Scopes:      []string{"prod"},
				RequestedBy: [][]string{{"jfrogtest"}},
				Checksum:    entities.Checksum{},
			},
			npmLsDependency: &npmLsDependency{
				Name:      "underscore",
				Version:   "1.13.6",
				Integrity: "sha512-+A5Sja4HP1M08MaXya7p5LvjuM7K6q/2EaC0+iovj/wOcMsTzMvDFbasi/oSapiwOlt252IqsKqPjCl7huKS0A==",
			},
		},
		"cors.js:0.0.1-security": {
			Dependency: entities.Dependency{
				Id:          "cors.js:0.0.1-security",
				Scopes:      []string{"prod"},
				RequestedBy: [][]string{{"jfrogtest"}},
				Checksum:    entities.Checksum{},
			},
			npmLsDependency: &npmLsDependency{
				Name:      "cors.js",
				Version:   "0.0.1-security",
				Integrity: "sha512-Cu4D8imt82jd/AuMBwTpjrXiULhaMdig2MD2NBhRKbbcuCTWeyN2070SCEDaJuI/4kA1J9Nnvj6/cBe/zfnrrw==",
			},
		},
		"lightweight:0.1.0": {
			Dependency: entities.Dependency{
				Id:          "lightweight:0.1.0",
				Scopes:      []string{"prod"},
				RequestedBy: [][]string{{"jfrogtest"}},
				Checksum:    entities.Checksum{},
			},
			npmLsDependency: &npmLsDependency{
				Name:      "lightweight",
				Version:   "0.1.0",
				Integrity: "sha512-10pYSQA9EJqZZnXDR0urhg8Z0Y1XnRfi41ZFj3ZFTKJ5PjRq82HzT7LKlPyxewy3w2WA2POfi3jQQn7Y53oPcQ==",
			},
		},
		"minimist:0.1.0": {
			Dependency: entities.Dependency{
				Id:          "minimist:0.1.0",
				Scopes:      []string{"prod"},
				RequestedBy: [][]string{{"jfrogtest"}},
				Checksum:    entities.Checksum{},
			},
			npmLsDependency: &npmLsDependency{
				Name:      "minimist",
				Version:   "0.1.0",
				Integrity: "sha512-wR5Ipl99t0mTGwLjQJnBjrP/O7zBbLZqvA3aw32DmLx+nXHfWctUjzDjnDx09pX1Po86WFQazF9xUzfMea3Cnw==",
			},
		},
	}
}

// A project built differently for each operating system.
func TestDependenciesTreeDifferentBetweenOKs(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreateNpmTest(t, path, "project4", true, npmVersion)
	defer cleanup()
	cacachePath := filepath.Join(projectPath, "tmpcache")

	// Install all the project's dependencies.
	npmArgs := []string{"--cache=" + cacachePath}
	_, _, err = RunNpmCmd("npm", projectPath, AppendNpmCommand(npmArgs, "ci"), logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "bundle-dependencies", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	assert.Greater(t, len(dependencies), 0, "Error: dependencies are not found!")

	// Remove node_modules directory, then calculate dependencies by package-lock.
	assert.NoError(t, utils.RemoveTempDir(filepath.Join(projectPath, "node_modules")))

	dependencies, err = CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	// Asserting there is at least one dependency.
	assert.Greater(t, len(dependencies), 0, "Error: dependencies are not found!")
}

func TestNpmProdFlag(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	testDependencyScopes := []struct {
		scope     string
		totalDeps int
	}{
		{"", 2},
		{"--prod", 1},
	}
	for _, entry := range testDependencyScopes {
		func() {
			projectPath, cleanup := tests.CreateNpmTest(t, path, "project3", false, npmVersion)
			defer cleanup()
			cacachePath := filepath.Join(projectPath, "tmpcache")
			npmArgs := []string{"--cache=" + cacachePath, entry.scope}

			// Install dependencies in the npm project.
			_, _, err = RunNpmCmd("npm", projectPath, AppendNpmCommand(npmArgs, "ci"), logger)
			assert.NoError(t, err)

			// Calculate dependencies with scope.
			dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", NpmTreeDepListParam{Args: npmArgs}, true, logger)
			assert.NoError(t, err)
			assert.Len(t, dependencies, entry.totalDeps)
		}()
	}
}

func TestGetConfigCacheNpmIntegration(t *testing.T) {
	innerLogger := utils.NewDefaultLogger(utils.DEBUG)
	npmVersion, _, err := GetNpmVersionAndExecPath(innerLogger)
	assert.NoError(t, err)

	// Create the first npm project which contains peerDependencies, devDependencies & bundledDependencies
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreateNpmTest(t, path, "project1", false, npmVersion)
	defer cleanup()
	cachePath := filepath.Join(projectPath, "tmpcache")
	npmArgs := []string{"--cache=" + cachePath}

	// Install dependencies in the npm project.
	_, _, err = RunNpmCmd("npm", projectPath, AppendNpmCommand(npmArgs, "install"), innerLogger)
	assert.NoError(t, err)

	configCache, err := GetNpmConfigCache(projectPath, "npm", npmArgs, innerLogger)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(cachePath, "_cacache"), configCache)

	oldCache := os.Getenv("npm_config_cache")
	if oldCache != "" {
		defer func() {
			assert.NoError(t, os.Setenv("npm_config_cache", oldCache))
		}()
	}
	assert.NoError(t, os.Setenv("npm_config_cache", cachePath))
	configCache, err = GetNpmConfigCache(projectPath, "npm", []string{}, innerLogger)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(cachePath, "_cacache"), configCache)
}

// This function executes Ci, then validate generating dependencies in two possible scenarios:
// 1. node_module exists in the project.
// 2. node_module doesn't exist in the project and generating dependencies needs package-lock.
func validateDependencies(t *testing.T, projectPath string, npmArgs []string) {
	// Install dependencies in the npm project.
	_, _, err := RunNpmCmd("npm", projectPath, AppendNpmCommand(npmArgs, "ci"), logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	assert.Greater(t, len(dependencies), 0, "Error: dependencies are not found!")

	// Remove node_modules directory, then calculate dependencies by package-lock.
	assert.NoError(t, utils.RemoveTempDir(filepath.Join(projectPath, "node_modules")))

	dependencies, err = CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	// Asserting there is at least one dependency.
	assert.Greater(t, len(dependencies), 0, "Error: dependencies are not found!")
}

func TestFilterUniqueArgs(t *testing.T) {
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
		assert.Equal(t, testcase.expectedResult, filterUniqueArgs(testcase.argsToFilter, testcase.alreadyExists))
	}
}
