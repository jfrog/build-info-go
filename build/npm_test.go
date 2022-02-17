package build

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	testdatautils "github.com/jfrog/build-info-go/build/testdata"
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
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
	projectPath, cleanup := testdatautils.CreateNpmTest(t, path, "project3", false, npmVersion)
	defer cleanup()

	// Install dependencies in the npm project.
	npmArgs := []string{"--cache=" + filepath.Join(projectPath, "tmpcache")}
	_, _, err = buildutils.RunNpmCmd("npm", projectPath, buildutils.Install, npmArgs, logger)
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
	expectedBuildInfo := testdatautils.GetBuildInfo(t, expectedBuildInfoJson)
	entities.IsEqualModuleSlices(buildInfo.Modules, expectedBuildInfo.Modules)
}
