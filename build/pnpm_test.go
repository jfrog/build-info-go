package build

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/tests"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var pnpmLogger = utils.NewDefaultLogger(utils.INFO)

func TestGenerateBuildInfoForPnpm(t *testing.T) {
	pnpmVersion, execPath, err := buildutils.GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	service := NewBuildInfoService()
	pnpmBuild, err := service.GetOrCreateBuild("build-info-go-test-pnpm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, pnpmBuild.Clean())
	}()

	// Create pnpm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	// Install dependencies in the pnpm project.
	_, _, err = buildutils.RunPnpmCmd(execPath, projectPath, []string{"install"}, pnpmLogger)
	assert.NoError(t, err)

	pnpmModule, err := pnpmBuild.AddPnpmModule(projectPath)
	assert.NoError(t, err)

	err = pnpmModule.CalcDependencies()
	assert.NoError(t, err)

	buildInfo, err := pnpmBuild.ToBuildInfo()
	assert.NoError(t, err)

	// Verify results - should have at least one module with dependencies
	assert.Len(t, buildInfo.Modules, 1)
	assert.Greater(t, len(buildInfo.Modules[0].Dependencies), 0, "Expected at least one dependency")

	// Verify pnpm version is supported
	assert.True(t, pnpmVersion.AtLeast("6.0.0"), "pnpm version should be at least 6.0.0")
}

func TestFilterPnpmArgsFlags(t *testing.T) {
	_, execPath, err := buildutils.GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	service := NewBuildInfoService()
	pnpmBuild, err := service.GetOrCreateBuild("build-info-go-test-pnpm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, pnpmBuild.Clean())
	}()

	// Create pnpm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	// Install dependencies first
	_, _, err = buildutils.RunPnpmCmd(execPath, projectPath, []string{"install"}, pnpmLogger)
	assert.NoError(t, err)

	pnpmModule, err := pnpmBuild.AddPnpmModule(projectPath)
	assert.NoError(t, err)

	// Test filtering with command and flags
	pnpmArgs := []string{"ls", "--json", "--long"}
	pnpmModule.SetPnpmArgs(pnpmArgs)
	pnpmModule.filterPnpmArgsFlags()
	expected := []string{"--json", "--long"}
	assert.Equal(t, expected, pnpmModule.pnpmArgs)

	// Test filtering with only flags
	pnpmArgs = []string{"--prod", "--json"}
	pnpmModule.SetPnpmArgs(pnpmArgs)
	pnpmModule.filterPnpmArgsFlags()
	expected = []string{"--prod", "--json"}
	assert.Equal(t, expected, pnpmModule.pnpmArgs)

	// Test filtering with single command (no flags)
	pnpmArgs = []string{"install"}
	pnpmModule.SetPnpmArgs(pnpmArgs)
	pnpmModule.filterPnpmArgsFlags()
	expected = []string{}
	assert.Equal(t, expected, pnpmModule.pnpmArgs)
}

func TestPnpmModuleSetters(t *testing.T) {
	_, execPath, err := buildutils.GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	service := NewBuildInfoService()
	pnpmBuild, err := service.GetOrCreateBuild("build-info-go-test-pnpm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, pnpmBuild.Clean())
	}()

	// Create pnpm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	// Install dependencies first
	_, _, err = buildutils.RunPnpmCmd(execPath, projectPath, []string{"install"}, pnpmLogger)
	assert.NoError(t, err)

	pnpmModule, err := pnpmBuild.AddPnpmModule(projectPath)
	assert.NoError(t, err)

	// Test SetName
	pnpmModule.SetName("custom-module-name")
	assert.Equal(t, "custom-module-name", pnpmModule.name)

	// Test SetPnpmArgs
	args := []string{"--prod", "--frozen-lockfile"}
	pnpmModule.SetPnpmArgs(args)
	assert.Equal(t, args, pnpmModule.pnpmArgs)

	// Test SetCollectBuildInfo
	pnpmModule.SetCollectBuildInfo(true)
	assert.True(t, pnpmModule.collectBuildInfo)
	pnpmModule.SetCollectBuildInfo(false)
	assert.False(t, pnpmModule.collectBuildInfo)
}

func TestPnpmModuleBuild(t *testing.T) {
	_, _, err := buildutils.GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	service := NewBuildInfoService()
	pnpmBuild, err := service.GetOrCreateBuild("build-info-go-test-pnpm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, pnpmBuild.Clean())
	}()

	// Create pnpm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	pnpmModule, err := pnpmBuild.AddPnpmModule(projectPath)
	assert.NoError(t, err)

	// Test Build with install command
	pnpmModule.SetPnpmArgs([]string{"install"})
	pnpmModule.SetCollectBuildInfo(false)
	err = pnpmModule.Build()
	assert.NoError(t, err)

	// Test Build with collectBuildInfo enabled
	pnpmModule.SetCollectBuildInfo(true)
	err = pnpmModule.Build()
	assert.NoError(t, err)

	buildInfo, err := pnpmBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
}

func TestNewPnpmModule(t *testing.T) {
	_, _, err := buildutils.GetPnpmVersionAndExecPath(pnpmLogger)
	if err != nil {
		t.Skip("pnpm is not installed, skipping test")
	}

	service := NewBuildInfoService()
	pnpmBuild, err := service.GetOrCreateBuild("build-info-go-test-pnpm", strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, pnpmBuild.Clean())
	}()

	// Create pnpm project.
	path, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	projectPath, cleanup := tests.CreatePnpmTest(t, path, "project1")
	defer cleanup()

	// Test creating module with explicit path
	pnpmModule, err := pnpmBuild.AddPnpmModule(projectPath)
	assert.NoError(t, err)
	assert.NotNil(t, pnpmModule)
	assert.Equal(t, projectPath, pnpmModule.srcPath)
	assert.NotEmpty(t, pnpmModule.executablePath)
}
