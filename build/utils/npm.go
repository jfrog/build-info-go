package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jfrog/gofrog/crypto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/buger/jsonparser"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
)

const npmInstallCommand = "install"

// CalculateNpmDependenciesList gets an npm project's dependencies.
func CalculateNpmDependenciesList(executablePath, srcPath, moduleId string, npmParams NpmTreeDepListParam, calculateChecksums bool, log utils.Log) ([]entities.Dependency, error) {
	if log == nil {
		log = &utils.NullLog{}
	}
	// Calculate npm dependency tree using 'npm ls...'.
	dependenciesMap, err := CalculateDependenciesMap(executablePath, srcPath, moduleId, npmParams, log, false)
	if err != nil {
		return nil, err
	}
	var cacache *cacache
	if calculateChecksums {
		// Get local npm cache.
		cacheLocation, err := GetNpmConfigCache(srcPath, executablePath, npmParams.Args, log)
		if err != nil {
			return nil, err
		}
		cacache = NewNpmCacache(cacheLocation)
	}
	var dependenciesList []entities.Dependency
	var missingPeerDeps, missingBundledDeps, missingOptionalDeps, otherMissingDeps []string
	for _, dep := range dependenciesMap {
		if dep.npmLsDependency.Integrity == "" && dep.npmLsDependency.InBundle {
			missingBundledDeps = append(missingBundledDeps, dep.Id)
			continue
		}
		if dep.npmLsDependency.Integrity == "" && dep.PeerMissing != nil {
			missingPeerDeps = append(missingPeerDeps, dep.Id)
			continue
		}
		if calculateChecksums {
			dep.Md5, dep.Sha1, dep.Sha256, err = calculateChecksum(cacache, dep.Name, dep.Version, dep.Integrity)
			if err != nil {
				if dep.Optional {
					missingOptionalDeps = append(missingOptionalDeps, dep.Id)
					continue
				}
				// Here, we don't know where is the tarball (or if it is actually exists in the filesystem) so we can't calculate the dependency checksum.
				// This case happens when the package-lock.json with property '"lockfileVersion": 1,' gets updated to version '"lockfileVersion": 2,' (from npm v6 to npm v7/v8).
				// Seems like the compatibility upgrades may result in dependencies losing their integrity.
				// We use the integrity to get the dependencies tarball
				otherMissingDeps = append(otherMissingDeps, dep.Id)
				log.Debug("couldn't calculate checksum for " + dep.Id + ". Error: '" + err.Error() + "'.")
				continue
			}
		}

		dependenciesList = append(dependenciesList, dep.Dependency)
	}
	if len(missingPeerDeps) > 0 {
		printMissingDependenciesWarning("peerDependency", missingPeerDeps, log)
	}
	if len(missingBundledDeps) > 0 {
		printMissingDependenciesWarning("bundleDependencies", missingBundledDeps, log)
	}
	if len(missingOptionalDeps) > 0 {
		printMissingDependenciesWarning("optionalDependencies", missingOptionalDeps, log)
	}
	if len(otherMissingDeps) > 0 {
		log.Warn("The following dependencies will not be included in the build-info, because they are missing in the npm cache: '" + strings.Join(otherMissingDeps, ",") + "'.\nHint: Try deleting 'node_modules' and/or 'package-lock.json'.")
	}
	return dependenciesList, nil
}

type dependencyInfo struct {
	entities.Dependency
	*npmLsDependency
}

// Run 'npm list ...' command and parse the returned result to create a dependencies map of.
// The dependencies map looks like name:version -> entities.Dependency.
func CalculateDependenciesMap(executablePath, srcPath, moduleId string, npmListParams NpmTreeDepListParam, log utils.Log, skipInstall bool) (map[string]*dependencyInfo, error) {
	dependenciesMap := make(map[string]*dependencyInfo)
	// These arguments must be added at the end of the command, to override their other values (if existed in nm.npmArgs).
	npmVersion, err := GetNpmVersion(executablePath, log)
	if err != nil {
		return nil, err
	}
	nodeModulesExist, err := utils.IsDirExists(filepath.Join(srcPath, "node_modules"), false)
	if err != nil {
		return nil, err
	}
	var data []byte
	// When `skipInstall` is true, we aim to rely on the dependencies specified in the package-lock, so Frogbot will not execute 'npm ls' on modules that are unbuilt and lack lock files (which may still have incomplete node_modules that could cause errors).
	if nodeModulesExist && !npmListParams.IgnoreNodeModules && !skipInstall {
		data = runNpmLsWithNodeModules(executablePath, srcPath, npmListParams.Args, log)
	} else {
		// If we don't have node_modules, the function will use the package-lock dependencies.
		data, err = runNpmLsWithoutNodeModules(executablePath, srcPath, npmListParams, log, npmVersion, skipInstall)
		if err != nil {
			return nil, err
		}
	}
	parseFunc := parseNpmLsDependencyFunc(npmVersion)
	// Parse the dependencies json object.
	return dependenciesMap, jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) (err error) {
		if string(key) == "dependencies" {
			err = parseDependencies(value, []string{moduleId}, dependenciesMap, parseFunc, log)
		}
		return err
	})
}

func runNpmLsWithNodeModules(executablePath, srcPath string, npmArgs []string, log utils.Log) (data []byte) {
	npmArgs = append(npmArgs, "--json", "--all", "--long")
	data, errData, err := RunNpmCmd(executablePath, srcPath, AppendNpmCommand(npmArgs, "ls"), log)
	if err != nil {
		// It is optional for the function to return this error.
		log.Warn(err.Error())
	} else if len(errData) > 0 {
		log.Warn("Encountered some issues while running 'npm ls' command:\n" + strings.TrimSpace(string(errData)))
	}
	return
}

func runNpmLsWithoutNodeModules(executablePath, srcPath string, npmListParams NpmTreeDepListParam, log utils.Log, npmVersion *version.Version, skipInstall bool) ([]byte, error) {
	installRequired, err := isInstallRequired(srcPath, npmListParams, log, skipInstall)
	if err != nil {
		return nil, err
	}

	if installRequired {
		err = installPackageLock(executablePath, srcPath, npmListParams.InstallCommandArgs, npmListParams.Args, log, npmVersion)
		if err != nil {
			return nil, err
		}
	}
	npmListParams.Args = append(npmListParams.Args, "--json", "--all", "--long", "--package-lock-only")
	data, errData, err := RunNpmCmd(executablePath, srcPath, AppendNpmCommand(npmListParams.Args, "ls"), log)
	if err != nil {
		log.Warn(err.Error())
	} else if len(errData) > 0 {
		log.Warn("Encountered some issues while running 'npm ls' command:\n" + strings.TrimSpace(string(errData)))
	}
	return data, nil
}

// This function determines whether a project installation is required by evaluating the following criteria:
// 1) Checks if the "package-lock.json" file exists in the project directory.
// 2) Verifies if an installation command was provided by the user.
// 3) Checks if the lock file should be updated due to an override request.
//
// Conditions for triggering installation:
// - If the user provided an installation command, installation is required.
// - If the "package-lock.json" file is missing or an override request to update the lock file exists, installation is required, unless the user explicitly requested to skip the installation.
//
// If installation is required but skipped by the user's request, an error is returned.
func isInstallRequired(srcPath string, npmListParams NpmTreeDepListParam, log utils.Log, skipInstall bool) (bool, error) {
	isPackageLockExist, err := utils.IsFileExists(filepath.Join(srcPath, "package-lock.json"), false)
	if err != nil {
		return false, err
	}

	if len(npmListParams.InstallCommandArgs) > 0 {
		return true, nil
	}
	if !isPackageLockExist || (npmListParams.OverwritePackageLock && checkIfLockFileShouldBeUpdated(srcPath, log)) {
		if skipInstall {
			return false, &utils.ErrProjectNotInstalled{UninstalledDir: srcPath}
		}
		return true, nil
	}
	return false, nil
}

func installPackageLock(executablePath, srcPath string, npmInstallCommandArgs, npmArgs []string, log utils.Log, npmVersion *version.Version) error {
	if npmVersion.AtLeast("6.0.0") {
		npmArgs = append(npmArgs, "--package-lock-only")
		// Including any 'install' command flags that were supplied by the user in preceding steps of the process, while ensuring that duplicates are avoided.
		npmArgs = append(npmArgs, filterUniqueArgs(npmInstallCommandArgs, npmArgs)...)
		// Installing package-lock to generate the dependencies map.
		_, _, err := RunNpmCmd(executablePath, srcPath, AppendNpmCommand(npmArgs, "install"), log)
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("it looks like you’re using version " + npmVersion.GetVersion() + " of the npm client. Versions below 6.0.0 require running `npm install` before running this command")
}

// Removes any arguments from argsToFilter that are already present in existingArgs. Furthermore, excludes the "install" command and retains only the flags in the resulting argument list.
func filterUniqueArgs(argsToFilter []string, existingArgs []string) []string {
	var filteredArgs []string
	for _, arg := range argsToFilter {
		if arg == npmInstallCommand {
			continue
		}
		if !slices.Contains(existingArgs, arg) {
			filteredArgs = append(filteredArgs, arg)
		}
	}
	return filteredArgs
}

// Check if package.json has been modified.
// This might indicate the addition of new packages to package.json that haven't been reflected in package-lock.json.
func checkIfLockFileShouldBeUpdated(srcPath string, log utils.Log) bool {
	packageJsonInfo, err := os.Stat(filepath.Join(srcPath, "package.json"))
	if err != nil {
		log.Warn("Failed to get file info for package.json, err: %v", err)
		return false
	}

	packageJsonInfoModTime := packageJsonInfo.ModTime()
	packageLockInfo, err := os.Stat(filepath.Join(srcPath, "package-lock.json"))
	if err != nil {
		log.Warn("Failed to get file info for package-lock.json, err: %v", err)
		return false
	}
	packageLockInfoModTime := packageLockInfo.ModTime()
	return packageJsonInfoModTime.After(packageLockInfoModTime)
}

func GetNpmVersion(executablePath string, log utils.Log) (*version.Version, error) {
	versionData, _, err := RunNpmCmd(executablePath, "", []string{"--version"}, log)
	if err != nil {
		return nil, err
	}
	return version.NewVersion(string(versionData)), nil
}

type NpmTreeDepListParam struct {
	// Required for the 'install' and 'ls' commands that could be triggered during the construction of the NPM dependency tree
	Args []string
	// Optional user-supplied arguments for the 'install' command. These arguments are not available from all entry points. They may be employed when constructing the NPM dependency tree, which could necessitate the execution of 'npm install...'
	InstallCommandArgs []string
	// Ignore the node_modules folder if exists, using the '--package-lock-only' flag
	IgnoreNodeModules bool
	// Rewrite package-lock.json, if exists.
	OverwritePackageLock bool
}

// npm >=7 ls results for a single dependency
type npmLsDependency struct {
	Name      string
	Version   string
	Integrity string
	InBundle  bool
	Dev       bool
	Optional  bool
	// Missing peer dependency in npm version 7/8
	Missing bool
	// Problems with missing peer dependency in npm version 7/8
	Problems []string
	// Missing  peer dependency in npm version 6
	// Bound to 'legacyNpmLsDependency' struct
	PeerMissing interface{}
}

// npm 6 ls results for a single dependency
type legacyNpmLsDependency struct {
	Name          string
	Version       string
	Missing       bool
	Integrity     string `json:"_integrity,omitempty"`
	InBundle      bool   `json:"_inBundle,omitempty"`
	Dev           bool   `json:"_development,omitempty"`
	InnerOptional bool   `json:"_optional,omitempty"`
	Optional      bool
	PeerMissing   interface{}
}

func (lnld *legacyNpmLsDependency) optional() bool {
	if lnld.Optional {
		return true
	}
	return lnld.InnerOptional
}

func (lnld *legacyNpmLsDependency) toNpmLsDependency() *npmLsDependency {
	return &npmLsDependency{
		Name:        lnld.Name,
		Version:     lnld.Version,
		Integrity:   lnld.Integrity,
		InBundle:    lnld.InBundle,
		Dev:         lnld.Dev,
		Optional:    lnld.optional(),
		Missing:     lnld.Missing,
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
func parseDependencies(data []byte, pathToRoot []string, dependencies map[string]*dependencyInfo, parseFunc func(data []byte) (*npmLsDependency, error), log utils.Log) error {
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
		if string(value) == "{}" {
			// Skip missing optional dependency.
			log.Debug(fmt.Sprintf("%s is missing. This may be the result of an optional dependency.", key))
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
		appendDependency(dependencies, npmLsDependency, pathToRoot)
		transitive, _, _, err := jsonparser.Get(value, "dependencies")
		if err != nil && err.Error() != "Key path not found" {
			return err
		}
		if len(transitive) > 0 {
			if err := parseDependencies(transitive, append([]string{npmLsDependency.id()}, pathToRoot...), dependencies, parseFunc, log); err != nil {
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

func appendDependency(dependencies map[string]*dependencyInfo, dep *npmLsDependency, pathToRoot []string) {
	depId := dep.id()
	scopes := dep.getScopes()
	if dependencies[depId] == nil {
		dependency := &dependencyInfo{
			Dependency:      entities.Dependency{Id: depId},
			npmLsDependency: dep,
		}

		dependencies[depId] = dependency
	}
	if dependencies[depId].Integrity == "" {
		dependencies[depId].Integrity = dep.Integrity
	}
	dependencies[depId].Scopes = appendScopes(dependencies[depId].Scopes, scopes)
	dependencies[depId].RequestedBy = append(dependencies[depId].RequestedBy, pathToRoot)
}

// Lookup for a dependency's tarball in npm cache, and calculate checksum.
func calculateChecksum(cacache *cacache, name, version, integrity string) (md5 string, sha1 string, sha256 string, err error) {
	if integrity == "" {
		var info *cacacheInfo
		info, err = cacache.GetInfo(name + "@" + version)
		if err != nil {
			return
		}
		integrity = info.Integrity
	}
	var path string
	path, err = cacache.GetTarball(integrity)
	if err != nil {
		return
	}
	checksums, err := crypto.GetFileChecksums(path)
	if err != nil {
		return
	}
	return checksums[crypto.MD5], checksums[crypto.SHA1], checksums[crypto.SHA256], err
}

// Merge two scopes and remove duplicates.
func appendScopes(oldScopes []string, newScopes []string) []string {
	contained := make(map[string]bool)
	allScopes := []string{}
	for _, scope := range append(oldScopes, newScopes...) {
		if scope == "" {
			continue
		}
		if !contained[scope] {
			allScopes = append(allScopes, scope)
		}
		contained[scope] = true
	}
	return allScopes
}

func RunNpmCmd(executablePath, srcPath string, npmArgs []string, log utils.Log) (stdResult, errResult []byte, err error) {
	args := make([]string, 0)
	for i := 0; i < len(npmArgs); i++ {
		if strings.TrimSpace(npmArgs[i]) != "" {
			args = append(args, npmArgs[i])
		}
	}
	log.Debug("Running 'npm " + strings.Join(npmArgs, " ") + "' command.")
	command := exec.Command(executablePath, args...)
	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err = command.Run()
	errResult = errBuffer.Bytes()
	stdResult = outBuffer.Bytes()
	if err != nil {
		err = fmt.Errorf("error while running '%s %s': %s\n%s", executablePath, strings.Join(args, " "), err.Error(), strings.TrimSpace(string(errResult)))
		return
	}
	log.Debug("npm '" + strings.Join(args, " ") + "' standard output is:\n" + strings.TrimSpace(string(stdResult)))
	return
}

// This function appends the Npm command as the first element in npmArgs strings array.
// For example, if npmArgs equals {"--json", "--all"}, and we call appendNpmCommand(npmArgs, "ls"), we will get npmArgs = {"ls", "--json", "--all"}.
func AppendNpmCommand(npmArgs []string, command string) []string {
	termpArgs := []string{command}
	termpArgs = append(termpArgs, npmArgs...)
	return termpArgs
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

	versionData, _, err := RunNpmCmd(npmExecPath, "", []string{"--version"}, log)
	if err != nil {
		return nil, "", err
	}
	return version.NewVersion(strings.TrimSpace(string(versionData))), npmExecPath, nil
}

type PackageInfo struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	Scope                string
}

// Read and populate package name, version and scope from the package.json file in the provided directory.
// If package.json does not exist, return an empty PackageInfo struct.
func ReadPackageInfoFromPackageJsonIfExists(packageJsonDirectory string, npmVersion *version.Version) (*PackageInfo, error) {
	packageJson, err := os.ReadFile(filepath.Join(packageJsonDirectory, "package.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &PackageInfo{}, nil
		} else {
			return nil, err
		}
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
	if pi.Name == "" || pi.Version == "" {
		return ""
	}
	nameBase := fmt.Sprintf("%s:%s", pi.Name, pi.Version)
	if pi.Scope == "" {
		return nameBase
	}
	return fmt.Sprintf("%s:%s", strings.TrimPrefix(pi.Scope, "@"), nameBase)
}

func (pi *PackageInfo) GetDeployPath() string {
	fileName := fmt.Sprintf("%s-%s.tgz", pi.Name, pi.Version)
	// The hyphen part below "/-/" is there in order to follow the layout used by the public NPM registry.
	if pi.Scope == "" {
		return fmt.Sprintf("%s/-/%s", pi.Name, fileName)
	}
	return fmt.Sprintf("%s/%s/-/%s/%s", pi.Scope, pi.Name, pi.Scope, fileName)
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
	data, errData, err := RunNpmCmd(executablePath, srcPath, AppendNpmCommand(append(npmArgs, "--json=false"), "config"), log)
	if err != nil {
		return "", err
	} else if len(errData) > 0 {
		// Some warnings and messages of npm are printed to stderr. They don't cause the command to fail, but we'd want to show them to the user.
		log.Warn("Encountered some issues while running 'npm get cache' command:\n" + string(errData))
	}
	cachePath := filepath.Join(strings.Trim(string(data), "\n"), "_cacache")
	found, err := utils.IsDirExists(cachePath, true)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("_cacache folder is not found in '" + cachePath + "'. Hint: Delete node_modules directory and run npm install or npm ci.")
	}
	return cachePath, nil
}

func printMissingDependenciesWarning(dependencyType string, dependencies []string, log utils.Log) {
	log.Debug("The following dependencies will not be included in the build-info, because the 'npm ls' command did not return their integrity.\nThe reason why the version wasn't returned may be because the package is a '" + dependencyType + "', which was not manually installed.\nIt is therefore okay to skip this dependency: " + strings.Join(dependencies, ","))
}
