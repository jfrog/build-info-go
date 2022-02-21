package pythonutils

import (
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetProjectNameFromFileContent(t *testing.T) {
	tests := []struct {
		fileContent         string
		expectedProjectName string
	}{
		{"Metadata-Version: 1.0\nName: jfrog-python-example-1\nVersion: 1.0\nSummary: Project example for building Python project with JFrog products\nHome-page: https://github.com/jfrog/project-examples\nAuthor: JFrog\nAuthor-email: jfrog@jfrog.com\nLicense: UNKNOWN\nDescription: UNKNOWN\nPlatform: UNKNOWN", "jfrog-python-example-1:1.0"},
		{"Metadata-Version: Name: jfrog-python-example-2\nLicense: UNKNOWN\nDescription: UNKNOWN\nPlatform: UNKNOWN\nName: jfrog-python-example-2\nVersion: 1.0\nSummary: Project example for building Python project with JFrog products\nHome-page: https://github.com/jfrog/project-examples\nAuthor: JFrog\nAuthor-email: jfrog@jfrog.com", "jfrog-python-example-2:1.0"},
		{"Name:Metadata-Version: 3.0\nName: jfrog-python-example-3\nVersion: 1.0\nSummary: Project example for building Python project with JFrog products\nHome-page: https://github.com/jfrog/project-examples\nAuthor: JFrog\nAuthor-email: jfrog@jfrog.com\nName: jfrog-python-example-4", "jfrog-python-example-3:1.0"},
	}

	for _, test := range tests {
		actualValue, err := getProjectIdFromFileContent([]byte(test.fileContent))
		if err != nil {
			t.Error(err)
		}
		if actualValue != test.expectedProjectName {
			t.Errorf("Expected value: %s, got: %s.", test.expectedProjectName, actualValue)
		}
	}
}

var moduleNameTestProvider = []struct {
	projectName         string
	moduleName          string
	expectedModuleName  string
	expectedPackageName string
}{
	{"setuppyproject", "", "jfrog-python-example:1.0", "jfrog-python-example:1.0"},
	{"setuppyproject", "overidden-module", "overidden-module", "jfrog-python-example:1.0"},
	{"requirementsproject", "", "", ""},
	{"requirementsproject", "overidden-module", "overidden-module", ""},
}

func TestDetermineModuleName(t *testing.T) {
	pythonExecutablePath, err := utils.GetExecutablePath("python")
	assert.NoError(t, err)

	for _, test := range moduleNameTestProvider {
		t.Run(strings.Join([]string{test.projectName, test.moduleName}, "/"), func(t *testing.T) {
			// Prepare test
			restoreCwd := changeToProjectDir(t, test.projectName)

			// Determine module name
			packageName, err := GetPackageNameFromSetuppy(pythonExecutablePath)
			assert.NoError(t, err)
			assert.Equal(t, test.expectedPackageName, packageName)

			// Cleanup
			restoreCwd()
		})
	}
}

func changeToProjectDir(t *testing.T, projectName string) func() {
	cwd, err := os.Getwd()
	assert.NoError(t, err)

	testdataDir := filepath.Join("..", "testdata", "pip", projectName)
	assert.NoError(t, os.Chdir(testdataDir))
	return func() {
		assert.NoError(t, os.Chdir(cwd))
	}
}
