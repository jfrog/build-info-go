package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
	"golang.org/x/exp/slices"
)

const minSupportedYarnVersion = "2.4.0"

type YarnModule struct {
	containingBuild          *Build
	name                     string
	srcPath                  string
	executablePath           string
	yarnVersion              string
	yarnArgs                 []string
	traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)
	threads                  int
	packageInfo              *buildutils.PackageInfo
}

// Pass an empty string for srcPath to find the Yarn project in the working directory.
func newYarnModule(srcPath string, containingBuild *Build) (*YarnModule, error) {
	executablePath, err := buildutils.GetYarnExecutable()
	if err != nil {
		return nil, err
	}
	containingBuild.logger.Debug("Found Yarn executable at:", executablePath)
	yarnVersion, err := validateYarnVersion(executablePath, srcPath)
	if err != nil {
		return nil, err
	}

	if srcPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		srcPath, err = utils.FindFileInDirAndParents(wd, "package.json")
		if err != nil {
			return nil, err
		}
	}

	// Read module name
	packageInfo, err := buildutils.ReadPackageInfoFromPackageJsonIfExists(srcPath, nil)
	if err != nil {
		return nil, err
	}
	name := packageInfo.BuildInfoModuleId()

	return &YarnModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath, yarnVersion: yarnVersion, threads: 3, packageInfo: packageInfo, yarnArgs: []string{"install"}}, nil
}

// Build builds the project, collects its dependencies and saves them in the build-info module.
func (ym *YarnModule) Build() error {
	err := RunYarnCommand(ym.executablePath, ym.srcPath, ym.yarnArgs...)
	if err != nil {
		return err
	}
	if !ym.containingBuild.buildNameAndNumberProvided() {
		return nil
	}
	dependenciesMap, err := ym.getDependenciesMap()
	if err != nil {
		return err
	}
	buildInfoDependencies, err := buildutils.TraverseDependencies(dependenciesMap, ym.traverseDependenciesFunc, ym.threads)
	if err != nil {
		return err
	}
	buildInfoModule := entities.Module{Id: ym.name, Type: entities.Npm, Dependencies: buildInfoDependencies}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}
	return ym.containingBuild.SaveBuildInfo(buildInfo)
}

func (ym *YarnModule) getDependenciesMap() (map[string]*entities.Dependency, error) {
	dependenciesMap, root, err := buildutils.GetYarnDependenciesWithVersion(ym.executablePath, ym.srcPath, ym.packageInfo, ym.yarnVersion, ym.containingBuild.logger, false)
	if err != nil {
		return nil, err
	}
	buildInfoDependencies := make(map[string]*entities.Dependency)
	if err = ym.appendDependencyRecursively(root, []string{}, dependenciesMap, buildInfoDependencies); err != nil {
		return nil, err
	}

	// Workspace root projects (monorepos) have no dependency edges on the root node in
	// `yarn info` output. Use `yarn workspaces list` to detect this case explicitly, then
	// seed an additional walk from each workspace member so their deps are captured.
	isWorkspace, err := buildutils.IsYarnWorkspaceProject(ym.executablePath, ym.srcPath)
	if err != nil {
		ym.containingBuild.logger.Warn("Could not determine workspace project status: " + err.Error())
	}
	if isWorkspace {
		rootName, err := root.Name()
		if err != nil {
			return nil, err
		}
		rootId := rootName + ":" + root.Details.Version
		for _, dep := range dependenciesMap {
			if dep.Value != root.Value && buildutils.IsWorkspaceLocator(dep.Value) {
				ym.containingBuild.logger.Debug(fmt.Sprintf(
					"Workspace member '%s': seeding dependency walk", dep.Value))
				if err = ym.appendDependencyRecursively(dep, []string{rootId}, dependenciesMap, buildInfoDependencies); err != nil {
					return nil, err
				}
			}
		}
	}
	return buildInfoDependencies, nil
}

func (ym *YarnModule) appendDependencyRecursively(yarnDependency *buildutils.YarnDependency, pathToRoot []string, yarnDependenciesMap map[string]*buildutils.YarnDependency,
	buildInfoDependencies map[string]*entities.Dependency) error {
	depName, err := yarnDependency.Name()
	if err != nil {
		return err
	}
	id := depName + ":" + yarnDependency.Details.Version

	// Guard against circular dependencies.
	if slices.Contains(pathToRoot, id) {
		return nil
	}

	isNonRegistry := buildutils.IsNonRegistryLocator(yarnDependency.Value)
	if isNonRegistry && len(pathToRoot) > 0 {
		// Non-registry inner node (workspace sibling, link, file, portal, git dep).
		// Log why it is skipped, then collapse it from the requestedBy chain (option B):
		// pass parent's pathToRoot unchanged so the non-registry node does not appear
		// as an intermediate in any descendant's requestedBy.
		ym.containingBuild.logger.Debug(fmt.Sprintf(
			"Skipping non-registry dependency '%s' (protocol: '%s'): local or non-Artifactory source",
			id, buildutils.ExtractLocatorProtocol(yarnDependency.Value)))
	}

	// Determine the pathToRoot to pass to children.
	// Root node (len==0): always extend so children record the module root in their chain.
	// Non-registry inner node: collapse — pass parent pathToRoot unchanged.
	// Registry inner node: extend normally.
	childPath := pathToRoot
	if len(pathToRoot) == 0 || !isNonRegistry {
		childPath = append([]string{id}, pathToRoot...)
	}

	for _, dependencyPtr := range yarnDependency.Details.Dependencies {
		innerDepKey := buildutils.GetYarnDependencyKeyFromLocator(dependencyPtr.Locator)
		innerYarnDep, exist := yarnDependenciesMap[innerDepKey]
		if !exist {
			return fmt.Errorf("an error occurred while creating dependencies tree: dependency %s was not found", dependencyPtr.Locator)
		}
		if err = ym.appendDependencyRecursively(innerYarnDep, childPath, yarnDependenciesMap, buildInfoDependencies); err != nil {
			return err
		}
	}

	// Root and non-registry nodes are not emitted as build-info dependencies.
	if len(pathToRoot) == 0 || isNonRegistry {
		return nil
	}

	buildInfoDependency, exist := buildInfoDependencies[id]
	if !exist {
		buildInfoDependency = &entities.Dependency{Id: id}
		buildInfoDependencies[id] = buildInfoDependency
	}

	// Limit requestedBy chains to prevent memory issues and excessive build-info size.
	// Following the same approach as Go, NuGet, and Python modules (see entities.RequestedByMaxLength).
	if len(buildInfoDependency.RequestedBy) < entities.RequestedByMaxLength {
		buildInfoDependency.RequestedBy = append(buildInfoDependency.RequestedBy, pathToRoot)
	}
	return nil
}

func (ym *YarnModule) SetName(name string) {
	ym.name = name
}

func (ym *YarnModule) SetArgs(yarnArgs []string) {
	ym.yarnArgs = yarnArgs
}

func (ym *YarnModule) SetThreads(threads int) {
	ym.threads = threads
}

// SetTraverseDependenciesFunc gets a function to execute on all dependencies after their collection in Build(), before they're saved.
// This function needs to return a boolean value indicating whether to save this dependency in the build-info or not.
// This function might run asynchronously with different dependencies (if the threads amount setting is bigger than 1).
// If more than one error are returned from this function in different threads, only the first of them will be returned from Build().
func (ym *YarnModule) SetTraverseDependenciesFunc(traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)) {
	ym.traverseDependenciesFunc = traverseDependenciesFunc
}

func (ym *YarnModule) AddArtifacts(artifacts ...entities.Artifact) error {
	return ym.containingBuild.AddArtifacts(ym.name, entities.Npm, artifacts...)
}

// validateYarnVersion checks the installed yarn version against the minimum supported
// version and returns the raw version string so callers can cache it.
func validateYarnVersion(executablePath, srcPath string) (string, error) {
	yarnVersionStr, err := buildutils.GetVersion(executablePath, srcPath)
	if err != nil {
		return "", err
	}
	yarnVersion := version.NewVersion(yarnVersionStr)
	if yarnVersion.Compare(minSupportedYarnVersion) > 0 {
		return "", errors.New("Yarn must have version " + minSupportedYarnVersion + " or higher. The current version is: " + yarnVersionStr)
	}
	return yarnVersionStr, nil
}

func RunYarnCommand(executablePath, srcPath string, args ...string) error {
	command := exec.Command(executablePath, args...)
	command.Dir = srcPath
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	err := command.Run()
	if _, ok := err.(*exec.ExitError); ok {
		err = errors.New(err.Error())
	}
	return err
}
