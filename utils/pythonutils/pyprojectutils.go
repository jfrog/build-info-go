package pythonutils

import (
	"github.com/BurntSushi/toml"
	"os"
)

type PyProjectToml struct {
	// Represents the [tool.poetry] section in pyproject.toml.
	Tool map[string]PoetryPackage
	// Represents the [project] section in pyproject.toml, for pypi package managers other than poetry.
	Project Project
}

// Pypi project defined for package managers other than poetry (pip, pipenv, etc...)
type Project struct {
	Name        string
	Version     string
	Description string
}

// Get project name and version by parsing the pyproject.toml file.
// Try to extract the project name and version from the [project] section, or if requested also try from [tool.poetry] section.
func extractPipProjectDetailsFromPyProjectToml(pyProjectFilePath string) (projectName, projectVersion string, err error) {
	pyProjectFile, err := decodePyProjectToml(pyProjectFilePath)
	if err != nil {
		return
	}
	return pyProjectFile.Project.Name, pyProjectFile.Project.Version, nil
}

func decodePyProjectToml(pyProjectFilePath string) (pyProjectFile PyProjectToml, err error) {
	content, err := os.ReadFile(pyProjectFilePath)
	if err != nil {
		return
	}
	_, err = toml.Decode(string(content), &pyProjectFile)
	return
}

// Look for 'pyproject.toml' file in current work dir.
// If found, return its absolute path.
func getPyProjectFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "pyproject.toml")
}
