package solution

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	buildinfo "github.com/jfrog/build-info-go/entities"

	"github.com/jfrog/build-info-go/build/utils/dotnet/dependencies"
	"github.com/jfrog/build-info-go/build/utils/dotnet/solution/project"
	"github.com/jfrog/build-info-go/utils"
)

type Solution interface {
	BuildInfo(module string, log utils.Log) (*buildinfo.BuildInfo, error)
	Marshal() ([]byte, error)
	GetProjects() []project.Project
}

var projectRegExp *regexp.Regexp

func Load(solutionPath, slnFile string, log utils.Log) (Solution, error) {
	solution := &solution{path: solutionPath, slnFile: slnFile}
	err := solution.getDependenciesSources()
	if err != nil {
		return solution, err
	}
	err = solution.loadProjects(log)
	return solution, err
}

type solution struct {
	path string
	// If there are more then one sln files in the directory,
	// the user must specify as arguments the sln file that should be used.
	slnFile             string
	projects            []project.Project
	dependenciesSources []string
}

func (solution *solution) BuildInfo(moduleName string, log utils.Log) (*buildinfo.BuildInfo, error) {
	build := &buildinfo.BuildInfo{}
	var modules []buildinfo.Module
	for _, project := range solution.projects {
		// Get All project dependencies
		dependencies, err := project.Extractor().AllDependencies(log)
		if err != nil {
			return nil, err
		}
		directDeps, err := project.Extractor().DirectDependencies()
		if err != nil {
			return nil, err
		}
		childrenMap, err := project.Extractor().ChildrenMap()
		if err != nil {
			return nil, err
		}

		// Create module
		module := buildinfo.Module{Id: getModuleId(moduleName, project.Name()), Type: buildinfo.Nuget}

		// Populate requestedBy field
		for _, directDepName := range directDeps {
			// Populate the direct dependency requested by only if the dependency exist in the cache
			if directDep, exist := dependencies[directDepName]; exist {
				directDep.RequestedBy = [][]string{{module.Id}}
				populateRequestedBy(*directDep, dependencies, childrenMap)
			}
		}

		// Populate module dependencies
		for _, dep := range dependencies {
			// If dependency has no RequestedBy field, it means that the depedency not accessible in the current project.
			// In that case, the dependency is assumed to be under a project which is referenced by this project.
			// We therefore don't include the dependency in the build-info.
			if len(dep.RequestedBy) > 0 {
				module.Dependencies = append(module.Dependencies, *dep)
			}
		}

		modules = append(modules, module)
	}
	build.Modules = modules
	return build, nil
}

func getModuleId(customModuleID, projectName string) string {
	if customModuleID != "" {
		return customModuleID
	}
	return projectName
}

// Populate requested by field for the input dependencies.
// parentDependency - The parent dependency
// dependenciesMap  - The input dependencies map
// childrenMap      - Map from dependency ID to children IDs
func populateRequestedBy(parentDependency buildinfo.Dependency, dependenciesMap map[string]*buildinfo.Dependency, childrenMap map[string][]string) {
	childrenList := childrenMap[getDependencyName(parentDependency.Id)]
	for _, childName := range childrenList {
		if childDep, ok := dependenciesMap[childName]; ok {
			if childDep.NodeHasLoop() || len(childDep.RequestedBy) >= buildinfo.RequestedByMaxLength {
				continue
			}
			for _, parentRequestedBy := range parentDependency.RequestedBy {
				childRequestedBy := append([]string{parentDependency.Id}, parentRequestedBy...)
				childDep.RequestedBy = append(childDep.RequestedBy, childRequestedBy)
			}
			// Run recursive call on child dependencies
			populateRequestedBy(*childDep, dependenciesMap, childrenMap)
		}
	}
}

func getDependencyName(dependencyKey string) string {
	dependencyName := dependencyKey[0:strings.Index(dependencyKey, ":")]
	return strings.ToLower(dependencyName)
}

func (solution *solution) Marshal() ([]byte, error) {
	return json.Marshal(&struct {
		Projects []project.Project `json:"projects,omitempty"`
	}{
		Projects: solution.projects,
	})
}

func (solution *solution) GetProjects() []project.Project {
	return solution.projects
}

func (solution *solution) loadProjects(log utils.Log) error {
	slnProjects, err := solution.getProjectsFromSlns()
	if err != nil {
		return err
	}
	if slnProjects != nil {
		return solution.loadProjectsFromSolutionFile(slnProjects, log)
	}

	return solution.loadSingleProjectFromDir(log)
}

func (solution *solution) loadProjectsFromSolutionFile(slnProjects []string, log utils.Log) error {
	for _, projectLine := range slnProjects {
		projectName, projFilePath, err := parseProjectLine(projectLine, solution.path)
		if err != nil {
			log.Error(err)
			continue
		}
		// Looking for .*proj files.
		if !strings.HasSuffix(filepath.Ext(projFilePath), "proj") {
			log.Debug(fmt.Sprintf("Skipping a project \"%s\", since it doesn't have a '.*proj' file path.", projectName))
			continue
		}
		solution.loadSingleProject(projectName, projFilePath, log)
	}
	return nil
}

func (solution *solution) loadSingleProjectFromDir(log utils.Log) error {
	// List files with .*proj extension.
	projFiles, err := utils.ListFilesByFilterFunc(solution.path, func(filePath string) (bool, error) {
		return strings.HasSuffix(filepath.Ext(filePath), "proj"), nil
	})
	if err != nil {
		return err
	}

	if len(projFiles) == 1 {
		projectName := strings.TrimSuffix(filepath.Base(projFiles[0]), filepath.Ext(projFiles[0]))
		solution.loadSingleProject(projectName, projFiles[0], log)
	}
	return nil
}

func (solution *solution) loadSingleProject(projectName, projFilePath string, log utils.Log) {
	// First we wil find the project's dependencies source.
	// It can be located directly in the project's root directory or in a directory with the project name under the solution root
	// or under obj directory (in case of assets.json file)
	projectRootPath := filepath.Dir(projFilePath)
	projectPathPattern := filepath.Join(projectRootPath, dependencies.AssetDirName) + string(filepath.Separator)
	projectNamePattern := string(filepath.Separator) + projectName + string(filepath.Separator)
	var dependenciesSource string
	for _, source := range solution.dependenciesSources {
		if projectRootPath == filepath.Dir(source) || strings.Contains(source, projectPathPattern) || strings.Contains(source, projectNamePattern) {
			dependenciesSource = source
			break
		}
	}
	// If no dependencies source was found, we will skip the current project
	if len(dependenciesSource) == 0 {
		log.Debug(fmt.Sprintf("Project dependencies was not found for project: %s", projectName))
		return
	}
	proj, err := project.Load(projectName, projectRootPath, dependenciesSource, log)
	if err != nil {
		log.Error(err)
		return
	}
	if proj.Extractor() != nil {
		solution.projects = append(solution.projects, proj)
	}
}

// Finds all the projects by reading the content of the sln files.
// Returns a slice with all the projects in the solution.
func (solution *solution) getProjectsFromSlns() ([]string, error) {
	var allProjects []string
	slnFiles, err := solution.getSlnFiles()
	if err != nil {
		return nil, err
	}
	for _, slnFile := range slnFiles {
		projects, err := parseSlnFile(slnFile)
		if err != nil {
			return nil, err
		}
		allProjects = append(allProjects, projects...)
	}
	return allProjects, nil
}

// If sln file is not provided, finds all sln files in the directory.
func (solution *solution) getSlnFiles() (slnFiles []string, err error) {
	if solution.slnFile != "" {
		slnFiles = append(slnFiles, filepath.Join(solution.path, solution.slnFile))
	} else {
		slnFiles, err = utils.ListFilesByFilterFunc(solution.path, func(filePath string) (bool, error) {
			return filepath.Ext(filePath) == ".sln", nil
		})
	}
	return
}

// Parses the project line for the project name and path information.
// Returns the name and path to proj file
func parseProjectLine(projectLine, path string) (projectName, projFilePath string, err error) {
	parsedLine := strings.Split(projectLine, "=")
	if len(parsedLine) <= 1 {
		return "", "", errors.New("Unexpected project line format: " + projectLine)
	}

	projectInfo := strings.Split(parsedLine[1], ",")
	if len(projectInfo) <= 2 {
		return "", "", errors.New("Unexpected project information format: " + parsedLine[1])
	}
	projectName = removeQuotes(projectInfo[0])
	// In case we are running on a non-Windows OS, the solution root path and the relative path to proj file might used different path separators.
	// We want to make sure we will get a valid path after we join both parts, so we will replace the proj separators.
	if utils.IsWindows() {
		projectInfo[1] = utils.UnixToWinPathSeparator(projectInfo[1])
	} else {
		projectInfo[1] = utils.WinToUnixPathSeparator(projectInfo[1])
	}
	projFilePath = filepath.Join(path, filepath.FromSlash(removeQuotes(projectInfo[1])))
	return
}

// Parse the sln file according to project regular expression and returns all the founded lines by the regex
func parseSlnFile(slnFile string) ([]string, error) {
	var err error
	if projectRegExp == nil {
		projectRegExp, err = utils.GetRegExp(`Project\("(.*)\nEndProject`)
		if err != nil {
			return nil, err
		}
	}

	content, err := ioutil.ReadFile(slnFile)
	if err != nil {
		return nil, err
	}
	projects := projectRegExp.FindAllString(string(content), -1)
	return projects, nil
}

func removeQuotes(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"")
}

// We'll walk through the file system to find all potential dependencies sources: packages.config and project.assets.json files
func (solution *solution) getDependenciesSources() error {
	err := utils.Walk(solution.path, func(path string, f os.FileInfo, err error) error {
		if strings.HasSuffix(path, dependencies.PackagesFileName) || strings.HasSuffix(path, dependencies.AssetFileName) {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			solution.dependenciesSources = append(solution.dependenciesSources, absPath)
		}
		return nil
	}, true)

	return err
}
