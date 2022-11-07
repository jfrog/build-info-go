package utils

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	testdatautils "github.com/jfrog/build-info-go/build/testdata"
	"github.com/jfrog/build-info-go/entities"
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
	err = parseDependencies([]byte(dependenciesJsonList), []string{"root"}, dependencies, npmLsDependencyParser, utils.NewDefaultLogger(utils.INFO))
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

	// Install dependencies in the npm project.
	_, _, err = RunNpmCmd("npm", projectPath, Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", npmArgs, true, logger)
	assert.NoError(t, err)

	// Check peer dependency is not found.
	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(projectPath, "excpected_dependencies_list.json"), &excpected))
	match, err := entities.IsEqualDependencySlices(excpected, dependencies)
	assert.NoError(t, err)
	if !match {
		testdatautils.PrintBuildInfoMismatch(t, []entities.Module{{Dependencies: excpected}}, []entities.Module{{Dependencies: dependencies}})
	}
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
	// Install dependencies in the npm project.
	_, _, err = RunNpmCmd("npm", projectPath, Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", npmArgs, true, logger)
	assert.NoError(t, err)

	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(projectPath, "excpected_dependencies_list.json"), &excpected))
	match, err := entities.IsEqualDependencySlices(dependencies, excpected)
	assert.NoError(t, err)
	if !match {
		testdatautils.PrintBuildInfoMismatch(t, []entities.Module{{Dependencies: excpected}}, []entities.Module{{Dependencies: dependencies}})
	}
}

// This case happends when the package-lock.json with property '"lockfileVersion": 1,' gets updated to version '"lockfileVersion": 2,' (from npm v6 to npm v7/v8).
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
	_, _, err = RunNpmCmd("npm", projectPath, Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "jfrogtest", npmArgs, true, logger)
	assert.NoError(t, err)

	// Verify results.
	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(projectPath, "excpected_dependencies_list.json"), &excpected))
	match, err := entities.IsEqualDependencySlices(excpected, dependencies)
	assert.NoError(t, err)
	if !match {
		testdatautils.PrintBuildInfoMismatch(t, []entities.Module{{Dependencies: excpected}}, []entities.Module{{Dependencies: dependencies}})
	}
}

// A project built differently for each operating system.
func TestDependenciesTreeDiffrentBetweenOss(t *testing.T) {
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)
	path, err := filepath.Abs(filepath.Join("..", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project4", true, npmVersion)
	defer cleanup()
	cacachePath := filepath.Join(projectPath, "tmpcache")

	// Install all of the project's dependencies.
	npmArgs := []string{"--cache=" + cacachePath}
	_, _, err = RunNpmCmd("npm", projectPath, Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "bundle-dependencies", npmArgs, true, logger)
	assert.NoError(t, err)

	// Verify results.
	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(projectPath, "excpected_dependencies_list.json"), &excpected))
	match, err := entities.IsEqualDependencySlices(excpected, dependencies)
	assert.NoError(t, err)
	if !match {
		testdatautils.PrintBuildInfoMismatch(t, []entities.Module{{Dependencies: excpected}}, []entities.Module{{Dependencies: dependencies}})
	}
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

		projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project3", false, npmVersion)
		defer cleanup()
		cacachePath := filepath.Join(projectPath, "tmpcache")
		npmArgs := []string{"--cache=" + cacachePath, entry.scope}

		// Install dependencies in the npm project.
		_, _, err = RunNpmCmd("npm", projectPath, Ci, npmArgs, logger)
		assert.NoError(t, err)

		// Calculate dependencies with scope.
		dependencies, err := CalculateNpmDependenciesList("npm", projectPath, "build-info-go-tests", npmArgs, true, logger)
		assert.NoError(t, err)
		assert.Len(t, dependencies, entry.totalDeps)
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
	_, _, err = RunNpmCmd("npm", projectPath, Install, npmArgs, innerLogger)
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
