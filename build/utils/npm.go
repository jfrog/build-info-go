package utils

import (
	"bytes"
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

// CalculateDependenciesList gets an npm project's dependencies.
func CalculateDependenciesList(executablePath, srcPath, moduleId string, npmArgs []string, log utils.Log) (dependenciesList []entities.Dependency, err error) {
	if log == nil {
		log = &utils.NullLog{}
	}
	// Get local npm cache.
	cacheLocation, err := GetNpmConfigCache(srcPath, executablePath, npmArgs, log)
	if err != nil {
		return nil, err
	}
	cacache := NewCacache(cacheLocation)
	prototypeTree, err := CalculatePrototypeTree(executablePath, srcPath, moduleId, npmArgs, cacache, log)
	if err != nil {
		return nil, err
	}
	var missingPeerDeps, missingBundledDeps []string
	for _, dep := range prototypeTree {
		if dep.npmLsDependency.Integrity == "" && dep.npmLsDependency.InBundle {
			missingBundledDeps = append(missingBundledDeps, dep.Id)
			continue
		}
		if dep.npmLsDependency.Integrity == "" && len(dep.PeerMissing) > 0 {
			missingPeerDeps = append(missingPeerDeps, dep.Id)
			continue
		}
		dep.Md5, dep.Sha1, dep.Sha256, err = calculateChecksum(cacache, dep.Name, dep.Version, dep.Integrity, log)
		if err != nil {
			// Here, we don't know where is the tarball (or if it is actually exists in the filesystem) so we can't calculate the dependency checksum.
			// This case happends when the package-lock.json with property '"lockfileVersion": 1,' gets updated to version '"lockfileVersion": 2,' (from npm v6 to npm v7/v8).
			// Seems like the compatibility upgrades may result in dependencies losing their integrity.
			// We use the integrity to get's the dependencies tarball
			log.Error("couldn't calculate checksum for : '" + dep.Id + "'. Try to delete 'node_models' and/or 'package-lock.json'.")
			return nil, err
		}

		dependenciesList = append(dependenciesList, dep.Dependency)
	}
	if len(missingPeerDeps) > 0 {
		log.Info("The following dependencies will not be included in the build-info, because the 'npm ls' command did not return its integrity.\nThe reason why the version wasn't returned may be because the package is a 'peerDependency', which was not manually installed.\n It is therefore okay to skip this dependency: \n" + strings.Join(missingPeerDeps, "\n"))
	}
	if len(missingBundledDeps) > 0 {
		log.Info("The following dependencies will not be included in the build-info, because the 'npm ls' command did not return its integrity.\nThe reason why the version wasn't returned may be because the package is a 'bundleDependencies', which was not manually installed.\n It is therefore okay to skip this dependency." + strings.Join(missingBundledDeps, "\n"))
	}
	return
}

type prototypeNode struct {
	entities.Dependency
	*npmLsDependency
}

// Run 'npm list ...' command and parse the returned result to create a dependencies map of .
// The dependencies map looks like name:version -> entities.Dependency
func CalculatePrototypeTree(executablePath, srcPath, moduleId string, npmArgs []string, cacache *cacache, log utils.Log) (map[string]*prototypeNode, error) {
	dependenciesMap := make(map[string]*prototypeNode)

	// These arguments must be added at the end of the command, to override their other values (if existed in nm.npmArgs)
	npmArgs = append(npmArgs, "--json=true", "--all", "--long")
	data, errData, err := RunNpmCmd(executablePath, srcPath, Ls, npmArgs, log)

	// Some warnings and messages of npm are printed to stderr. They don't cause the command to fail, but we'd want to show them to the user.
	if len(errData) > 0 {
		log.Warn("Some errors occurred while collecting dependencies info:\n" + string(errData))
	}
	if err != nil {
		log.Warn("npm list command failed with error:", err.Error())
	}
	npmVersion, err := GetNpmVersion(executablePath, log)
	if err != nil {
		return nil, err
	}
	parseFunc := parseNpmLsDependencyFunc(npmVersion)

	// Parse the dependencies json object.
	return dependenciesMap, jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) (err error) {
		if string(key) == "dependencies" {
			err = parseDependencies(value, []string{moduleId}, dependenciesMap, cacache, parseFunc, log)
		}
		return err
	})
}

func GetNpmVersion(executablePath string, log utils.Log) (*version.Version, error) {
	versionData, _, err := RunNpmCmd(executablePath, "", Version, nil, log)
	if err != nil {
		return nil, err
	}
	return version.NewVersion(string(versionData)), nil
}

// A partial struct of the dependency json object coming from npm ls output.
type npmLsDependency struct {
	Name      string
	Version   string
	Integrity string
	InBundle  bool
	Dev       bool
	// Missing peer dependency in npm version 7/8
	Missing bool
	// Problems with missing peer dependency in npm version 7/8
	Problems []string
	// Missing  peer dependency in npm version 6
	// Bound to 'legacyNpmLsDependency' struct
	PeerMissing []*peerMissing
}

type legacyNpmLsDependency struct {
	Name        string
	Version     string
	Integrity   string `json:"_integrity,omitempty"`
	InBundle    bool   `json:"_inBundle,omitempty"`
	Dev         bool   `json:"_development,omitempty"`
	PeerMissing []*peerMissing
}

type peerMissing struct {
	RequiredBy string
	Requires   string
}

func (lnld *legacyNpmLsDependency) toNpmLsDependency() *npmLsDependency {
	return &npmLsDependency{
		Name:        lnld.Name,
		Version:     lnld.Version,
		Integrity:   lnld.Integrity,
		InBundle:    lnld.InBundle,
		Dev:         lnld.Dev,
		PeerMissing: lnld.PeerMissing,
	}
}

// Return name:version of a dependency
func (nld *npmLsDependency) id() string {
	return nld.Name + ":" + nld.Version
}

func (nld *npmLsDependency) getScopes() (scopes []string) {
	if nld.Dev {
		scopes = append(scopes, "dev")
	} else {
		scopes = append(scopes, "prod")
	}
	if strings.HasPrefix(nld.Name, "@") {
		splitValues := strings.Split(nld.Name, "/")
		if len(splitValues) > 2 {
			scopes = append(scopes, splitValues[0])
		}
	}
	return
}

// Parses npm dependencies recursively and adds the collected dependencies to the given dependencies map.
func parseDependencies(data []byte, pathToRoot []string, dependencies map[string]*prototypeNode, cacache *cacache, parseFunc func(data []byte) (*npmLsDependency, error), log utils.Log) error {
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
		if string(value) == "{}" {
			// Skip missing optional dependency.
			log.Debug(fmt.Sprintf("%s is missing, this may be the result of an optional dependency.", key))
			return nil
		}
		npmLsDependency, err := parseFunc(value)
		if err != nil {
			return err
		}
		if npmLsDependency.Version == "" {
			if npmLsDependency.Missing || npmLsDependency.Problems != nil {
				// Skip missing peer dependency.
				log.Debug(fmt.Sprintf("%s is missing, this may be the result of an peer dependency.", key))
				return nil
			}
			return errors.New("failed to parse '" + string(value) + "' from npm ls output.")
		}
		if err := appendDependency(dependencies, npmLsDependency, cacache, pathToRoot, log); err != nil {
			return err
		}
		transitive, _, _, err := jsonparser.Get(value, "dependencies")
		if err != nil && err.Error() != "Key path not found" {
			return err
		}
		if len(transitive) > 0 {
			if err := parseDependencies(transitive, append([]string{npmLsDependency.id()}, pathToRoot...), dependencies, cacache, parseFunc, log); err != nil {
				return err
			}
		}
		return nil
	})
}

func parseNpmLsDependencyFunc(npmVersion *version.Version) func(data []byte) (*npmLsDependency, error) {
	// If npm older than v7, use legacy struct for npm ls output.
	if npmVersion.Compare("7.0.0") > 0 {
		return legacyNpmLsDependencyParser
	}
	return npmLsDependencyParser
}

func legacyNpmLsDependencyParser(data []byte) (*npmLsDependency, error) {
	legacyNpmLsDependency := new(legacyNpmLsDependency)
	err := json.Unmarshal(data, &legacyNpmLsDependency)
	if err != nil {
		return nil, err
	}
	return legacyNpmLsDependency.toNpmLsDependency(), nil
}

func npmLsDependencyParser(data []byte) (*npmLsDependency, error) {
	npmLsDependency := new(npmLsDependency)
	return npmLsDependency, json.Unmarshal(data, &npmLsDependency)
}

func appendDependency(dependencies map[string]*prototypeNode, dep *npmLsDependency, cacache *cacache, pathToRoot []string, log utils.Log) (err error) {
	depId := dep.id()
	scopes := dep.getScopes()
	if (dependencies)[depId] == nil {
		dependency := &prototypeNode{
			Dependency:      entities.Dependency{Id: depId},
			npmLsDependency: dep,
		}

		(dependencies)[depId] = dependency
	}
	if (dependencies)[depId].Integrity == "" {
		(dependencies)[depId].Integrity = dep.Integrity
	}
	(dependencies)[depId].Scopes = appendScopes((dependencies)[depId].Scopes, scopes)
	(dependencies)[depId].RequestedBy = append((dependencies)[depId].RequestedBy, pathToRoot)
	return
}

// Lookup for a dependency's tarball in npm cache, and calculate checksum.
// Return (md5,sha1,sha256,error)
func calculateChecksum(cacache *cacache, name, version, integrity string, log utils.Log) (string, string, string, error) {
	if integrity == "" {
		info, err := cacache.GetInfo(name + "@" + version)
		if err != nil {
			return "", "", "", err
		}
		integrity = info.Integrity
	}
	path, err := cacache.GetTarball(integrity)
	if err != nil {
		return "", "", "", err
	}
	return utils.GetFileChecksums(path)
}

// Merge two scopes and remove duplicates.
func appendScopes(oldScopes []string, newScopes []string) []string {
	set := utils.NewStringSet(oldScopes...)
	set.AddAll(newScopes...)
	return set.ToSlice()
}

type NpmCmd int

const (
	Ls NpmCmd = iota
	Config
	Install
	Ci
	Pack
	Version
)

func (nc NpmCmd) String() string {
	return [...]string{"ls", "config", "install", "ci", "pack", "-version"}[nc]
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
	stdResult = outBuffer.Bytes()
	if err != nil {
		err = errors.New("error while running the command :'" + executablePath + " " + strings.Join(cmdArgs, " ") + "'\nError output is:\n" + string(errResult))
		return
	}
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

	versionData, _, err := RunNpmCmd(npmExecPath, "", Version, nil, log)
	if err != nil {
		return nil, "", err
	}
	log.Debug("Using npm version:", string(versionData))
	return version.NewVersion(string(versionData)), npmExecPath, nil
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

// Return the npm cache path.
// Default: Windows: %LocalAppData%\npm-cache, Posix: ~/.npm
func GetNpmConfigCache(srcPath, executablePath string, npmArgs []string, log utils.Log) (string, error) {
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
	found, err := utils.IsDirExists(cachePath, true)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("failed to locate '_cacache' folder in " + cachePath)
	}
	return cachePath, nil
}
