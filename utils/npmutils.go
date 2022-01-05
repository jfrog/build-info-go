package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/buger/jsonparser"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/parallel"
	"github.com/jfrog/gofrog/version"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type TypeRestriction int

const (
	DefaultRestriction TypeRestriction = iota
	All
	DevOnly
	ProdOnly
)

// CalculateDependenciesList gets an npm project's dependencies.
// It sends each of them as a parameter to traverseDependenciesFunc and based on its return values, they will be returned from CalculateDependenciesList.
func CalculateDependenciesList(typeRestriction TypeRestriction, executablePath, srcPath, moduleId string, npmArgs []string, traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error), threads int, log Log) (dependenciesList []entities.Dependency, err error) {
	if log == nil {
		log = &NullLog{}
	}
	dependenciesMap := make(map[string]*entities.Dependency)
	if typeRestriction != ProdOnly {
		if err = prepareDependencies("dev", executablePath, srcPath, moduleId, npmArgs, &dependenciesMap, log); err != nil {
			return
		}
	}
	if typeRestriction != DevOnly {
		if err = prepareDependencies("prod", executablePath, srcPath, moduleId, npmArgs, &dependenciesMap, log); err != nil {
			return
		}
	}

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

// Run npm list and parse the returned JSON.
// typeRestriction must be one of: 'dev' or 'prod'!
func prepareDependencies(typeRestriction, executablePath, srcPath, moduleId string, npmArgs []string, results *map[string]*entities.Dependency, log Log) error {
	// Run npm list
	// Although this command can get --development as a flag (according to npm docs), it's not working on npm 6.
	// Although this command can get --only=development as a flag (according to npm docs), it's not working on npm 7.
	data, errData, err := runList(typeRestriction, executablePath, srcPath, npmArgs, log)
	// Some warnings and messages of npm are printed to stderr. They don't cause the command to fail, but we'd want to show them to the user.
	if len(errData) > 0 {
		log.Warn("Some errors occurred while collecting dependencies info:\n" + string(errData))
	}
	if err != nil {
		return errors.New(fmt.Sprintf("npm list command failed with an error: %s", err.Error()))
	}

	// Parse the dependencies json object
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) (err error) {
		if string(key) == "dependencies" {
			err = parseDependencies(value, typeRestriction, []string{moduleId}, results, log)
		}
		return err
	})
}

// Parses npm dependencies recursively and adds the collected dependencies to the given dependencies map.
func parseDependencies(data []byte, scope string, pathToRoot []string, dependencies *map[string]*entities.Dependency, log Log) error {
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
		depName := string(key)
		ver, _, _, err := jsonparser.Get(data, depName, "version")
		if err != nil && err != jsonparser.KeyPathNotFoundError {
			return err
		} else if err == jsonparser.KeyPathNotFoundError {
			log.Debug(fmt.Sprintf("%s dependency will not be included in the build-info, because the 'npm ls' command did not return its version.\nThe reason why the version wasn't returned may be because the package is a 'peerdependency', which was not manually installed.\n'npm install' does not download 'peerdependencies' automatically. It is therefore okay to skip this dependency.", depName))
		}
		depVersion := string(ver)
		depId := depName + ":" + depVersion
		if err == nil {
			appendDependency(dependencies, depId, scope, pathToRoot)
		}
		transitive, _, _, err := jsonparser.Get(data, depName, "dependencies")
		if err != nil && err.Error() != "Key path not found" {
			return err
		}
		if len(transitive) > 0 {
			if err := parseDependencies(transitive, scope, append([]string{depId}, pathToRoot...), dependencies, log); err != nil {
				return err
			}
		}
		return nil
	})
}

func appendDependency(dependencies *map[string]*entities.Dependency, depId, scope string, pathToRoot []string) {
	if (*dependencies)[depId] == nil {
		(*dependencies)[depId] = &entities.Dependency{Id: depId, Scopes: []string{scope}}
	} else if !scopeAlreadyExists(scope, (*dependencies)[depId].Scopes) {
		(*dependencies)[depId].Scopes = append((*dependencies)[depId].Scopes, scope)
	}
	(*dependencies)[depId].RequestedBy = append((*dependencies)[depId].RequestedBy, pathToRoot)
}

func scopeAlreadyExists(scope string, existingScopes []string) bool {
	for _, existingScope := range existingScopes {
		if existingScope == scope {
			return true
		}
	}
	return false
}

func runList(typeRestriction, executablePath, srcPath string, npmArgs []string, log Log) (stdResult, errResult []byte, err error) {
	log.Debug("Running npm list command.")
	cmdArgs := []string{"list"}
	cmdArgs = append(cmdArgs, npmArgs...)

	// These arguments must be added at the end of the command, to override their other values (if existed in nm.npmArgs)
	cmdArgs = append(cmdArgs, "--json=true", "--all", "--"+typeRestriction)

	command := exec.Command(executablePath, cmdArgs...)
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err = command.Run()
	errResult = errBuffer.Bytes()
	log.Debug("npm list error output is:\n" + string(errResult))
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			err = errors.New(err.Error())
		}
		return
	}
	stdResult = outBuffer.Bytes()
	log.Debug("npm list standard output is:\n" + string(stdResult))
	return
}

func GetNpmVersionAndExecPath(log Log) (*version.Version, string, error) {
	if log == nil {
		log = &NullLog{}
	}
	npmExecPath, err := exec.LookPath("npm")
	if err != nil {
		return nil, "", err
	}

	if npmExecPath == "" {
		return nil, "", errors.New("could not find the 'npm' executable in the system PATH")
	}

	log.Debug("Using npm executable:", npmExecPath)

	npmVersion, err := getVersion(npmExecPath)
	if err != nil {
		return nil, "", err
	}
	log.Debug("Using npm version:", npmVersion)
	return version.NewVersion(npmVersion), npmExecPath, nil
}

func getVersion(executablePath string) (string, error) {
	command := exec.Command(executablePath, "-version")
	buffer := bytes.NewBuffer([]byte{})
	command.Stderr = buffer
	command.Stdout = buffer
	err := command.Run()
	if _, ok := err.(*exec.ExitError); ok {
		err = errors.New(err.Error())
	}
	return buffer.String(), err
}

type PackageInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Scope   string
}

func ReadPackageInfoFromPackageJson(packageJsonDirectory string, npmVersion *version.Version) (*PackageInfo, error) {
	packageJson, err := ioutil.ReadFile(filepath.Join(packageJsonDirectory, "package.json"))
	if err != nil {
		return nil, err
	}
	return ReadPackageInfo(packageJson, npmVersion)
}

func ReadPackageInfo(data []byte, npmVersion *version.Version) (*PackageInfo, error) {
	parsedResult := new(PackageInfo)
	if err := json.Unmarshal(data, parsedResult); err != nil {
		return nil, err
	}
	// If npm older than v7, remove prefixes.
	if npmVersion == nil || npmVersion.Compare("7.0.0") > 0 {
		removeVersionPrefixes(parsedResult)
	}
	splitScopeFromName(parsedResult)
	return parsedResult, nil
}

func (pi *PackageInfo) BuildInfoModuleId() string {
	nameBase := fmt.Sprintf("%s:%s", pi.Name, pi.Version)
	if pi.Scope == "" {
		return nameBase
	}
	return fmt.Sprintf("%s:%s", strings.TrimPrefix(pi.Scope, "@"), nameBase)
}

func (pi *PackageInfo) GetDeployPath() string {
	fileName := fmt.Sprintf("%s-%s.tgz", pi.Name, pi.Version)
	if pi.Scope == "" {
		return fmt.Sprintf("%s/-/%s", pi.Name, fileName)
	}
	return fmt.Sprintf("%s/%s/-/%s", pi.Scope, pi.Name, fileName)
}

func (pi *PackageInfo) FullName() string {
	if pi.Scope == "" {
		return pi.Name
	}
	return fmt.Sprintf("%s/%s", pi.Scope, pi.Name)
}

func splitScopeFromName(packageInfo *PackageInfo) {
	if strings.HasPrefix(packageInfo.Name, "@") && strings.Contains(packageInfo.Name, "/") {
		splitValues := strings.Split(packageInfo.Name, "/")
		packageInfo.Scope = splitValues[0]
		packageInfo.Name = splitValues[1]
	}
}

// A leading "=" or "v" character is stripped off and ignored by npm.
func removeVersionPrefixes(packageInfo *PackageInfo) {
	packageInfo.Version = strings.TrimPrefix(packageInfo.Version, "v")
	packageInfo.Version = strings.TrimPrefix(packageInfo.Version, "=")
}
