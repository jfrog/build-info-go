package pythonutils

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
	"golang.org/x/exp/maps"
)

type PyprojectToml struct {
	Tool map[string]PoetryPackage
}
type PoetryPackage struct {
	Name            string
	Version         string
	Dependencies    map[string]interface{}
	DevDependencies map[string]interface{} `toml:"dev-dependencies"`
}

type PoetryLock struct {
	Package []*PoetryPackage
}

// Extract all poetry dependencies from the pyproject.toml and poetry.lock files.
// Returns a dependency map of all the installed poetry packages in the current environment and another list of the top level dependencies.
func getPoetryDependencies(srcPath string) (graph map[string][]string, directDependencies []string, err error) {
	filePath, err := getPoetryLockFilePath(srcPath)
	if err != nil || filePath == "" {
		// Error was returned or poetry.lock does not exist in directory.
		return nil, nil, err
	}
	projectName, directDependencies, err := getPackageNameFromPyproject(srcPath)
	if err != nil {
		return nil, nil, err
	}
	// Extract packages names from poetry.lock
	dependencies, dependenciesVersions, err := extractPackagesFromPoetryLock(filePath)
	if err != nil {
		return nil, nil, err
	}
	graph = make(map[string][]string)
	// Add the root node - the project itself.
	for _, directDependency := range directDependencies {
		directDependencyName := directDependency + ":" + dependenciesVersions[directDependency]
		graph[projectName] = append(graph[projectName], directDependencyName)
	}
	// Add versions to all dependencies
	for dependency, transitiveDependencies := range dependencies {
		for _, transitiveDependency := range transitiveDependencies {
			transitiveDependencyName := transitiveDependency + ":" + dependenciesVersions[transitiveDependency]
			graph[dependency] = append(graph[dependency], transitiveDependencyName)
		}
	}
	return graph, graph[projectName], nil
}

func getPackageNameFromPyproject(srcPath string) (string, []string, error) {
	filePath, err := getPyprojectFilePath(srcPath)
	if err != nil || filePath == "" {
		// Error was returned or pyproject.toml does not exist in directory.
		return "", []string{}, err
	}
	// Extract package name from pyproject.toml.
	project, err := extractProjectFromPyproject(filePath)
	if err != nil {
		return "", []string{}, err
	}
	return project.Name, append(maps.Keys(project.Dependencies), maps.Keys(project.DevDependencies)...), nil
}

// Look for 'pyproject.toml' file in current work dir.
// If found, return its absolute path.
func getPyprojectFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "pyproject.toml")
}

// Look for 'poetry.lock' file in current work dir.
// If found, return its absolute path.
func getPoetryLockFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "poetry.lock")
}

// Get the project-name by parsing the pyproject.toml file
func extractProjectFromPyproject(pyprojectFilePath string) (project PoetryPackage, err error) {
	content, err := os.ReadFile(pyprojectFilePath)
	if err != nil {
		return
	}
	var pyprojectFile PyprojectToml

	_, err = toml.Decode(string(content), &pyprojectFile)
	if err != nil {
		return
	}
	if poetryProject, ok := pyprojectFile.Tool["poetry"]; ok {
		// Extract project name from file content.
		poetryProject.Name = poetryProject.Name + ":" + poetryProject.Version
		return poetryProject, nil
	}
	return PoetryPackage{}, errors.New("Couldn't find project name and version in " + pyprojectFilePath)
}

// Get the project-name by parsing the poetry.lock file
func extractPackagesFromPoetryLock(lockFilePath string) (dependencies map[string][]string, dependenciesVersions map[string]string, err error) {
	content, err := os.ReadFile(lockFilePath)
	if err != nil {
		return
	}
	var poetryLockFile PoetryLock

	_, err = toml.Decode(string(content), &poetryLockFile)
	if err != nil {
		return
	}
	dependenciesVersions = make(map[string]string)
	dependencies = make(map[string][]string)
	for _, dependency := range poetryLockFile.Package {
		dependenciesVersions[dependency.Name] = dependency.Version
		dependencyName := dependency.Name + ":" + dependency.Version
		dependencies[dependencyName] = maps.Keys(dependency.Dependencies)
	}
	return
}
