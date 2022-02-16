package integrationtests

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/build"
	testdatautils "github.com/jfrog/build-info-go/build/testdata"
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var runNpmIntegrationTests = flag.Bool("npmIntegration", false, "Run the npm integration tests, additionally to the unit tests")
var logger = utils.NewDefaultLogger(utils.INFO)

func TestBundledDependenciesList(t *testing.T) {
	initNpmIntegrationTests(t)

	path := getTestDir(t, "project1", false)
	tmpProject1Path, p1Cleanup := testdatautils.CreateTestProject(t, path)
	defer p1Cleanup()
	cacachePath := filepath.Join(tmpProject1Path, "tmpcache")
	npmArgs := []string{"--cache=" + cacachePath}

	// Install dependencies in the npm project.
	_, _, err := buildutils.RunNpmCmd("npm", tmpProject1Path, buildutils.Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := buildutils.CalculateDependenciesList("npm", tmpProject1Path, "build-info-go-tests", npmArgs, logger)
	assert.NoError(t, err)

	// Check peer dependency is not found.
	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(tmpProject1Path, "excpected_dependencies_list.json"), &excpected))
	assert.True(t, entities.IsEqualDependencySlices(excpected, dependencies))
}

// This case happends when the package-lock.json with property '"lockfileVersion": 1,' gets updated to version '"lockfileVersion": 2,' (from npm v6 to npm v7/v8).
// Seems like the compatibility upgrades may result in dependencies losing their integrity.
// We try to get the integrity from the cache index.
func TestDependencyWithNoIntegrity(t *testing.T) {
	initNpmIntegrationTests(t)

	// Create the second npm project which has a transitive dependency without integrity (ansi-regex:5.0.0).
	path := getTestDir(t, "project2", true)
	tmpProject2Path, p2Cleanup := testdatautils.CreateTestProject(t, path)
	defer p2Cleanup()

	// Run npm CI to create this special case where the 'ansi-regex:5.0.0' is missing the integrity.
	npmArgs := []string{"--cache=" + filepath.Join(tmpProject2Path, "tmpcache")}
	_, _, err := buildutils.RunNpmCmd("npm", tmpProject2Path, buildutils.Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := buildutils.CalculateDependenciesList("npm", tmpProject2Path, "jfrogtest", npmArgs, logger)
	assert.NoError(t, err)

	// Verify results.
	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(tmpProject2Path, "excpected_dependencies_list.json"), &excpected))
	assert.True(t, entities.IsEqualDependencySlices(excpected, dependencies))
}

func TestGenerateBuildInfoForNpm(t *testing.T) {
	initNpmIntegrationTests(t)

	service := build.NewBuildInfoService()
	npmBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, npmBuild.Clean())
	}()

	// Create npm project.
	path := getTestDir(t, "project3", false)

	tmpProjectPath, cleanup := testdatautils.CreateTestProject(t, path)
	defer cleanup()
	npmArgs := []string{"--cache=" + filepath.Join(tmpProjectPath, "tmpcache")}

	// Install dependencies in the npm project.
	_, _, err = buildutils.RunNpmCmd("npm", tmpProjectPath, buildutils.Install, npmArgs, logger)
	assert.NoError(t, err)
	npmModule, err := npmBuild.AddNpmModule(tmpProjectPath)
	assert.NoError(t, err)
	npmModule.SetNpmArgs(npmArgs)
	err = npmModule.CalcDependencies()
	assert.NoError(t, err)
	buildInfo, err := npmBuild.ToBuildInfo()
	assert.NoError(t, err)

	// Verify results.
	expectedBuildInfoJson := filepath.Join(path, "expected_npm_buildinfo.json")
	expectedBuildInfo := testdatautils.GetBuildInfo(t, expectedBuildInfoJson)
	entities.IsEqualModuleSlices(buildInfo.Modules, expectedBuildInfo.Modules)
}

// A project built differently for each operating system.
func TestDependenciesTreeDiffrentBetweenOss(t *testing.T) {
	initNpmIntegrationTests(t)
	path := getTestDir(t, "project4", true)
	tmpProject4Path, p1Cleanup := testdatautils.CreateTestProject(t, path)
	defer p1Cleanup()
	cacachePath := filepath.Join(tmpProject4Path, "tmpcache")

	// Install all of the project's dependencies.
	npmArgs := []string{"--cache=" + cacachePath}
	_, _, err := buildutils.RunNpmCmd("npm", tmpProject4Path, buildutils.Ci, npmArgs, logger)
	assert.NoError(t, err)

	// Calculate dependencies.
	dependencies, err := buildutils.CalculateDependenciesList("npm", tmpProject4Path, "bundle-dependencies", npmArgs, logger)
	assert.NoError(t, err)

	// Verify results.
	var excpected []entities.Dependency
	assert.NoError(t, utils.Unmarshal(filepath.Join(tmpProject4Path, "excpected_dependencies_list.json"), &excpected))
	assert.True(t, entities.IsEqualDependencySlices(excpected, dependencies))
}

func TestGetConfigCacheNpmIntegration(t *testing.T) {
	initNpmIntegrationTests(t)
	path := getTestDir(t, "project1", false)
	// Create the first npm project which containes peerDependencies, devDependencies & bundledDependencies
	tmpProject1Path, p1Cleanup := testdatautils.CreateTestProject(t, path)
	defer p1Cleanup()
	cachePath := filepath.Join(tmpProject1Path, "tmpcache")
	npmArgs := []string{"--cache=" + cachePath}
	// Install dependencies in the npm project.
	_, _, err := buildutils.RunNpmCmd("npm", tmpProject1Path, buildutils.Install, npmArgs, logger)
	assert.NoError(t, err)

	configCache, err := buildutils.GetNpmConfigCache(tmpProject1Path, "npm", npmArgs, logger)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(cachePath, "_cacache"), configCache)

	oldCache := os.Getenv("npm_config_cache")
	if oldCache != "" {
		defer func() {
			assert.NoError(t, os.Setenv("npm_config_cache", oldCache))
		}()
	}
	assert.NoError(t, os.Setenv("npm_config_cache", cachePath))
	configCache, err = buildutils.GetNpmConfigCache(tmpProject1Path, "npm", []string{}, logger)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(cachePath, "_cacache"), configCache)
}

func initNpmIntegrationTests(t *testing.T) {
	if !*runNpmIntegrationTests {
		t.Skip("To run this test, use: go test -npmIntegration")
	}
}

// Return the project path based on 'projectDir'.
// withOsInPath - some tests have individual cases for specific os, if true, return the tests for that belong to the current running os.
func getTestDir(t *testing.T, projectDir string, withOsInPath bool) string {
	version, err := buildutils.GetNpmVersion("npm", logger)
	assert.NoError(t, err)
	var npmVersionDir string
	switch {
	case version.AtLeast("8.0.0"):
		npmVersionDir = "npmv8"
	case version.AtLeast("7.0.0"):
		npmVersionDir = "npmv7"
	case version.AtLeast("6.0.0"):
		npmVersionDir = "npmv6"
	}
	if withOsInPath {
		switch runtime.GOOS {
		case "windows":
			npmVersionDir = filepath.Join(npmVersionDir, "windows")

		case "linux":
			npmVersionDir = filepath.Join(npmVersionDir, "linux")

		default:
			//MacOs
			npmVersionDir = filepath.Join(npmVersionDir, "macos")
		}
	}
	return filepath.Join("testdata", "npm", projectDir, npmVersionDir)
}
