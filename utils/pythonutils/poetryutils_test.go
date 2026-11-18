package pythonutils

import (
	"os"
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
		{"pep621", "my-poetry-project:0.1.0"},
		{"pep621-mixed", "my-poetry-project:1.2.0"},
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
		// Poetry 2.0+ default layout: dependencies are declared in the PEP 621 [project] table.
		{"pep621", []string{"aiostream:0.8.1"}, [][]string{{"typing-extensions:4.16.0"}}},
		// [project.dependencies], [project.optional-dependencies], [tool.poetry.dependencies] and [tool.poetry.group.*.dependencies] combined.
		// "Typing_Extensions" is resolved to its poetry.lock name, and the duplicate "aiostream" declaration is only listed once.
		{"pep621-mixed", []string{"aiostream:0.8.1", "click:8.1.8", "pytest:8.3.4", "typing-extensions:4.16.0"}, [][]string{{"typing-extensions:4.16.0"}, {"colorama:0.4.6"}, {"colorama:0.4.6", "iniconfig:2.0.0"}, nil}},
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

func TestGetPoetryPackageNameFromPep621Project(t *testing.T) {
	tmpProjectPath, cleanup := tests.CreateTestProject(t, filepath.Join("..", "testdata", "poetry", "pep621"))
	defer cleanup()

	packageName, err := GetPackageName(Poetry, tmpProjectPath)
	assert.NoError(t, err)
	assert.Equal(t, "my-poetry-project:0.1.0", packageName)
}

func TestExtractPoetryPackageFromPyProjectTomlWithoutProject(t *testing.T) {
	pyProjectFilePath := filepath.Join(t.TempDir(), "pyproject.toml")
	assert.NoError(t, os.WriteFile(pyProjectFilePath, []byte("[build-system]\nrequires = [\"setuptools\"]\n"), 0644))

	_, err := extractPoetryPackageFromPyProjectToml(pyProjectFilePath)
	assert.ErrorContains(t, err, "Couldn't find project name and version")
}

func TestNormalizePackageName(t *testing.T) {
	for input, expected := range map[string]string{
		"requests":          "requests",
		"Typing_Extensions": "typing-extensions",
		"zope.interface":    "zope-interface",
		"Foo-._Bar":         "foo-bar",
	} {
		assert.Equal(t, expected, normalizePackageName(input), input)
	}
}
