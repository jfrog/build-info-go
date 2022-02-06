package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
)

type TypeRestriction int

const (
	DefaultRestriction TypeRestriction = iota
	All
	DevOnly
	ProdOnly
)

// A partial (dependencies only) struct of the package-lock.json file.
type packageLock struct {
	// This is an object that maps dependencies locations in the node_modules folder to an object containing the information about that dependency.
	// The root project is typically listed with a key of "", and all other dependencies are listed with their relative paths (e.g. node_modules/dependency_name).
	Dependencies map[string]dependencyInfo `json:"Packages,omitempty"`

	// Legacy data structure of Dependencies, used for backward compitability with npm v6.
	LegacyDependencies map[string]dependencyInfo `json:"dependencies,omitempty"`
}

// A dependency (package) info structure as in package-lock.json
type dependencyInfo struct {
	Version   string
	Integrity string
}

func newPackageLock(srcPath string) (*packageLock, error) {
	packageLock := packageLock{}
	path := filepath.Join(srcPath, "package-lock.json")
	exists, err := utils.IsFileExists(path, false)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("failed to calculate npm dependencies tree, package-lock.json isn't found in " + path)
	}
	return &packageLock, utils.Unmarshal(path, &packageLock)
}

// Returns a map of dependency-name:dependency-version -> integrity from the package-lock.json
func (pl *packageLock) getIntegrityMap() map[string]string {
	idToIntegrity := make(map[string]string)
	if pl.Dependencies != nil {
		for name, info := range pl.Dependencies {
			idx := strings.LastIndex(name, "node_modules/")
			if idx == -1 {
				continue
			}
			name := name[idx+len("node_modules/"):]
			idToIntegrity[name+":"+info.Version] = info.Integrity
		}
		return idToIntegrity
	}
	for k, v := range pl.LegacyDependencies {
		idToIntegrity[k+":"+v.Version] = v.Integrity
	}
	return idToIntegrity
}

// cachePath - the npm global cache dir.
// integrity - the dependency integrity.
// Return the path of a dependency tarball based on its integrity.
func getTarball(cachePath, integrity string) (string, error) {
	hashAlgorithms, hash, err := integrityToSha(integrity)
	if err != nil {
		return "", err
	}
	if len(hash) < 5 {
		return "", errors.New("failed to calculate npm dependencies tree, bad dependency integrity " + integrity)
	}
	tarballPath := filepath.Join(cachePath, "content-v2", hashAlgorithms, hash[0:2], hash[2:4], hash[4:])
	found, err := utils.IsFileExists(tarballPath, false)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("failed to locate dependency integrity '" + integrity + " 'tarball at " + tarballPath)
	}
	return tarballPath, nil
}

func integrityToSha(integrity string) (string, string, error) {
	data := strings.SplitN(integrity, "-", 2)
	if len(data) != 2 {
		return "", "", errors.New("the integrity '" + integrity + "' has bad format (valid format is HashAlgorithms-Hash)")
	}
	hashAlgorithm, hash := data[0], data[1]
	decoded, err := base64.StdEncoding.DecodeString(hash)
	return hashAlgorithm, hex.EncodeToString(decoded), err
}

// Return the local npm cache dir.
// it uses the command 'npm config get cache'
func getNpmConfigCache(srcPath, executablePath string, npmArgs []string, log utils.Log) (string, error) {
	npmArgs = append([]string{"get", "cache"}, npmArgs...)
	data, errData, err := RunNpmCmd(executablePath, srcPath, Config, append(npmArgs, "--json=false"), log)
	// Some warnings and messages of npm are printed to stderr. They don't cause the command to fail, but we'd want to show them to the user.
	if len(errData) > 0 {
		log.Warn("Some errors occurred while collecting dependencies info:\n" + string(errData))
	}
	if err != nil {
		return "", errors.New(fmt.Sprintf("npm config command failed with an error: %s", err.Error()))
	}
	cachePath := filepath.Join(strings.Trim(string(data), "\n"), "_cacache")
	found, err := utils.IsDirExists(cachePath, false)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("failed to locate '_cacache' folder in " + cachePath)
	}
	return cachePath, nil
}

// CalculateDependenciesList gets an npm project's dependencies.
// It sends each of them as a parameter to traverseDependenciesFunc and based on its return values, they will be returned from CalculateDependenciesList.
func CalculateDependenciesList(typeRestriction TypeRestriction, executablePath, srcPath, moduleId string, npmArgs []string, log utils.Log) ([]entities.Dependency, error) {
	if log == nil {
		log = &utils.NullLog{}
	}
	dependenciesMap, err := createDependenciesList(typeRestriction, executablePath, srcPath, moduleId, npmArgs, log)
	if err != nil {
		return nil, err
	}
	return calculateChecksum(dependenciesMap, executablePath, srcPath, moduleId, npmArgs, log)
}

func createDependenciesList(typeRestriction TypeRestriction, executablePath, srcPath, moduleId string, npmArgs []string, log utils.Log) (dependenciesMap map[string]*entities.Dependency, err error) {
	dependenciesMap = make(map[string]*entities.Dependency)
	if typeRestriction != ProdOnly {
		if err = prepareDependencies("dev", executablePath, srcPath, moduleId, npmArgs, dependenciesMap, log); err != nil {
			return
		}
	}
	if typeRestriction != DevOnly {
		err = prepareDependencies("prod", executablePath, srcPath, moduleId, npmArgs, dependenciesMap, log)
	}
	return
}

func calculateChecksum(dependenciesMap map[string]*entities.Dependency, executablePath, srcPath, moduleId string, npmArgs []string, log utils.Log) ([]entities.Dependency, error) {
	packageLock, err := newPackageLock(srcPath)
	if err != nil {
		return nil, err
	}
	integrityMap := packageLock.getIntegrityMap()
	cacheLocation, err := getNpmConfigCache(srcPath, executablePath, npmArgs, log)
	if err != nil {
		return nil, err
	}
	var dependenciesList []entities.Dependency
	for _, dependency := range dependenciesMap {
		path, err := getTarball(cacheLocation, integrityMap[dependency.Id])
		if err != nil {
			return nil, err
		}
		dependency.Md5, dependency.Sha1, dependency.Sha256, err = utils.GetFileChecksums(path)
		if err != nil {
			return nil, err
		}
		dependenciesList = append(dependenciesList, *dependency)
	}
	return dependenciesList, nil
}

// Run npm list and parse the returned JSON.
// typeRestriction must be one of: 'dev' or 'prod'!
func prepareDependencies(typeRestriction, executablePath, srcPath, moduleId string, npmArgs []string, results map[string]*entities.Dependency, log utils.Log) error {
	// Run npm list
	// Although this command can get --development as a flag (according to npm docs), it's not working on npm 6.
	// Although this command can get --only=development as a flag (according to npm docs), it's not working on npm 7.
	// These arguments must be added at the end of the command, to override their other values (if existed in nm.npmArgs)
	npmArgs = append(npmArgs, "--json=true", "--all", "--"+typeRestriction)
	data, errData, err := RunNpmCmd(executablePath, srcPath, Ls, npmArgs, log)
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
func parseDependencies(data []byte, scope string, pathToRoot []string, dependencies map[string]*entities.Dependency, log utils.Log) error {
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

func appendDependency(dependencies map[string]*entities.Dependency, depId, scope string, pathToRoot []string) {
	if (dependencies)[depId] == nil {
		(dependencies)[depId] = &entities.Dependency{Id: depId, Scopes: []string{scope}}
	} else if !scopeAlreadyExists(scope, (dependencies)[depId].Scopes) {
		(dependencies)[depId].Scopes = append((dependencies)[depId].Scopes, scope)
	}
	(dependencies)[depId].RequestedBy = append((dependencies)[depId].RequestedBy, pathToRoot)
}

func scopeAlreadyExists(scope string, existingScopes []string) bool {
	for _, existingScope := range existingScopes {
		if existingScope == scope {
			return true
		}
	}
	return false
}

type NpmCmd int

const (
	Ls NpmCmd = iota
	Config
	Install
)

func (nc NpmCmd) String() string {
	return [...]string{"ls", "config", "install"}[nc]
}

func RunNpmCmd(executablePath, srcPath string, npmCmd NpmCmd, npmArgs []string, log utils.Log) (stdResult, errResult []byte, err error) {
	log.Debug("Running npm " + npmCmd.String() + " command.")
	cmdArgs := []string{npmCmd.String()}
	cmdArgs = append(cmdArgs, npmArgs...)

	command := exec.Command(executablePath, cmdArgs...)
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err = command.Run()
	errResult = errBuffer.Bytes()
	if err != nil {
		err = errors.New("error while running the command :'" + executablePath + " " + strings.Join(cmdArgs, " ") + "'\nError output is:\n" + string(errResult))
		return
	}
	stdResult = outBuffer.Bytes()
	log.Debug("npm " + npmCmd.String() + " standard output is:\n" + string(stdResult))
	return
}

func GetNpmVersionAndExecPath(log utils.Log) (*version.Version, string, error) {
	if log == nil {
		log = &utils.NullLog{}
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
	if npmVersion != nil && npmVersion.Compare("7.0.0") > 0 {
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
