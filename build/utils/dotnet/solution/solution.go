package solution

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jfrog/build-info-go/build/utils/dotnet/dependencies"
	"github.com/jfrog/build-info-go/build/utils/dotnet/solution/project"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	gofrog "github.com/jfrog/gofrog/io"
)

type Solution interface {
	BuildInfo(module string, log utils.Log) (*buildinfo.BuildInfo, error)
	// BuildInfoWithNameVersionModuleId behaves like BuildInfo but, when no module override is
	// given, defaults each module ID to the fixed "<Name>:<Version>" form using the project's
	// own version (from project.assets.json). Used by the FlexPack path; BuildInfo retains the
	// legacy project-name default to avoid changing existing 'jf rt nuget/dotnet' build-info.
	BuildInfoWithNameVersionModuleId(module string, log utils.Log) (*buildinfo.BuildInfo, error)
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

// LoadProject loads only the explicitly selected project, rather than discovering every
// solution or project below its directory.
func LoadProject(projectFilePath string, log utils.Log) (Solution, error) {
	projectDirectory := filepath.Dir(projectFilePath)
	solution := &solution{path: projectDirectory}
	selectedProjects := []project.Project{project.CreateProject(projectFilePath)}
	if err := solution.getDependenciesSources(selectedProjects); err != nil {
		return solution, err
	}
	if err := solution.loadProjects(selectedProjects, log); err != nil {
		return solution, err
	}
	return solution, nil
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
	return solution.buildInfo(moduleName, false, log)
}

// BuildInfoWithNameVersionModuleId builds build-info using the fixed "<Name>:<Version>" module
// ID default (see the Solution interface documentation).
func (solution *solution) BuildInfoWithNameVersionModuleId(moduleName string, log utils.Log) (*buildinfo.BuildInfo, error) {
	return solution.buildInfo(moduleName, true, log)
}

func (solution *solution) buildInfo(moduleName string, useNameVersionModuleId bool, log utils.Log) (*buildinfo.BuildInfo, error) {
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
		moduleID := getModuleId(moduleName, currProject.Name())
		if useNameVersionModuleId {
			// Use PackageID rather than Name: it's what pack/push embed in the produced .nupkg's
			// file name, so restore's module here matches the module pack/push produce later for
			// the same project instead of splitting into two disconnected modules.
			moduleID = getNameVersionModuleId(moduleName, currProject.PackageID(), projectVersion(currProject))
		}
		module := buildinfo.Module{Id: moduleID, Type: buildinfo.Nuget}

		// Populate requestedBy field
		// Seed all direct dependencies with the module path
		for _, directDepName := range directDeps {
			if directDep, exist := projectDependencies[directDepName]; exist {
				// Add the direct path (don't overwrite - merge with existing)
				directDep.RequestedBy = append(directDep.RequestedBy, []string{module.Id})
			}
		}
		// Propagate paths to transitive dependencies
		for _, directDepName := range directDeps {
			if directDep, exist := projectDependencies[directDepName]; exist {
				populateRequestedBy(*directDep, projectDependencies, childrenMap)
			}
		}

		// Populate module dependencies
		// Sort dependency keys for deterministic output
		depKeys := make([]string, 0, len(projectDependencies))
		for key := range projectDependencies {
			depKeys = append(depKeys, key)
		}
		sort.Strings(depKeys)

		for _, key := range depKeys {
			dep := projectDependencies[key]
			// If dependency has no RequestedBy field, it means that the dependency not accessible in the current project.
			// In that case, the dependency is assumed to be under a project which is referenced by this project.
			// We therefore don't include the dependency in the build-info.
			if len(dep.RequestedBy) == 0 {
				continue
			}
			// Every path was seeded/propagated to always terminate in module.Id (see above), so
			// callers can tell which dependencies belong to this project. That's redundant in the
			// emitted chain itself though - a dependency's RequestedBy should only describe the
			// other packages that pulled it in, not the enclosing module it's already listed
			// under. Strip it before emitting, dropping paths that become empty (a direct
			// dependency, requested by nothing but the module itself). Scoped to the FlexPack
			// module-ID convention only ("<PackageID>:<Version>", matching Maven/Gradle FlexPack's
			// requestedBy shape) - BuildInfo's legacy project-name default keeps its existing
			// requestedBy shape so 'jf rt nuget'/'jf rt dotnet' output doesn't change.
			if useNameVersionModuleId {
				dep.RequestedBy = stripModuleFromRequestedBy(dep.RequestedBy, module.Id)
			}
			sortRequestedByPaths(dep.RequestedBy)
			module.Dependencies = append(module.Dependencies, *dep)
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

// getNameVersionModuleId returns the module ID as the fixed "<PackageID>:<Version>" form. It
// falls back to the bare packageID when the version is unavailable (e.g. legacy packages.config
// projects), and always yields to a user-supplied module override.
func getNameVersionModuleId(customModuleID, packageID, projectVersion string) string {
	if customModuleID != "" {
		return customModuleID
	}
	if projectVersion != "" {
		return packageID + ":" + projectVersion
	}
	return packageID
}

// projectVersion safely returns the project's own version via its dependency extractor,
// or an empty string when no extractor/version is available.
func projectVersion(currProject project.Project) string {
	if extractor := currProject.Extractor(); extractor != nil {
		return extractor.ProjectVersion()
	}
	return ""
}

// Populate requested by field for the input dependencies.
// parentDependency - The parent dependency
// dependenciesMap  - The input dependencies map
// childrenMap      - Map from dependency ID to children IDs
func populateRequestedBy(parentDependency buildinfo.Dependency, dependenciesMap map[string]*buildinfo.Dependency, childrenMap map[string][]string) {
	key := strings.ToLower(parentDependency.Id)
	childrenList, ok := childrenMap[key]
	if !ok {
		// Legacy fallback: packagesExtractor keys childrenMap by name only (no version).
		// Remove once packagesExtractor is migrated to name:version keys.
		if idx := strings.Index(key, ":"); idx != -1 {
			childrenList = childrenMap[key[:idx]]
		}
	}
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

// stripModuleFromRequestedBy removes the trailing module ID from each path, consistent with
// npm/Go/Maven FlexPack: transitive deps show only the intermediate package chain, while
// direct deps (whose path was just [moduleId]) keep the module as their sole requester so
// requestedBy is never empty for a dependency that is reachable from the module.
func stripModuleFromRequestedBy(paths [][]string, moduleId string) [][]string {
	var stripped [][]string
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		if path[len(path)-1] == moduleId {
			inner := path[:len(path)-1]
			if len(inner) == 0 {
				// Direct dependency: path was [moduleId] only. Keep it so the module is
				// recorded as the requester, matching npm/Go behaviour for direct deps.
				stripped = append(stripped, path)
			} else {
				stripped = append(stripped, inner)
			}
		} else {
			stripped = append(stripped, path)
		}
	}
	return stripped
}

// sortRequestedByPaths sorts RequestedBy paths for deterministic output.
// Shorter paths come first (direct deps before transitive), then lexicographic order.
func sortRequestedByPaths(paths [][]string) {
	sort.Slice(paths, func(i, j int) bool {
		// Shorter paths come first
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		// Same length: compare lexicographically
		for k := 0; k < len(paths[i]); k++ {
			if paths[i][k] != paths[j][k] {
				return paths[i][k] < paths[j][k]
			}
		}
		return false
	})
}

// isMatchingDependencySource checks if a dependency source file belongs to a project.
// It matches if the source is:
// - directly in the project root directory
// - under the project's obj directory (for project.assets.json)
// - in a subdirectory named after the project
func isMatchingDependencySource(source, projectRootPath, projectObjPattern, projectNamePattern string) bool {
	sourceLower := strings.ToLower(source)
	// Check if source is directly in project root
	isInRoot := projectRootPath == strings.ToLower(filepath.Dir(source))
	// Check if source is under the project's obj directory
	isUnderObjDir := strings.Contains(sourceLower, projectObjPattern)
	// Check if source path contains the project name directory (handles subdirs like /projectname/obj/)
	isUnderSubDirWithName := strings.Contains(sourceLower, projectNamePattern)
	return isInRoot || isUnderObjDir || isUnderSubDirWithName
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
	// getProjectsFromSlns discovers both classic '.sln' and modern '.slnx' solutions (the latter
	// takes precedence, see getSlnFiles) and returns their referenced projects as synthetic
	// '.sln'-shaped lines either way, so a single parseProjectsFromSolutionFile call below handles
	// exclude-pattern/'.*proj' filtering uniformly for both formats.
	slnProjects, err := solution.getProjectsFromSlns()
	if err != nil {
		return nil, err
	}
	if slnProjects == nil {
		return nil, nil
	}
	if len(excludePattern) > 0 {
		log.Debug(fmt.Sprintf("Testing to exclude projects by pattern: %s", excludePattern))
	}
	return solution.parseProjectsFromSolutionFile(slnProjects, excludePattern, log)
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
		projects = append(projects, project.CreateProject(projFilePath))
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
		return solution.loadSingleProject(project.CreateProject(projFiles[0]), log)
	}
	if len(projFiles) == 0 {
		// A standalone dependencies source (e.g. packages.config) with no enclosing *proj file -
		// synthesize a project from the directory itself so its dependencies still get collected,
		// instead of silently producing zero modules. Matched strictly (source's immediate parent
		// must be this exact directory) rather than via the generic subdir-name heuristic below:
		// that heuristic checks whether the *directory's own name* appears in a source's path,
		// which is always true here since this directory's name is a path-prefix component of
		// everything nested under it (e.g. testdata fixtures for unrelated projects).
		lowerDir := strings.ToLower(solution.path)
		for _, source := range solution.dependenciesSources {
			if strings.ToLower(filepath.Dir(source)) == lowerDir {
				return solution.loadProjectWithSource(project.CreateProjectFromDir(solution.path), source, log)
			}
		}
	}
	log.Warn(fmt.Sprintf("expecting 1 'proj' file but fuond %d files in path: %s", len(projFiles), solution.path))
	return nil
}

func (solution *solution) loadSingleProject(project project.Project, log utils.Log) error {
	// First we wil find the project's dependencies source.
	// It can be located directly in the project's root directory or in a directory with the project name under the solution root
	// or under obj directory (in case of assets.json file)
	projectRootPath := strings.ToLower(project.RootPath())
	projectObjPattern := strings.ToLower(filepath.Join(projectRootPath, dependencies.AssetDirName) + string(filepath.Separator))
	// Pattern includes trailing separator to avoid partial matches (e.g., "project" matching "projectname")
	projectNamePattern := strings.ToLower(string(filepath.Separator) + project.Name() + string(filepath.Separator))
	var dependenciesSource string
	for _, source := range solution.dependenciesSources {
		if isMatchingDependencySource(source, projectRootPath, projectObjPattern, projectNamePattern) {
			dependenciesSource = source
			break
		}
	}
	// If no dependencies source was found, we will skip the current project
	if len(dependenciesSource) == 0 {
		log.Debug(fmt.Sprintf("Project dependencies were not found for project: %s", project.Name()))
		return nil
	}
	return solution.loadProjectWithSource(project, dependenciesSource, log)
}

func (solution *solution) loadProjectWithSource(project project.Project, dependenciesSource string, log utils.Log) error {
	proj, err := project.Load(dependenciesSource, log)
	if err != nil {
		return err
	}
	if proj.Extractor() != nil {
		solution.projects = append(solution.projects, proj)
	}
	return nil
}

// Finds all the projects by reading the content of the sln/slnx files.
// Returns a slice with all the projects in the solution.
func (solution *solution) getProjectsFromSlns() ([]string, error) {
	var allProjects []string
	slnFiles, err := solution.getSlnFiles()
	if err != nil {
		return nil, err
	}
	for _, slnFile := range slnFiles {
		parseFile := parseSlnFile
		if strings.EqualFold(filepath.Ext(slnFile), ".slnx") {
			parseFile = parseSlnxFile
		}
		projects, err := parseFile(slnFile)
		if err != nil {
			return nil, err
		}
		allProjects = append(allProjects, projects...)
	}
	return allProjects, nil
}

// If sln file is not provided, finds all sln/slnx files in the directory.
// '.slnx' files take precedence over '.sln' files: after 'dotnet sln migrate', a directory can
// contain both a legacy '.sln' and its migrated '.slnx' counterpart, and auto-discovering both
// would parse the same projects twice.
func (solution *solution) getSlnFiles() (slnFiles []string, err error) {
	if solution.slnFile != "" {
		slnFiles = append(slnFiles, filepath.Join(solution.path, solution.slnFile))
		return
	}
	slnFiles, err = utils.ListFilesByFilterFunc(solution.path, func(filePath string) (bool, error) {
		return strings.EqualFold(filepath.Ext(filePath), ".slnx"), nil
	})
	if err != nil || len(slnFiles) > 0 {
		return
	}
	slnFiles, err = utils.ListFilesByFilterFunc(solution.path, func(filePath string) (bool, error) {
		return strings.EqualFold(filepath.Ext(filePath), ".sln"), nil
	})
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

// slnxDocument is the XML schema of a '.slnx' solution file.
// Projects can sit directly under <Solution> or inside a <Folder>, which is purely organizational
// and carries no dependency info. Per Slnx.xsd, <Folder> cannot nest further <Folder> elements;
// all folders are flat siblings directly under <Solution>, and hierarchy is expressed via a
// path-style Name attribute (e.g. "/src/nested/") rather than actual XML nesting.
//
//	<Solution>
//	  <Project Path="src/MyProject/MyProject.csproj" />
//	  <Folder Name="/tests/">
//	    <Project Path="tests/MyProject.Tests/MyProject.Tests.csproj" />
//	  </Folder>
//	</Solution>
type slnxDocument struct {
	XMLName  xml.Name      `xml:"Solution"`
	Projects []slnxProject `xml:"Project"`
	Folders  []slnxFolder  `xml:"Folder"`
}

type slnxFolder struct {
	Projects []slnxProject `xml:"Project"`
}

type slnxProject struct {
	// Path is relative to the .slnx file's directory, using '/' as separator.
	Path string `xml:"Path,attr"`
	// DisplayName overrides the default display name. If empty, it defaults to the project file's base name.
	DisplayName string `xml:"DisplayName,attr"`
}

// collectSlnxProjects collects every <Project>, whether directly under <Solution> or inside one
// of its flat <Folder> siblings.
func collectSlnxProjects(projects []slnxProject, folders []slnxFolder) []slnxProject {
	all := append([]slnxProject{}, projects...)
	for _, folder := range folders {
		all = append(all, folder.Projects...)
	}
	return all
}

// parseSlnxFile parses a '.slnx' file and returns synthetic project lines shaped like the
// legacy '.sln' format ('Project("{guid}") = "Name", "path"'), so parseProjectLine can
// handle both formats unchanged. The GUID is a placeholder; parseProjectLine ignores it.
func parseSlnxFile(slnxFile string) ([]string, error) {
	content, err := os.ReadFile(slnxFile)
	if err != nil {
		return nil, err
	}
	var doc slnxDocument
	if err = xml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}
	var lines []string
	for _, proj := range collectSlnxProjects(doc.Projects, doc.Folders) {
		if proj.Path == "" {
			continue
		}
		name := proj.DisplayName
		if name == "" {
			name = slnxProjectNameFromPath(proj.Path)
		}
		lines = append(lines, fmt.Sprintf(`Project("{00000000-0000-0000-0000-000000000000}") = "%s", "%s"`, name, proj.Path))
	}
	return lines, nil
}

// slnxProjectNameFromPath derives a default project name from a Path attribute,
// e.g. "src/MyProject/MyProject.csproj" -> "MyProject".
func slnxProjectNameFromPath(projectPath string) string {
	normalized := strings.ReplaceAll(projectPath, "\\", "/")
	base := normalized
	if idx := strings.LastIndex(normalized, "/"); idx != -1 {
		base = normalized[idx+1:]
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
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
