package utils

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

// Copy a project from path to temp dir.
// projectPath - Local path to a project
// return the copied project location and a cleanup function to delete it.
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
	data, err := ioutil.ReadFile(filePath)
	assert.NoError(t, err)
	var buildinfo entities.BuildInfo
	assert.NoError(t, json.Unmarshal(data, &buildinfo))
	return buildinfo
}
