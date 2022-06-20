package utils

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/jfrog/build-info-go/utils"
	"github.com/pkg/errors"
	"os/exec"
	"strings"
	"sync"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/parallel"
)

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

// GetYarnDependencies returns a map of the dependencies of a Yarn project and the root package of the project.
// The keys are the packages' values (Yarn's full identifiers of the packages), for example: '@scope/package-name@npm:1.0.0'.
// Pay attention that a package's value won't necessarily contain its version. Use the version in package's details instead.
// Note that in some versions of Yarn, the version of the root package is '0.0.0-use.local', instead of the version in the package.json file.
func GetYarnDependencies(executablePath, srcPath string, packageInfo *PackageInfo, log utils.Log) (dependenciesMap map[string]*YarnDependency, root *YarnDependency, err error) {
	// Run 'yarn info'
	responseStr, errStr, err := runYarnInfo(executablePath, srcPath)
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
		err = nil
	}

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

func runYarnInfo(executablePath, srcPath string) (outResult, errResult string, err error) {
	command := exec.Command(executablePath, "info", "--all", "--recursive", "--json")
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
