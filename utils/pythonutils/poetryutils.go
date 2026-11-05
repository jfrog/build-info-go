package pythonutils

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/exp/maps"
)

type PoetryPackage struct {
	Name            string
	Version         string
	Dependencies    map[string]interface{}
	DevDependencies map[string]interface{} `toml:"dev-dependencies"`
	Group map[string]PoetryDependencyGroup `toml:"group"`
}

type PoetryDependencyGroup struct {
	Dependencies map[string]interface{}
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
		return map[string][]string{}, []string{}, err
	}
	projectName, directDependencies, err := getPoetryPackageFromPyProject(srcPath)
	if err != nil {
		return map[string][]string{}, []string{}, err
	}
	// Extract packages names from poetry.lock
	dependencies, dependenciesVersions, err := extractPackagesFromPoetryLock(filePath)
	if err != nil {
		return map[string][]string{}, []string{}, err
	}
	graph = make(map[string][]string)
	// Add the root node - the project itself.
	for _, directDependency := range directDependencies {
		directDependencyName := directDependency + ":" + dependenciesVersions[strings.ToLower(directDependency)]
		graph[projectName] = append(graph[projectName], directDependencyName)
	}
	// Add versions to all dependencies
	for dependency, transitiveDependencies := range dependencies {
		for _, transitiveDependency := range transitiveDependencies {
			transitiveDependencyName := transitiveDependency + ":" + dependenciesVersions[strings.ToLower(transitiveDependency)]
			graph[dependency] = append(graph[dependency], transitiveDependencyName)
		}
	}
	return graph, graph[projectName], nil
}

func getPoetryPackageFromPyProject(srcPath string) (string, []string, error) {
	filePath, err := getPyProjectFilePath(srcPath)
	if err != nil || filePath == "" {
		return "", []string{}, err
	}
	project, err := extractPoetryPackageFromPyProjectToml(filePath)
	if err != nil {
		return "", []string{}, err
	}
	return project.Name, append(maps.Keys(project.Dependencies), maps.Keys(project.DevDependencies)...), nil
}

// Look for 'poetry.lock' file in current work dir.
// If found, return its absolute path.
func getPoetryLockFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "poetry.lock")
}

// Get poetry package by parsing the pyproject.toml file.
func extractPoetryPackageFromPyProjectToml(pyProjectFilePath string) (project PoetryPackage, err error) {
	pyProjectFile, err := decodePyProjectToml(pyProjectFilePath)
	if err != nil {
		return
	}
	poetryTool := pyProjectFile.Tool["poetry"]
	groupDependencies := collectPoetryGroupDependencies(poetryTool)

	if poetryTool.Name != "" {
		// Extract project name from file content.
		poetryTool.Name = poetryTool.Name + ":" + poetryTool.Version
		mergeIntoDevDependencies(&poetryTool, groupDependencies)
		return poetryTool, nil
	}
	if pyProjectFile.Project.Name != "" {
		project = poetryPackageFromPep621Project(pyProjectFile.Project)
		mergeIntoDevDependencies(&project, groupDependencies)
		return project, nil
	}
	return PoetryPackage{}, errors.New("Couldn't find project name and version in " + pyProjectFilePath)
}

func collectPoetryGroupDependencies(poetryTool PoetryPackage) map[string]interface{} {
	if len(poetryTool.Group) == 0 {
		return nil
	}
	merged := make(map[string]interface{})
	for _, group := range poetryTool.Group {
		for name, spec := range group.Dependencies {
			merged[name] = spec
		}
	}
	return merged
}

func mergeIntoDevDependencies(project *PoetryPackage, groupDependencies map[string]interface{}) {
	if len(groupDependencies) == 0 {
		return
	}
	if project.DevDependencies == nil {
		project.DevDependencies = make(map[string]interface{}, len(groupDependencies))
	}
	for name, spec := range groupDependencies {
		project.DevDependencies[name] = spec
	}
}

func poetryPackageFromPep621Project(project Project) PoetryPackage {
	dependencies := make(map[string]interface{}, len(project.Dependencies))
	for _, requirement := range project.Dependencies {
		if name := pep508PackageName(requirement); name != "" {
			dependencies[name] = requirement
		}
	}
	return PoetryPackage{
		Name:         project.Name + ":" + project.Version,
		Dependencies: dependencies,
	}
}

var pep508NameRegex = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)

func pep508PackageName(requirement string) string {
	match := pep508NameRegex.FindStringSubmatch(requirement)
	if match == nil {
		return ""
	}
	return match[1]
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
		dependenciesVersions[strings.ToLower(dependency.Name)] = dependency.Version
		dependencyName := dependency.Name + ":" + dependency.Version
		dependencies[dependencyName] = maps.Keys(dependency.Dependencies)
	}
	return
}
