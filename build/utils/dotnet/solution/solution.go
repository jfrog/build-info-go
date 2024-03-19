package solution

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/build/utils/dotnet/dependencies"
	"github.com/jfrog/build-info-go/build/utils/dotnet/solution/project"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	gofrog "github.com/jfrog/gofrog/io"
)

type Solution interface {
	BuildInfo(module string, log utils.Log) (*buildinfo.BuildInfo, error)
	Marshal() ([]byte, error)
	GetProjects() []project.Project
	GetDependenciesSources() []string
}

var projectRegExp *regexp.Regexp

func Load(path, slnFile, excludePattern string, log utils.Log) (Solution, error) {
	solution := &solution{path: path, slnFile: slnFile}
	// Reads all projects from '.sln' files.
	slnProjects, err := solution.getProjectsListFromSlns(excludePattern, log)
	if err != nil {
		return solution, err
	}
	// Find all potential dependencies sources: packages.config and project.assets.json files.
	err = solution.getDependenciesSources(slnProjects)
	if err != nil {
		return solution, err
	}
	err = solution.loadProjects(slnProjects, log)
	return solution, err
}

type solution struct {
	path string
	// If there are more than one sln files in the directory,
	// the user must specify as arguments the sln file that should be used.
	slnFile             string
	projects            []project.Project
	dependenciesSources []string
}

func (solution *solution) BuildInfo(moduleName string, log utils.Log) (*buildinfo.BuildInfo, error) {
	build := &buildinfo.BuildInfo{}
	var modules []buildinfo.Module
	for _, currProject := range solution.projects {
		// Get All project dependencies
		projectDependencies, err := currProject.Extractor().AllDependencies(log)
		if err != nil {
			return nil, err
		}
		directDeps, err := currProject.Extractor().DirectDependencies()
		if err != nil {
			return nil, err
		}
		childrenMap, err := currProject.Extractor().ChildrenMap()
		if err != nil {
			return nil, err
		}

		// Create module
		module := buildinfo.Module{Id: getModuleId(moduleName, currProject.Name()), Type: buildinfo.Nuget}

		// Populate requestedBy field
		for _, directDepName := range directDeps {
			// Populate the direct dependency requested by only if the dependency exist in the cache
			if directDep, exist := projectDependencies[directDepName]; exist {
				directDep.RequestedBy = [][]string{{module.Id}}
				populateRequestedBy(*directDep, projectDependencies, childrenMap)
			}
		}

		// Populate module dependencies
		for _, dep := range projectDependencies {
			// If dependency has no RequestedBy field, it means that the dependency not accessible in the current project.
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
			// Update RequestedBy field from parent's RequestedBy.
			childDep.UpdateRequestedBy(parentDependency.Id, parentDependency.RequestedBy)

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

func (solution *solution) GetDependenciesSources() []string {
	return solution.dependenciesSources
}

func (solution *solution) DependenciesSourcesAndProjectsPathExist() bool {
	return len(solution.dependenciesSources) > 0 && len(solution.projects) > 0
}

func (solution *solution) getProjectsListFromSlns(excludePattern string, log utils.Log) ([]project.Project, error) {
	slnProjects, err := solution.getProjectsFromSlns()
	if err != nil {
		return nil, err
	}
	if slnProjects != nil {
		if len(excludePattern) > 0 {
			log.Debug(fmt.Sprintf("Testing to exclude projects by pattern: %s", excludePattern))
		}
		return solution.parseProjectsFromSolutionFile(slnProjects, excludePattern, log)
	}
	return nil, nil
}

func (solution *solution) loadProjects(slnProjects []project.Project, log utils.Log) error {
	// No '.sln' file was provided as a parameter/found - load project from the given directory.
	if slnProjects == nil {
		return solution.loadSingleProjectFromDir(log)
	}
	// Loading all projects listed in the relevant '.sln' files.
	for _, slnProject := range slnProjects {
		err := solution.loadSingleProject(slnProject, log)
		if err != nil {
			return err
		}
	}
	return nil
}

func (solution *solution) parseProjectsFromSolutionFile(slnProjects []string, excludePattern string, log utils.Log) ([]project.Project, error) {
	var projects []project.Project
	for _, projectLine := range slnProjects {
		projectName, projFilePath, err := parseProjectLine(projectLine, solution.path)
		if err != nil {
			log.Error(err)
			continue
		}
		// Exclude projects by pattern.
		if exclude, err := isProjectExcluded(projFilePath, excludePattern); err != nil {
			log.Error(err)
			continue
		} else if exclude {
			log.Debug(fmt.Sprintf("Skipping a project \"%s\", since the path '%s' is excluded", projectName, projFilePath))
			continue
		}
		// Looking for .*proj files.
		if !strings.HasSuffix(filepath.Ext(projFilePath), "proj") {
			log.Debug(fmt.Sprintf("Skipping a project \"%s\", since it doesn't have a '.*proj' file path.", projectName))
			continue
		}
		projects = append(projects, project.CreateProject(projectName, filepath.Dir(projFilePath)))
	}
	return projects, nil
}

func isProjectExcluded(projFilePath, excludePattern string) (exclude bool, err error) {
	if len(excludePattern) == 0 {
		return
	}
	return regexp.MatchString(excludePattern, projFilePath)
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
		projectDir := filepath.Dir(projFiles[0])
		return solution.loadSingleProject(project.CreateProject(projectName, projectDir), log)
	}
	log.Warn(fmt.Sprintf("expecting 1 'proj' file but fuond %d files in path: %s", len(projFiles), solution.path))
	return nil
}

func (solution *solution) loadSingleProject(project project.Project, log utils.Log) error {
	// First we wil find the project's dependencies source.
	// It can be located directly in the project's root directory or in a directory with the project name under the solution root
	// or under obj directory (in case of assets.json file)
	projectRootPath := strings.ToLower(project.RootPath())
	projectPathPattern := strings.ToLower(filepath.Join(projectRootPath, dependencies.AssetDirName) + string(filepath.Separator))
	projectNamePattern := strings.ToLower(string(filepath.Separator) + project.Name() + string(filepath.Separator))
	var dependenciesSource string
	for _, source := range solution.dependenciesSources {
		if projectRootPath == strings.ToLower(filepath.Dir(source)) || strings.Contains(strings.ToLower(source), projectPathPattern) || strings.Contains(strings.ToLower(source), projectNamePattern) {
			dependenciesSource = source
			break
		}
	}
	// If no dependencies source was found, we will skip the current project
	if len(dependenciesSource) == 0 {
		log.Debug(fmt.Sprintf("Project dependencies were not found for project: %s", project.Name()))
		return nil
	}
	proj, err := project.Load(dependenciesSource, log)
	if err != nil {
		return err
	}
	if proj.Extractor() != nil {
		solution.projects = append(solution.projects, proj)
	}
	return nil
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
	if len(projectInfo) < 2 {
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
		projectRegExp, err = utils.GetRegExp(`Project\("(.*\..*proj)`)
		if err != nil {
			return nil, err
		}
	}

	content, err := os.ReadFile(slnFile)
	if err != nil {
		return nil, err
	}
	projects := projectRegExp.FindAllString(string(content), -1)
	return projects, nil
}

func removeQuotes(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"")
}

// getDependenciesSourcesInProjectsDir Find potential dependencies sources: packages.config and project.assets.json files.
// For each project:
// 1. Check if the project is located under the solutions' directory (which was scanned before)
// 2. If it doesn't -find all potential dependencies sources for the relevant projects:
//   - 'project.assets.json' files are located in 'obj' directory in project's root.
//   - 'packages.config' files are located in the project root/ in solutions root in a directory named after project's name.
func (solution *solution) getDependenciesSourcesInProjectsDir(slnProjects []project.Project) error {
	// Walk and search for dependencies sources files in project's directories.
	for _, slnProject := range slnProjects {
		// Before running this function we already looked for dependencies sources in solutions directory.
		// If a project isn't located under solutions' dir - we should look for the dependencies sources in this specific project's directory.
		if !strings.HasPrefix(slnProject.RootPath(), solution.path) {
			err := gofrog.Walk(slnProject.RootPath(), func(path string, f os.FileInfo, err error) error {
				return solution.addPathToDependenciesSourcesIfNeeded(path)
			}, true)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Find all potential dependencies sources: packages.config and project.assets.json files.
func (solution *solution) getDependenciesSourcesInSolutionsDir() error {
	err := gofrog.Walk(solution.path, func(path string, f os.FileInfo, err error) error {
		return solution.addPathToDependenciesSourcesIfNeeded(path)
	}, true)

	return err
}

func (solution *solution) addPathToDependenciesSourcesIfNeeded(path string) error {
	if strings.HasSuffix(path, dependencies.PackagesFileName) || strings.HasSuffix(path, dependencies.AssetFileName) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		solution.dependenciesSources = append(solution.dependenciesSources, absPath)
	}
	return nil
}

// Find all potential dependencies sources: packages.config and project.assets.json files in solution/project root.
func (solution *solution) getDependenciesSources(slnProjects []project.Project) error {
	err := solution.getDependenciesSourcesInSolutionsDir()
	if err != nil {
		return err
	}
	return solution.getDependenciesSourcesInProjectsDir(slnProjects)
}
