package pythonutils

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPyProject(t *testing.T) {
	testCases := []struct {
		testName          string
		pyProjectFilePath string
		expectedName      string
		expectedVersion   string
		errExpected       bool
	}{
		{"successful", filepath.Join("..", "testdata", "pip", "pyproject"), "pip-project-with-pyproject", "1.2.3", false},
		{"does not contain project section", filepath.Join("..", "testdata", "poetry", "project"), "", "", false},
		{"invalid", filepath.Join("..", "testdata", "pip", "setuppyproject", "setup.py"), "", "", true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			name, version, err := extractPipProjectDetailsFromPyProjectToml(filepath.Join(testCase.pyProjectFilePath, "pyproject.toml"))
			if testCase.errExpected {
				assert.Error(t, err)
				return
			}
			assert.Equal(t, testCase.expectedName, name)
			assert.Equal(t, testCase.expectedVersion, version)
			assert.NoError(t, err)
		})
	}
}
