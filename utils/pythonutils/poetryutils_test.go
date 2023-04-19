package pythonutils

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	testdatautils "github.com/jfrog/build-info-go/build/testdata"
	"github.com/stretchr/testify/assert"
)

func TestGetProjectNameFromPyproject(t *testing.T) {
	tests := []struct {
		poetryProject       string
		expectedProjectName string
	}{
		{"project", "my-poetry-project:1.1.0"},
		{"nodevdeps", "my-poetry-project:1.1.17"},
	}

	for _, test := range tests {
		t.Run(test.poetryProject, func(t *testing.T) {
			tmpProjectPath, cleanup := testdatautils.CreateTestProject(t, filepath.Join("..", "testdata", "poetry", test.poetryProject))
			defer cleanup()

			actualValue, err := extractProjectFromPyproject(filepath.Join(tmpProjectPath, "pyproject.toml"))
			assert.NoError(t, err)
			if actualValue.Name != test.expectedProjectName {
				t.Errorf("Expected value: %s, got: %s.", test.expectedProjectName, actualValue)
			}
		})
	}
}

func TestGetProjectDependencies(t *testing.T) {
	tests := []struct {
		poetryProject                  string
		expectedDirectDependencies     []string
		expectedTransitiveDependencies [][]string
	}{
		{"project", []string{"numpy:1.23.0", "pytest:5.4.3", "python:"}, [][]string{nil, {"atomicwrites:1.4.0", "attrs:21.4.0", "colorama:0.4.5", "more-itertools:8.13.0", "packaging:21.3", "pluggy:0.13.1", "py:1.11.0", "wcwidth:0.2.5"}, nil}},
		{"nodevdeps", []string{"numpy:1.23.0", "python:"}, [][]string{nil, nil, nil}},
	}

	for _, test := range tests {
		t.Run(test.poetryProject, func(t *testing.T) {
			tmpProjectPath, cleanup := testdatautils.CreateTestProject(t, filepath.Join("..", "testdata", "poetry", test.poetryProject))
			defer cleanup()

			graph, directDependencies, err := getPoetryDependencies(tmpProjectPath)
			assert.NoError(t, err)
			sort.Strings(directDependencies)
			if !reflect.DeepEqual(directDependencies, test.expectedDirectDependencies) {
				t.Errorf("Expected value: %s, got: %s.", test.expectedDirectDependencies, directDependencies)
			}
			for i, directDependency := range directDependencies {
				transitiveDependencies := graph[directDependency]
				sort.Strings(transitiveDependencies)
				if !reflect.DeepEqual(transitiveDependencies, test.expectedTransitiveDependencies[i]) {
					t.Errorf("Expected value: %s, got: %s.", test.expectedTransitiveDependencies[i], graph[directDependency])
				}
			}
		})
	}
}
