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
	// Dependency groups ([tool.poetry.group.<name>.dependencies]), which replaced dev-dependencies in Poetry 1.2.
	Group map[string]PoetryDependencyGroup
}

type PoetryDependencyGroup struct {
	Dependencies map[string]interface{}
}

var (
	// Matches the distribution name at the start of a PEP 508 requirement string.
	pep508NameRegex = regexp.MustCompile(`^\s*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)`)
	// Runs of characters that PEP 503 treats as equivalent name separators.
	pep503SeparatorsRegex = regexp.MustCompile(`[-_.]+`)
)

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
	dependencies, packageIds, err := extractPackagesFromPoetryLock(filePath)
	if err != nil {
		return map[string][]string{}, []string{}, err
	}
	graph = make(map[string][]string)
	// Add the root node - the project itself.
	for _, directDependency := range directDependencies {
		graph[projectName] = append(graph[projectName], getPoetryPackageId(directDependency, packageIds))
	}
	// Add versions to all dependencies
	for dependency, transitiveDependencies := range dependencies {
		for _, transitiveDependency := range transitiveDependencies {
			graph[dependency] = append(graph[dependency], getPoetryPackageId(transitiveDependency, packageIds))
		}
	}
	return graph, graph[projectName], nil
}

// Returns the "name:version" ID of the given package as recorded in poetry.lock, so that it matches the package's node in the graph
// even when it is referenced by an equivalent (PEP 503) name, e.g. "Typing_Extensions" for "typing-extensions".
// Packages that are missing from poetry.lock are returned with an empty version.
func getPoetryPackageId(name string, packageIds map[string]string) string {
	if packageId, ok := packageIds[normalizePackageName(name)]; ok {
		return packageId
	}
	return name + ":"
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
// Since Poetry 2.0, the project's metadata and dependencies are declared by default in the PEP 621 [project]
// table, while [tool.poetry] may still hold the legacy fields or additional dependency constraints.
// Both tables are therefore merged, with [tool.poetry] metadata taking precedence when set.
func extractPoetryPackageFromPyProjectToml(pyProjectFilePath string) (project PoetryPackage, err error) {
	pyProjectFile, err := decodePyProjectToml(pyProjectFilePath)
	if err != nil {
		return
	}
	poetryProject, isPoetryProject := pyProjectFile.Tool["poetry"]
	pep621Project := pyProjectFile.Project
	if !isPoetryProject && pep621Project.Name == "" {
		return PoetryPackage{}, errors.New("Couldn't find project name and version in " + pyProjectFilePath)
	}
	if poetryProject.Name == "" {
		poetryProject.Name = pep621Project.Name
	}
	if poetryProject.Version == "" {
		poetryProject.Version = pep621Project.Version
	}
	// Extract project name from file content.
	poetryProject.Name = poetryProject.Name + ":" + poetryProject.Version
	poetryProject.Dependencies = mergePoetryDependencies(poetryProject.Dependencies, pep621Project.Dependencies)
	for _, extraDependencies := range pep621Project.OptionalDependencies {
		poetryProject.Dependencies = mergePoetryDependencies(poetryProject.Dependencies, extraDependencies)
	}
	for _, group := range poetryProject.Group {
		poetryProject.DevDependencies = mergeDependencyMaps(poetryProject.DevDependencies, group.Dependencies)
	}
	return poetryProject, nil
}

// Adds the packages of the given PEP 508 requirement strings to the dependencies map, skipping packages that already exist in it.
func mergePoetryDependencies(dependencies map[string]interface{}, pep508Requirements []string) map[string]interface{} {
	pep621Dependencies := make(map[string]interface{}, len(pep508Requirements))
	for _, requirement := range pep508Requirements {
		if match := pep508NameRegex.FindStringSubmatch(requirement); match != nil {
			pep621Dependencies[match[1]] = requirement
		}
	}
	return mergeDependencyMaps(dependencies, pep621Dependencies)
}

// Adds the entries of source to target, skipping packages that already exist in target under an equivalent (PEP 503) name.
func mergeDependencyMaps(target, source map[string]interface{}) map[string]interface{} {
	if len(source) == 0 {
		return target
	}
	if target == nil {
		target = make(map[string]interface{}, len(source))
	}
	existing := make(map[string]bool, len(target))
	for name := range target {
		existing[normalizePackageName(name)] = true
	}
	for name, spec := range source {
		if normalizedName := normalizePackageName(name); !existing[normalizedName] {
			existing[normalizedName] = true
			target[name] = spec
		}
	}
	return target
}

// Normalizes a Python package name according to PEP 503, so that equivalent names (e.g. "Typing_Extensions" and "typing-extensions") match.
func normalizePackageName(name string) string {
	return pep503SeparatorsRegex.ReplaceAllString(strings.ToLower(name), "-")
}

// Get the packages from the poetry.lock file.
// Returns the dependencies of each package, and the "name:version" ID of each package keyed by its normalized name.
func extractPackagesFromPoetryLock(lockFilePath string) (dependencies map[string][]string, packageIds map[string]string, err error) {
	content, err := os.ReadFile(lockFilePath)
	if err != nil {
		return
	}
	var poetryLockFile PoetryLock

	_, err = toml.Decode(string(content), &poetryLockFile)
	if err != nil {
		return
	}
	packageIds = make(map[string]string)
	dependencies = make(map[string][]string)
	for _, dependency := range poetryLockFile.Package {
		dependencyName := dependency.Name + ":" + dependency.Version
		packageIds[normalizePackageName(dependency.Name)] = dependencyName
		dependencies[dependencyName] = maps.Keys(dependency.Dependencies)
	}
	return
}
