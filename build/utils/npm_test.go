package utils

import (
	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	testdatautils "github.com/jfrog/build-info-go/build/testdata"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var logger = utils.NewDefaultLogger(utils.INFO)

func TestReadPackageInfoFromPackageJson(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	if err != nil {
		assert.NoError(t, err)
		return
	}

	tests := []struct {
		json string
		pi   *PackageInfo
	}{
		{`{ "name": "build-info-go-tests", "version": "1.0.0", "description": "test package"}`,
			&PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: ""}},
		{`{ "name": "@jfrog/build-info-go-tests", "version": "1.0.0", "description": "test package"}`,
			&PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: "@jfrog"}},
	}
	for _, test := range tests {
		t.Run(test.json, func(t *testing.T) {
			packInfo, err := ReadPackageInfo([]byte(test.json), npmVersion)
			if err != nil {
				t.Error("No error was expected in this test", err)
			}

			equals := reflect.DeepEqual(test.pi, packInfo)
			if !equals {
				t.Error("expected:", test.pi, "got:", packInfo)
			}
		})
	}
}

func TestGetDeployPath(t *testing.T) {
	tests := []struct {
		expectedPath string
		pi           *PackageInfo
	}{
		{`build-info-go-tests/-/build-info-go-tests-1.0.0.tgz`, &PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: ""}},
		{`@jfrog/build-info-go-tests/-/build-info-go-tests-1.0.0.tgz`, &PackageInfo{Name: "build-info-go-tests", Version: "1.0.0", Scope: "@jfrog"}},
	}
	for _, test := range tests {
		t.Run(test.expectedPath, func(t *testing.T) {
			actualPath := test.pi.GetDeployPath()
			if actualPath != test.expectedPath {
				t.Error("expected:", test.expectedPath, "got:", actualPath)
			}
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
	if err != nil {
		t.Error(err)
	}
	if len(expectedDependenciesList) != len(dependencies) {
		t.Error("The expected dependencies list length is", len(expectedDependenciesList), "and should be:\n", expectedDependenciesList,
			"\nthe actual dependencies list length is", len(dependencies), "and the list is:\n", dependencies)
		t.Error("The expected dependencies list length is", len(expectedDependenciesList), "and should be:\n", expectedDependenciesList,
			"\nthe actual dependencies list length is", len(dependencies), "and the list is:\n", dependencies)
	}
	for _, eDependency := range expectedDependenciesList {
		found := false
		for aDependency, v := range dependencies {
			if aDependency == eDependency.Key && assert.ElementsMatch(t, v.RequestedBy, eDependency.pathToRoot) {
				found = true
				break
			}
		}
		if !found {
			t.Error("The expected dependency:", eDependency, "is missing from the actual dependencies list:\n", dependencies)
		}
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
		if !assert.ElementsMatch(t, result, v.expected) {
			t.Errorf("appendScopes(\"%s\",\"%s\") => '%s', want '%s'", v.a, v.b, result, v.expected)
		}
	}
}

func TestBundledDependenciesList(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)

	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project1", false, npmVersion)
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

	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project5", true, npmVersion)
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
	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project2", true, npmVersion)
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

// This test case check that CalculateNpmDependenciesList ignore node_modules and update package-lock.json when needed,
// this according to the params 'IgnoreNodeModules' and 'OverWritePackageLock'.
func TestDependencyPackageLockOnly(t *testing.T) {
	path, cleanup := testdatautils.CreateTestProject(t, filepath.Join("..", "testdata/npm/project6"))
	defer cleanup()
	data, err := os.ReadFile(filepath.Join(path, "package-lock_test.json"))
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(path, "package-lock_test.json"))
	require.NoError(t, err)
	os.WriteFile(filepath.Join(path, "package-lock.json"), data, info.Mode().Perm())
	// sleep so the package.json modified time will be bigger than the package-lock.json, this make sure it will recalculate lock file.
	time.Sleep(time.Millisecond * 5)
	require.NoError(t, os.Chtimes(filepath.Join(path, "package.json"), time.Now(), time.Now()))

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", path, "jfrogtest",
		NpmTreeDepListParam{Args: []string{}, IgnoreNodeModules: true, OverWritePackageLock: true}, true, logger)
	assert.NoError(t, err)
	expectedDeps := []entities.Dependency{{
		Id:          "underscore:1.13.6",
		Scopes:      []string{"prod"},
		RequestedBy: [][]string{{"jfrogtest"}},
		Checksum: entities.Checksum{
			Sha1:   "04786a1f589dc6c09f761fc5f45b89e935136441",
			Md5:    "945e1ea169a281c296b82ad2dd5466f6",
			Sha256: "aef5a43ac7f903136a93e75a274e3a7b50de1a92277e1666457cabf62eeb0140",
		},
	},
		{
			Id:          "cors.js:0.0.1-security",
			Scopes:      []string{"prod"},
			RequestedBy: [][]string{{"jfrogtest"}},
			Checksum: entities.Checksum{
				Sha1:   "a1304531e44d11f4406b424b8377c3f3f1d3a934",
				Md5:    "f798d8a0d5e59e0d1b10a8fdc7660df0",
				Sha256: "e2352450325dba7f38c45ec43ca77eab2cdba66fdb232061045e7039ada1da7e",
			},
		},
		{
			Id:          "lightweight:0.1.0",
			Scopes:      []string{"prod"},
			RequestedBy: [][]string{{"jfrogtest"}},
			Checksum: entities.Checksum{
				Sha1:   "5e154f8080f0e07a3a28950a5e5ee563df625ed3",
				Md5:    "8a0ac99046e2c9c962aee498633eccc3",
				Sha256: "4119c009fa51fba45331235f00908ab77f2a402ee37e47dfc2dd8d422faa160f",
			},
		},
		{
			Id:          "minimist:0.1.0",
			Type:        "",
			Scopes:      []string{"prod"},
			RequestedBy: [][]string{{"jfrogtest"}},
			Checksum: entities.Checksum{
				Sha1:   "99df657a52574c21c9057497df742790b2b4c0de",
				Md5:    "0c9e3002c2af447fcf831fe3f751b2d8",
				Sha256: "d8d08725641599bd538ef91f6e77109fec81f74aecaa994d568d61b44d06df6d",
			},
		},
	}
	sort.Slice(expectedDeps, func(i, j int) bool {
		return expectedDeps[i].Id > expectedDeps[j].Id
	})
	sort.Slice(dependencies, func(i, j int) bool {
		return dependencies[i].Id > dependencies[j].Id
	})
	assert.Equal(t, expectedDeps, dependencies)
}

// A project built differently for each operating system.
func TestDependenciesTreeDifferentBetweenOKs(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project4", true, npmVersion)
	defer cleanup()
	cacachePath := filepath.Join(projectPath, "tmpcache")

	// Install all the project's dependencies.
	npmArgs := []string{"--cache=" + cacachePath}
	_, _, err = RunNpmCmd("npm", projectPath, AppendNpmCommand(npmArgs, "ci"), logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "bundle-dependencies", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	assert.Greaterf(t, len(dependencies), 0, "Error: dependencies are not found!")

	// Remove node_modules directory, then calculate dependencies by package-lock.
	assert.NoError(t, utils.RemoveTempDir(filepath.Join(projectPath, "node_modules")))

	dependencies, err = CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	// Asserting there is at least one dependency.
	assert.Greaterf(t, len(dependencies), 0, "Error: dependencies are not found!")
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
			projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project3", false, npmVersion)
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
	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project1", false, npmVersion)
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

	assert.Greaterf(t, len(dependencies), 0, "Error: dependencies are not found!")

	// Remove node_modules directory, then calculate dependencies by package-lock.
	assert.NoError(t, utils.RemoveTempDir(filepath.Join(projectPath, "node_modules")))

	dependencies, err = CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", NpmTreeDepListParam{Args: npmArgs}, true, logger)
	assert.NoError(t, err)

	// Asserting there is at least one dependency.
	assert.Greaterf(t, len(dependencies), 0, "Error: dependencies are not found!")
}
