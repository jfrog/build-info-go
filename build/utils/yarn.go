package utils

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/parallel"
	"github.com/jfrog/gofrog/version"
	"golang.org/x/exp/maps"
	"os/exec"
	"strings"
	"sync"
)

const yarnV2Version = "2.0.0"

// Executes traverseDependenciesFunc on all dependencies in dependenciesMap. Each dependency that gets true in return, is added to dependenciesList.
func TraverseDependencies(dependenciesMap map[string]*entities.Dependency, traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error), threads int) (dependenciesList []entities.Dependency, err error) {
	producerConsumer := parallel.NewBounedRunner(threads, false)
	dependenciesChan := make(chan *entities.Dependency)
	errorChan := make(chan error, 1)

	go func() {
		defer producerConsumer.Done()
		for _, dep := range dependenciesMap {
			handlerFunc := createHandlerFunc(dep, dependenciesChan, traverseDependenciesFunc)
			_, err = producerConsumer.AddTaskWithError(handlerFunc, func(err error) {
				// Write the error to the channel, but don't wait if the channel buffer is full.
				select {
				case errorChan <- err:
				default:
					return
				}
			})
			if err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for dep := range dependenciesChan {
			dependenciesList = append(dependenciesList, *dep)
		}
		wg.Done()
	}()

	producerConsumer.Run()
	if err != nil {
		return
	}
	close(dependenciesChan)
	wg.Wait()

	// Read the error from the channel, but don't wait if the channel buffer is empty.
	select {
	case err = <-errorChan:
	default:
		return
	}
	return
}

// createHandlerFunc creates a function that runs traverseDependenciesFunc (if it's not nil) with dep as its parameter.
// If traverseDependenciesFunc returns false, then dep will not be saved in the module's dependencies list.
func createHandlerFunc(dep *entities.Dependency, dependenciesChan chan *entities.Dependency, traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)) func(threadId int) error {
	return func(threadId int) error {
		var err error
		saveDep := true
		if traverseDependenciesFunc != nil {
			saveDep, err = traverseDependenciesFunc(dep)
			if err != nil {
				return err
			}
		}
		if saveDep {
			dependenciesChan <- dep
		}
		return nil
	}
}

func GetYarnExecutable() (string, error) {
	yarnExecPath, err := exec.LookPath("yarn")
	if err != nil {
		return "", err
	}
	return yarnExecPath, nil
}

// Returns a map of the dependencies of a Yarn project along with the root package of the project.
// The map's keys are the full identifiers of the packages as used by Yarn; for example:
// '@scope/package-name@1.0.0' (for Yarn versions < 2.0.0) or '@scope/package-name@npm:1.0.0' (for Yarn versions >= 2.0.0).
// Note that a package's value may not necessarily contain its version; instead, use the version in the package's details.
// Arguments:
// executablePath - The path to the Yarn executable.
// srcPath - The path to the project's source that we wish to work with.
// packageInfo - The project's package information.
// log - The logger.
// allowPartialResults - If true, the function will allow some errors to occur without failing the flow and will generate partial results.
func GetYarnDependencies(executablePath, srcPath string, packageInfo *PackageInfo, log utils.Log, allowPartialResults bool) (dependenciesMap map[string]*YarnDependency, root *YarnDependency, err error) {
	executableVersionStr, err := GetVersion(executablePath, srcPath)
	if err != nil {
		return
	}

	isV2AndAbove := version.NewVersion(executableVersionStr).Compare(yarnV2Version) <= 0

	// Run 'yarn info or list'
	responseStr, errStr, err := runYarnInfoOrList(executablePath, srcPath, isV2AndAbove)
	// Some warnings and messages of Yarn are printed to stderr. They don't necessarily cause the command to fail, but we'd want to show them to the user.
	if len(errStr) > 0 {
		log.Warn("An error occurred while collecting dependencies info:\n" + errStr)
	}
	if err != nil {
		log.Warn("An error was thrown while collecting dependencies info: " + err.Error() + "\nCommand output:\n" + responseStr)

		// A returned error doesn't necessarily mean that the operation totally failed. If, in addition, the response is empty, then it probably failed.
		if responseStr == "" {
			return
		}
	}

	if isV2AndAbove {
		dependenciesMap, root, err = buildYarnV2DependencyMap(packageInfo, responseStr)
	} else {
		dependenciesMap, root, err = buildYarnV1DependencyMap(packageInfo, responseStr, allowPartialResults, log)
	}
	return
}

// GetVersion getVersion gets the current project's yarn version
func GetVersion(executablePath, srcPath string) (string, error) {
	command := exec.Command(executablePath, "--version")
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err := command.Run()
	if _, ok := err.(*exec.ExitError); ok {
		err = errors.New("An error occurred while attempting to run the 'yarn --version' command. The executed yarn version could not be retrieved:\n" + err.Error())
	}
	return strings.TrimSpace(outBuffer.String()), err
}

// Builds a map of dependencies for Yarn versions < 2.0.0.
// Note that in Yarn < 2.0.0, the project itself, along with its direct dependencies, is not present when running the
// command 'yarn list'; therefore, the root is built manually.
// Arguments:
// packageInfo - The project's package information.
// responseStr - The response string from the 'yarn list' command.
// allowPartialResults - If true, the function will allow some errors to occur without failing the flow and will generate partial results.
// log - The logger.
func buildYarnV1DependencyMap(packageInfo *PackageInfo, responseStr string, allowPartialResults bool, log utils.Log) (dependenciesMap map[string]*YarnDependency, root *YarnDependency, err error) {
	dependenciesMap = make(map[string]*YarnDependency)
	var depTree Yarn1Data
	err = json.Unmarshal([]byte(responseStr), &depTree)
	if err != nil {
		err = errors.New("couldn't parse 'yarn list' results in order to create the dependencyMap:\n" + err.Error())
		return
	}

	if depTree.Data.DepTree == nil {
		err = errors.New("received an empty tree field while parsing 'yarn list' results")
		return
	}

	packNameToFullName := make(map[string]string)

	// Initializing dependencies map without child dependencies for each dependency + creating a map that maps: package-name -> package-name@version
	// The two phases mapping is performed since the responseStr from 'yarn list' contains the resolved versions of the dependencies at the map's first level, but may contain the caret version range (^) at the children level.
	// Therefore, a manual matching must be made in order to output a map with parent-child relation containing only resolved versions.
	for _, curDependency := range depTree.Data.DepTree {
		var packageCleanName, packageVersion string
		packageCleanName, packageVersion, err = splitNameAndVersion(curDependency.Name)
		if err != nil {
			return
		}
		// We insert to dependenciesMap dependencies with the resolved versions only. All dependencies at the responseStr first level contain resolved versions only (their children may contain caret version ranges).
		dependenciesMap[curDependency.Name] = &YarnDependency{
			Value:   curDependency.Name,
			Details: YarnDepDetails{Version: packageVersion},
		}
		packNameToFullName[packageCleanName] = curDependency.Name
	}
	log.Debug(fmt.Sprintf("'yarn list' output string: %s\n\nPackage name to full name map content: %v", responseStr, packNameToFullName))

	// Adding child dependencies for each dependency
	for _, curDependency := range depTree.Data.DepTree {
		dependencyToUpdateInMap := dependenciesMap[curDependency.Name]

		for _, subDep := range curDependency.Dependencies {
			var subDepName string
			subDepName, _, err = splitNameAndVersion(subDep.DependencyName)
			if err != nil {
				return
			}

			packageWithResolvedVersion := packNameToFullName[subDepName]
			if packageWithResolvedVersion == "" {
				if allowPartialResults {
					log.Warn(fmt.Sprintf("error occurred during Yarn dependencies map calculation: couldn't find resolved version for '%s' in 'yarn list' output\nFinal rasults may be partial", subDep.DependencyName))
					continue
				}

				err = fmt.Errorf("couldn't find resolved version for '%s' in 'yarn list' output", subDep.DependencyName)
				return
			}
			dependencyToUpdateInMap.Details.Dependencies = append(dependencyToUpdateInMap.Details.Dependencies, YarnDependencyPointer{subDep.DependencyName, packageWithResolvedVersion})
		}
		dependenciesMap[curDependency.Name] = dependencyToUpdateInMap
	}

	rootDependency := buildYarn1Root(packageInfo, packNameToFullName)
	root = &rootDependency
	dependenciesMap[root.Value] = root
	return
}

// Builds a map of dependencies for Yarn version >= 2.0.0
// Note that in some versions of Yarn, the version of the root package is '0.0.0-use.local', instead of the version in the package.json file.
func buildYarnV2DependencyMap(packageInfo *PackageInfo, responseStr string) (dependenciesMap map[string]*YarnDependency, root *YarnDependency, err error) {
	dependenciesMap = make(map[string]*YarnDependency)
	scanner := bufio.NewScanner(strings.NewReader(responseStr))

	for scanner.Scan() {
		var currDependency YarnDependency
		currDepBytes := scanner.Bytes()
		err = json.Unmarshal(currDepBytes, &currDependency)
		if err != nil {
			return
		}

		// Check whether this dependency's name starts with the package name (which means this is the root)
		if strings.HasPrefix(currDependency.Value, packageInfo.FullName()+"@") {
			// In some versions of Yarn, the version of the root project returned from the 'yarn info' command is '0.0.0-use.local', instead of the version mentioned in the package.json.
			// It was fixed in later versions of Yarn.
			if currDependency.Details.Version == "0.0.0-use.local" {
				currDependency.Details.Version = packageInfo.Version
			}
			root = &currDependency
		}
		dependenciesMap[currDependency.Value] = &currDependency
	}
	return
}

// Depending on the Yarn version currently in use for the project, this function runs the command that retrieves the project's dependencies
func runYarnInfoOrList(executablePath string, srcPath string, v2AndAbove bool) (outResult, errResult string, err error) {
	var command *exec.Cmd
	if v2AndAbove {
		command = exec.Command(executablePath, "info", "--all", "--recursive", "--json")
	} else {
		command = exec.Command(executablePath, "list", "--json", "--flat", "--no-progress")
	}
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err = command.Run()
	outResult = strings.TrimSpace(outBuffer.String())
	errResult = errBuffer.String()

	if err != nil {
		// urfave/cli (aka codegangsta) exits when an ExitError is returned, so if it's an ExitError we'll convert it to a regular error.
		if _, ok := err.(*exec.ExitError); ok {
			err = errors.New(err.Error())
		}
		return
	}
	return
}

// GetYarnDependencyKeyFromLocator gets a Yarn dependency locator and returns its key in the dependencies map.
// Yarn dependency locator usually looks like this: package-name@npm:1.2.3, which is used as the key in the dependencies map.
// But sometimes it points to a virtual package, so it looks different: package-name@virtual:[ID of virtual package]#npm:1.2.3.
// In this case we need to omit the part of the virtual package ID, to get the key as it is found in the dependencies map.
func GetYarnDependencyKeyFromLocator(yarnDepLocator string) string {
	virtualIndex := strings.Index(yarnDepLocator, "@virtual:")
	if virtualIndex == -1 {
		return yarnDepLocator
	}

	hashSignIndex := strings.LastIndex(yarnDepLocator, "#")
	return yarnDepLocator[:virtualIndex+1] + yarnDepLocator[hashSignIndex+1:]
}

// buildYarn1Root builds the root of the project's dependency tree (from direct dependencies in package.json)
func buildYarn1Root(packageInfo *PackageInfo, packNameToFullName map[string]string) YarnDependency {
	rootDependency := YarnDependency{
		Value:   packageInfo.Name,
		Details: YarnDepDetails{Version: packageInfo.Version},
	}

	rootDeps := maps.Keys(packageInfo.Dependencies)
	rootDeps = append(rootDeps, maps.Keys(packageInfo.DevDependencies)...)
	rootDeps = append(rootDeps, maps.Keys(packageInfo.PeerDependencies)...)
	rootDeps = append(rootDeps, maps.Keys(packageInfo.OptionalDependencies)...)

	for _, directDepName := range rootDeps {
		if fullPackageName, packageExist := packNameToFullName[directDepName]; packageExist {
			rootDependency.Details.Dependencies = append(rootDependency.Details.Dependencies, YarnDependencyPointer{Locator: fullPackageName})
		}
	}
	return rootDependency
}

// splitNameAndVersion splits package name for package version for th following formats ONLY: package-name@version, package-name@npm:version
func splitNameAndVersion(packageFullName string) (packageCleanName string, packageVersion string, err error) {
	packageFullName = strings.Replace(packageFullName, "npm:", "", 1)
	indexOfLastAt := strings.LastIndex(packageFullName, "@")
	if indexOfLastAt == -1 {
		err = errors.New("received package name of incorrect format (expected: package-name@version)")
		return
	}
	packageVersion = packageFullName[indexOfLastAt+1:]
	packageCleanName = packageFullName[:indexOfLastAt]

	if strings.Contains(packageCleanName, "@") {
		// Packages may have '@' at their name due to package scoping or package aliasing
		// In order to process the dependencies correctly we need names without aliases or unique naming conventions that exist in some packages
		packageCleanName = removeAliasingFromPackageName(packageCleanName)
	}
	return
}

func removeAliasingFromPackageName(packageNameToClean string) string {
	indexOfLastAt := strings.Index(packageNameToClean[1:], "@")

	if indexOfLastAt == -1 {
		// This case covers scoped package: @my-package
		return packageNameToClean
	}
	// This case covers an aliased package: @my-package@other-dependent-package --> @my-package / my-package@other-dependent-package --> my-package
	return packageNameToClean[:indexOfLastAt+1]
}

type Yarn1Data struct {
	Data Yarn1DependencyTree `json:"data,omitempty"`
}

type Yarn1DependencyTree struct {
	DepTree []Yarn1DependencyDetails `json:"trees,omitempty"`
}

type Yarn1DependencyDetails struct {
	Name         string                   `json:"name,omitempty"`
	Dependencies []Yarn1DependencyPointer `json:"children,omitempty"`
}

type Yarn1DependencyPointer struct {
	DependencyName string `json:"name,omitempty"`
	Shadow         bool   `json:"shadow,omitempty"`
}

type YarnDependency struct {
	// The value is usually in this structure: @scope/package-name@npm:1.0.0
	Value   string         `json:"value,omitempty"`
	Details YarnDepDetails `json:"children,omitempty"`
}

func (yd *YarnDependency) Name() string {
	// Find the first index of '@', starting from position 1. In scoped dependencies (like '@jfrog/package-name@npm:1.2.3') we want to keep the first '@' as part of the name.
	if strings.Contains(yd.Value[1:], "@") {
		atSignIndex := strings.Index(yd.Value[1:], "@") + 1
		return yd.Value[:atSignIndex]
	}
	// In some cases when using yarn V1 we encounter package names without their version (project's package name)
	return yd.Value
}

type YarnDepDetails struct {
	Version      string                  `json:"Version,omitempty"`
	Dependencies []YarnDependencyPointer `json:"Dependencies,omitempty"`
}

type YarnDependencyPointer struct {
	Descriptor string `json:"descriptor,omitempty"`
	Locator    string `json:"locator,omitempty"`
}
