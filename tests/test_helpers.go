package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
	"github.com/stretchr/testify/assert"
)

// Copy a project from path to temp dir.
// projectPath - Local path to a project
// Return the copied project location and a cleanup function to delete it.
func CreateTestProject(t *testing.T, projectPath string) (tmpProjectPath string, cleanup func()) {
	var err error
	tmpProjectPath, err = utils.CreateTempDir()
	assert.NoError(t, err)
	assert.NoError(t, utils.CopyDir(projectPath, tmpProjectPath, true, nil))
	cleanup = func() {
		assert.NoError(t, utils.RemoveTempDir(tmpProjectPath))
	}
	return
}

func GetBuildInfo(t *testing.T, filePath string) entities.BuildInfo {
	data, err := os.ReadFile(filePath)
	assert.NoError(t, err)
	var buildinfo entities.BuildInfo
	assert.NoError(t, json.Unmarshal(data, &buildinfo))
	return buildinfo
}

// Return the project path based on 'projectDir'.
// withOsInPath - some tests have individual cases for specific os, if true, return the tests for that belong to the current running os.
// testdataPath - abs path to testdata dir.
// projectDirName - name of the project's directory.
func CreateNpmTest(t *testing.T, testdataPath, projectDirName string, withOsInPath bool, version *version.Version) (tmpProjectPath string, cleanup func()) {
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
			// MacOs
			npmVersionDir = filepath.Join(npmVersionDir, "macos")
		}
	}
	path := filepath.Join(testdataPath, "npm", projectDirName, npmVersionDir)
	return CreateTestProject(t, path)
}

func PrintBuildInfoMismatch(t *testing.T, expected, actual []entities.Module) {
	excpectedStr, err := json.MarshalIndent(expected, "", "  ")
	assert.NoError(t, err)
	actualStr, err := json.MarshalIndent(actual, "", "  ")
	assert.NoError(t, err)
	t.Errorf("build-info don't match. want: \n%v\ngot:\n%s\n", string(excpectedStr), string(actualStr))
}

func CreateTempDirWithCallbackAndAssert(t *testing.T) (string, func()) {
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	return tempDirPath, func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}
}
