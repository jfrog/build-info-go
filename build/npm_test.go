package build

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/tests"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var logger = utils.NewDefaultLogger(utils.INFO)

func TestGenerateBuildInfoForNpm(t *testing.T) {
	service := NewBuildInfoService()
	npmBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, npmBuild.Clean())
	}()
	npmVersion, _, err := buildutils.GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)

	// Create npm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreateNpmTest(t, path, "project3", false, npmVersion)
	defer cleanup()

	// Install dependencies in the npm project.
	npmArgs := []string{"--cache=" + filepath.Join(projectPath, "tmpcache")}
	_, _, err = buildutils.RunNpmCmd("npm", projectPath, buildutils.AppendNpmCommand(npmArgs, "install"), logger)
	assert.NoError(t, err)
	npmModule, err := npmBuild.AddNpmModule(projectPath)
	assert.NoError(t, err)
	npmModule.SetNpmArgs(npmArgs)
	err = npmModule.CalcDependencies()
	assert.NoError(t, err)
	buildInfo, err := npmBuild.ToBuildInfo()
	assert.NoError(t, err)

	// Verify results.
	expectedBuildInfoJson := filepath.Join(projectPath, "expected_npm_buildinfo.json")
	expectedBuildInfo := tests.GetBuildInfo(t, expectedBuildInfoJson)
	match, err := entities.IsEqualModuleSlices(buildInfo.Modules, expectedBuildInfo.Modules)
	assert.NoError(t, err)
	if !match {
		tests.PrintBuildInfoMismatch(t, expectedBuildInfo.Modules, buildInfo.Modules)
	}
}

func TestFilterNpmArgsFlags(t *testing.T) {
	service := NewBuildInfoService()
	npmBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, npmBuild.Clean())
	}()
	npmVersion, _, err := buildutils.GetNpmVersionAndExecPath(logger)
	assert.NoError(t, err)

	// Create npm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreateNpmTest(t, path, "project3", false, npmVersion)
	defer cleanup()

	// Set arguments in npmArgs.
	npmArgs := []string{"ls", "--package-lock-only"}
	_, _, err = buildutils.RunNpmCmd("npm", projectPath, buildutils.AppendNpmCommand(npmArgs, "install"), logger)
	assert.NoError(t, err)
	npmModule, err := npmBuild.AddNpmModule(projectPath)
	assert.NoError(t, err)
	npmModule.SetNpmArgs(npmArgs)
	npmModule.filterNpmArgsFlags()
	expected := []string{"--package-lock-only"}
	assert.Equal(t, expected, npmModule.npmArgs)
	npmArgs = []string{"config", "cache", "--json", "--all"}
	npmModule.SetNpmArgs(npmArgs)
	npmModule.filterNpmArgsFlags()
	expected = []string{"--json", "--all"}
	assert.Equal(t, expected, npmModule.npmArgs)
}
