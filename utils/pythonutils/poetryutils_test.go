package pythonutils

import (
	"testing"
)

func TestGetProjectNameFromPyproject(t *testing.T) {
	tests := []struct {
		pyprojectFileContent string
		expectedProjectName  string
	}{
		{"/Users/tala/dev/forks/project-examples/python-example/poetry/my-poetry-project/pyproject.toml", "my-poetry-project:0.1.0"},
		//{`[tool.poetry]\nname = "my-poetry-project"\nversion = "0.1.0"\ndescription = ""\nauthors = ["Tal Arian <tala@jfrog.com>"]\n\n\n[tool.poetry.dependencies]\npython = "^3.10"\nnumpy = "^1.23.0"\n\n[tool.poetry.dev-dependencies]\npytest = "^5.2"\n\n[build-system]\nrequires = ["poetry-core>=1.0.0"]\nbuild-backend = "poetry.core.masonry.api"\n`, "my-poetry-project:0.1.0"},
	}

	for _, test := range tests {
		actualValue, err := ExtractProjectFromPyproject(test.pyprojectFileContent)
		if err != nil {
			t.Error(err)
		}
		if actualValue.Name != test.expectedProjectName {
			t.Errorf("Expected value: %s, got: %s.", test.expectedProjectName, actualValue)
		}
	}
}

func TestGetProjectDepsFromPoetryLock(t *testing.T) {
	tests := []struct {
		pyprojectFileContent string
		expectedProjectName  string
	}{
		{"/Users/tala/dev/forks/project-examples/python-example/poetry/my-poetry-project/", "my-poetry-project:0.1.0"},
	}

	for _, test := range tests {
		_, _, err := getPoetryDependencies(test.pyprojectFileContent)
		if err != nil {
			t.Error(err)
		}

	}
}
