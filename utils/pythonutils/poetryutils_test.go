package pythonutils

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/jfrog/build-info-go/tests"
	"github.com/stretchr/testify/assert"
)

func TestGetProjectNameFromPyproject(t *testing.T) {
	testCases := []struct {
		poetryProject       string
		expectedProjectName string
	}{
		{"project", "my-poetry-project:1.1.0"},
		{"nodevdeps", "my-poetry-project:1.1.17"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.poetryProject, func(t *testing.T) {
			tmpProjectPath, cleanup := tests.CreateTestProject(t, filepath.Join("..", "testdata", "poetry", testCase.poetryProject))
			defer cleanup()

			actualValue, err := extractPoetryPackageFromPyProjectToml(filepath.Join(tmpProjectPath, "pyproject.toml"))
			assert.NoError(t, err)
			if actualValue.Name != testCase.expectedProjectName {
				t.Errorf("Expected value: %s, got: %s.", testCase.expectedProjectName, actualValue)
			}
		})
	}
}

func TestGetProjectDependencies(t *testing.T) {
	testCases := []struct {
		poetryProject                  string
		expectedDirectDependencies     []string
		expectedTransitiveDependencies [][]string
	}{
		{"project", []string{"numpy:1.23.0", "pytest:5.4.3", "python:"}, [][]string{nil, {"atomicwrites:1.4.0", "attrs:21.4.0", "colorama:0.4.5", "more-itertools:8.13.0", "packaging:21.3", "pluggy:0.13.1", "py:1.11.0", "wcwidth:0.2.5"}, nil}},
		{"nodevdeps", []string{"numpy:1.23.0", "python:"}, [][]string{nil, nil, nil}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.poetryProject, func(t *testing.T) {
			tmpProjectPath, cleanup := tests.CreateTestProject(t, filepath.Join("..", "testdata", "poetry", testCase.poetryProject))
			defer cleanup()

			graph, directDependencies, err := getPoetryDependencies(tmpProjectPath)
			assert.NoError(t, err)
			sort.Strings(directDependencies)
			if !reflect.DeepEqual(directDependencies, testCase.expectedDirectDependencies) {
				t.Errorf("Expected value: %s, got: %s.", testCase.expectedDirectDependencies, directDependencies)
			}
			for i, directDependency := range directDependencies {
				transitiveDependencies := graph[directDependency]
				sort.Strings(transitiveDependencies)
				if !reflect.DeepEqual(transitiveDependencies, testCase.expectedTransitiveDependencies[i]) {
					t.Errorf("Expected value: %s, got: %s.", testCase.expectedTransitiveDependencies[i], graph[directDependency])
				}
			}
		})
	}
}
