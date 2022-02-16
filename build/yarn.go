package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
	"os"
	"os/exec"
	"strings"
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
	executablePath, err := getYarnExecutable()
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
	packageInfo, err := buildutils.ReadPackageInfoFromPackageJson(srcPath, nil)
	if err != nil {
		return nil, err
	}
	name := packageInfo.BuildInfoModuleId()

	return &YarnModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath, threads: 3, packageInfo: packageInfo, yarnArgs: []string{"install"}}, nil
}

// Build builds the project, collects its dependencies and saves them in the build-info module.
func (ym *YarnModule) Build() error {
	err := runYarnCommand(ym.executablePath, ym.srcPath, ym.yarnArgs...)
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
	// Run 'yarn info'
	responseStr, errStr, err := runInfo(ym.executablePath, ym.srcPath)
	// Some warnings and messages of Yarn are printed to stderr. They don't necessarily cause the command to fail, but we'd want to show them to the user.
	if len(errStr) > 0 {
		ym.containingBuild.logger.Warn("Some errors occurred while collecting dependencies info:\n" + errStr)
	}
	if err != nil {
		ym.containingBuild.logger.Warn("An error was thrown while collecting dependencies info:", err.Error())
		// A returned error doesn't necessarily mean that the operation totally failed. If, in addition, the response is empty, then it probably does.
		if responseStr == "" {
			return nil, err
		}
	}

	dependenciesMap := make(map[string]*YarnDependency)
	scanner := bufio.NewScanner(strings.NewReader(responseStr))
	packageName := ym.packageInfo.FullName()
	var root *YarnDependency

	for scanner.Scan() {
		var currDependency YarnDependency
		currDepBytes := scanner.Bytes()
		err = json.Unmarshal(currDepBytes, &currDependency)
		if err != nil {
			return nil, err
		}
		dependenciesMap[currDependency.Value] = &currDependency

		// Check whether this dependency's name starts with the package name (which means this is the root)
		if strings.HasPrefix(currDependency.Value, packageName+"@") {
			root = &currDependency
		}
	}

	buildInfoDependencies := make(map[string]*entities.Dependency)
	err = ym.appendDependencyRecursively(root, []string{}, dependenciesMap, buildInfoDependencies)
	return buildInfoDependencies, err
}

func (ym *YarnModule) appendDependencyRecursively(yarnDependency *YarnDependency, pathToRoot []string, yarnDependenciesMap map[string]*YarnDependency,
	buildInfoDependencies map[string]*entities.Dependency) error {
	name := yarnDependency.Name()
	var ver string
	if len(pathToRoot) == 0 {
		// The version of the local project returned from 'yarn info' is '0.0.0-use.local', but we need the version mentioned in package.json
		ver = ym.packageInfo.Version
	} else {
		ver = yarnDependency.Details.Version
	}
	id := name + ":" + ver

	// To avoid infinite loops in case of circular dependencies, the dependency won't be added if it's already in pathToRoot
	if stringsSliceContains(pathToRoot, id) {
		return nil
	}

	for _, dependencyPtr := range yarnDependency.Details.Dependencies {
		innerDepKey := getYarnDependencyKeyFromLocator(dependencyPtr.Locator)
		innerYarnDep, exist := yarnDependenciesMap[innerDepKey]
		if !exist {
			return errors.New(fmt.Sprintf("An error occurred while creating dependencies tree: dependency %s was not found.", dependencyPtr.Locator))
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
	if !ym.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to add artifacts")
	}
	partial := &entities.Partial{ModuleId: ym.name, ModuleType: entities.Npm, Artifacts: artifacts}
	return ym.containingBuild.SavePartialBuildInfo(partial)
}

type YarnDependency struct {
	// The value is usually in this structure: @scope/package-name@npm:1.0.0
	Value   string         `json:"value,omitempty"`
	Details YarnDepDetails `json:"children,omitempty"`
}

func (yd *YarnDependency) Name() string {
	// Find the first index of '@', starting from position 1. In scoped dependencies (like '@jfrog/package-name@npm:1.2.3') we want to keep the first '@' as part of the name.
	atSignIndex := strings.Index(yd.Value[1:], "@") + 1
	return yd.Value[:atSignIndex]
}

type YarnDepDetails struct {
	Version      string                  `json:"Version,omitempty"`
	Dependencies []YarnDependencyPointer `json:"Dependencies,omitempty"`
}

type YarnDependencyPointer struct {
	Descriptor string `json:"descriptor,omitempty"`
	Locator    string `json:"locator,omitempty"`
}

func getYarnExecutable() (string, error) {
	yarnExecPath, err := exec.LookPath("yarn")
	if err != nil {
		return "", err
	}
	return yarnExecPath, nil
}

func validateYarnVersion(executablePath, srcPath string) error {
	yarnVersionStr, err := getVersion(executablePath, srcPath)
	if err != nil {
		return err
	}
	yarnVersion := version.NewVersion(yarnVersionStr)
	if yarnVersion.Compare(minSupportedYarnVersion) > 0 {
		return errors.New("Yarn must have version " + minSupportedYarnVersion + " or higher. The current version is: " + yarnVersionStr)
	}
	return nil
}

func getVersion(executablePath, srcPath string) (string, error) {
	command := exec.Command(executablePath, "--version")
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	command.Stderr = os.Stderr
	err := command.Run()
	if _, ok := err.(*exec.ExitError); ok {
		err = errors.New(err.Error())
	}
	return strings.TrimSpace(outBuffer.String()), err
}

// Yarn dependency locator usually looks like this: package-name@npm:1.2.3, which is used as the key in the dependencies map.
// But sometimes it points to a virtual package, so it looks different: package-name@virtual:[ID of virtual package]#npm:1.2.3.
// In this case we need to omit the part of the virtual package ID, to get the key as it is found in the dependencies map.
func getYarnDependencyKeyFromLocator(yarnDepLocator string) string {
	virtualIndex := strings.Index(yarnDepLocator, "@virtual:")
	if virtualIndex == -1 {
		return yarnDepLocator
	}

	hashSignIndex := strings.LastIndex(yarnDepLocator, "#")
	return yarnDepLocator[:virtualIndex+1] + yarnDepLocator[hashSignIndex+1:]
}

func runInfo(executablePath, srcPath string) (outResult, errResult string, err error) {
	command := exec.Command(executablePath, "info", "--all", "--recursive", "--json")
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err = command.Run()
	errResult = errBuffer.String()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			err = errors.New(err.Error())
		}
		return
	}
	outResult = strings.TrimSpace(outBuffer.String())
	return
}

func runYarnCommand(executablePath, srcPath string, args ...string) error {
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

func stringsSliceContains(slice []string, str string) bool {
	for _, element := range slice {
		if element == str {
			return true
		}
	}
	return false
}
