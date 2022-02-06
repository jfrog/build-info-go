package build

import (
	"path/filepath"
	"testing"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBuildInfoForNpmProject(t *testing.T) {
	service := NewBuildInfoService()
	npmBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, npmBuild.Clean())
	}()

	// Create npm project.
	projectPath, err := filepath.Abs(filepath.Join("testdata", "npm", "project"))
	assert.NoError(t, err)
	tmpProjectPath, cleanup := buildutils.CreateTestProject(t, projectPath)
	defer cleanup()

	// Install dependencies in the npm project.
	_, _, err = buildutils.RunNpmCmd("npm", tmpProjectPath, buildutils.Install, nil, &utils.NullLog{})
	assert.NoError(t, err)

	npmModule, err := npmBuild.AddNpmModule(tmpProjectPath)
	assert.NoError(t, err)
	err = npmModule.CalcDependencies()
	assert.NoError(t, err)
	buildInfo, err := npmBuild.ToBuildInfo()
	assert.NoError(t, err)

	// Verify results.
	path, err := filepath.Abs(filepath.Join("testdata", "npm", "project", "expected_npm_buildinfo.json"))
	assert.NoError(t, err)
	expectedBuildInfo := buildutils.GetBuildInfo(t, path)
	entities.IsEqualModuleSlices(buildInfo.Modules, expectedBuildInfo.Modules)
}
