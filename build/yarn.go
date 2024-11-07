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
	err = validateYarnVersion(executablePath, srcPath)
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

	return &YarnModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath, threads: 3, packageInfo: packageInfo, yarnArgs: []string{"install"}}, nil
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
	dependenciesMap, root, err := buildutils.GetYarnDependencies(ym.executablePath, ym.srcPath, ym.packageInfo, ym.containingBuild.logger, false)
	if err != nil {
		return nil, err
	}
	buildInfoDependencies := make(map[string]*entities.Dependency)
	err = ym.appendDependencyRecursively(root, []string{}, dependenciesMap, buildInfoDependencies)
	return buildInfoDependencies, err
}

func (ym *YarnModule) appendDependencyRecursively(yarnDependency *buildutils.YarnDependency, pathToRoot []string, yarnDependenciesMap map[string]*buildutils.YarnDependency,
	buildInfoDependencies map[string]*entities.Dependency) error {
	id := yarnDependency.Name() + ":" + yarnDependency.Details.Version

	// To avoid infinite loops in case of circular dependencies, the dependency won't be added if it's already in pathToRoot
	if slices.Contains(pathToRoot, id) {
		return nil
	}

	for _, dependencyPtr := range yarnDependency.Details.Dependencies {
		innerDepKey := buildutils.GetYarnDependencyKeyFromLocator(dependencyPtr.Locator)
		innerYarnDep, exist := yarnDependenciesMap[innerDepKey]
		if !exist {
			return fmt.Errorf("an error occurred while creating dependencies tree: dependency %s was not found", dependencyPtr.Locator)
		}
		err := ym.appendDependencyRecursively(innerYarnDep, append([]string{id}, pathToRoot...), yarnDependenciesMap,
			buildInfoDependencies)
		if err != nil {
			return err
		}
	}

	// The root project should not be added to the dependencies list
	if len(pathToRoot) == 0 {
		return nil
	}

	buildInfoDependency, exist := buildInfoDependencies[id]
	if !exist {
		buildInfoDependency = &entities.Dependency{Id: id}
		buildInfoDependencies[id] = buildInfoDependency
	}

	buildInfoDependency.RequestedBy = append(buildInfoDependency.RequestedBy, pathToRoot)
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

func validateYarnVersion(executablePath, srcPath string) error {
	yarnVersionStr, err := buildutils.GetVersion(executablePath, srcPath)
	if err != nil {
		return err
	}
	yarnVersion := version.NewVersion(yarnVersionStr)
	if yarnVersion.Compare(minSupportedYarnVersion) > 0 {
		return errors.New("Yarn must have version " + minSupportedYarnVersion + " or higher. The current version is: " + yarnVersionStr)
	}
	return nil
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
