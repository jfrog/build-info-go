package build

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils/pythonutils"

	"github.com/jfrog/build-info-go/tests"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBuildInfoForPython(t *testing.T) {
	allTests := []struct {
		name                string
		pythonTool          pythonutils.PythonTool
		cmdArgs             []string
		moduleName          string
		expectedResultsJson string
	}{
		{"pip-with-module", pythonutils.Pip, []string{".", "--no-cache-dir", "--force-reinstall"}, "testModuleName:1.0.0", "expected_pip_buildinfo_with_module_name.json"},
		{"pip-without-module", pythonutils.Pip, []string{".", "--no-cache-dir", "--force-reinstall"}, "", "expected_pip_buildinfo_without_module_name.json"},

		{"pipenv-with-module", pythonutils.Pipenv, []string{}, "testModuleName:1.0.0", "expected_pipenv_buildinfo_with_module_name.json"},
		{"pipenv-without-module", pythonutils.Pipenv, []string{}, "", "expected_pipenv_buildinfo_without_module_name.json"},
	}
	// Run test cases.
	for _, test := range allTests {
		t.Run(test.name, func(t *testing.T) {
			testGenerateBuildInfoForPython(t, test.pythonTool, test.cmdArgs, test.moduleName, test.expectedResultsJson)
		})
	}
}

func testGenerateBuildInfoForPython(t *testing.T, pythonTool pythonutils.PythonTool, cmdArgs []string, moduleName, expectedResultsJson string) {
	service := NewBuildInfoService()
	pythonBuild, err := service.GetOrCreateBuild("build-info-go-test-"+string(pythonTool), strconv.FormatInt(time.Now().Unix(), 10))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, pythonBuild.Clean())
	}()
	// Create npm project.
	testdataDir, err := filepath.Abs(filepath.Join(".", "testdata"))
	assert.NoError(t, err)
	// Create python project
	projectPath := filepath.Join(testdataDir, "python", string(pythonTool))
	tmpProjectPath, cleanup := tests.CreateTestProject(t, projectPath)
	defer cleanup()

	// Install dependencies in the pip project.
	pythonModule, err := pythonBuild.AddPythonModule(tmpProjectPath, pythonTool)
	assert.NoError(t, err)
	pythonModule.SetName(moduleName)
	assert.NoError(t, pythonModule.RunInstallAndCollectDependencies(cmdArgs))
	buildInfo, err := pythonBuild.ToBuildInfo()
	if assert.NoError(t, err) {
		// Verify results.
		expectedBuildInfoJson := filepath.Join(projectPath, expectedResultsJson)
		expectedBuildInfo := tests.GetBuildInfo(t, expectedBuildInfoJson)
		match, err := entities.IsEqualModuleSlices(buildInfo.Modules, expectedBuildInfo.Modules)
		assert.NoError(t, err)
		if !match {
			tests.PrintBuildInfoMismatch(t, expectedBuildInfo.Modules, buildInfo.Modules)
		}
	}
}
