package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/gofrog/crypto"

	"golang.org/x/exp/slices"

	"github.com/buger/jsonparser"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
)

const pnpmInstallCommand = "install"
const packageJsonFile = "package.json"
const pnpmLockFile = "pnpm-lock.yaml"
const minPnpmVersion = "6.0.0"

// CalculatePnpmDependenciesList gets a pnpm project's dependencies.
func CalculatePnpmDependenciesList(executablePath, srcPath, moduleId string, pnpmParams PnpmTreeDepListParam, calculateChecksums bool, log utils.Log) ([]entities.Dependency, error) {
	if log == nil {
		log = &utils.NullLog{}
	}
	// Calculate pnpm dependency tree using 'pnpm ls...'.
	dependenciesMap, err := CalculatePnpmDependenciesMap(executablePath, srcPath, moduleId, pnpmParams, log, false)
	if err != nil {
		return nil, err
	}
	var cacache *cacache
	if calculateChecksums {
		// Get local pnpm cache (pnpm uses the same cacache format as npm).
		cacheLocation, err := GetPnpmConfigCache(srcPath, executablePath, log)
		if err != nil {
			return nil, err
		}
		cacache = NewNpmCacache(cacheLocation)
	}
	var dependenciesList []entities.Dependency
	var missingPeerDeps, missingBundledDeps, missingOptionalDeps, otherMissingDeps []string
	for _, dep := range dependenciesMap {
		if dep.Integrity == "" && dep.InBundle {
			missingBundledDeps = append(missingBundledDeps, dep.Id)
			continue
		}
		if dep.Integrity == "" && dep.PeerMissing != nil {
			missingPeerDeps = append(missingPeerDeps, dep.Id)
			continue
		}
		if calculateChecksums {
			dep.Md5, dep.Sha1, dep.Sha256, err = calculatePnpmChecksum(cacache, dep.Name, dep.Version, dep.Integrity)
			if err != nil {
				if dep.Optional {
					missingOptionalDeps = append(missingOptionalDeps, dep.Id)
					continue
				}
				otherMissingDeps = append(otherMissingDeps, dep.Id)
				log.Debug("couldn't calculate checksum for " + dep.Id + ". Error: '" + err.Error() + "'.")
				continue
			}
		}

		dependenciesList = append(dependenciesList, dep.Dependency)
	}
	if len(missingPeerDeps) > 0 {
		printPnpmMissingDependenciesWarning("peerDependency", missingPeerDeps, log)
	}
	if len(missingBundledDeps) > 0 {
		printPnpmMissingDependenciesWarning("bundleDependencies", missingBundledDeps, log)
	}
	if len(missingOptionalDeps) > 0 {
		printPnpmMissingDependenciesWarning("optionalDependencies", missingOptionalDeps, log)
	}
	if len(otherMissingDeps) > 0 {
		log.Warn("The following dependencies will not be included in the build-info, because they are missing in the pnpm store: '" + strings.Join(otherMissingDeps, ",") + "'.\nHint: Try deleting 'node_modules' and/or '" + pnpmLockFile + "'.")
	}
	return dependenciesList, nil
}

type pnpmDependencyInfo struct {
	entities.Dependency
	*pnpmLsDependency
}

// Run 'pnpm list ...' command and parse the returned result to create a dependencies map.
// The dependencies map looks like name:version -> entities.Dependency.
func CalculatePnpmDependenciesMap(executablePath, srcPath, moduleId string, pnpmListParams PnpmTreeDepListParam, log utils.Log, skipInstall bool) (map[string]*pnpmDependencyInfo, error) {
	dependenciesMap := make(map[string]*pnpmDependencyInfo)
	pnpmVersion, err := GetPnpmVersion(executablePath, log)
	if err != nil {
		return nil, err
	}
	nodeModulesExist, err := utils.IsDirExists(filepath.Join(srcPath, "node_modules"), false)
	if err != nil {
		return nil, err
	}
	var data []byte
	if nodeModulesExist && !pnpmListParams.IgnoreNodeModules && !skipInstall {
		data = runPnpmLsWithNodeModules(executablePath, srcPath, pnpmListParams.Args, log)
	} else {
		data, err = runPnpmLsWithoutNodeModules(executablePath, srcPath, pnpmListParams, log, pnpmVersion, skipInstall)
		if err != nil {
			return nil, err
		}
	}
	// pnpm ls --json returns an array with one object containing dependencies
	// We need to extract the first element
	data = extractPnpmLsData(data, log)
	if data == nil {
		return dependenciesMap, nil
	}
	// Parse the dependencies json object.
	return dependenciesMap, jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) (err error) {
		if string(key) == "dependencies" || string(key) == "devDependencies" {
			err = parsePnpmDependencies(value, []string{moduleId}, dependenciesMap, log)
		}
		return err
	})
}

// extractPnpmLsData extracts the dependency data from pnpm ls --json output.
// pnpm ls --json returns an array: [{ "name": "...", "dependencies": {...} }]
func extractPnpmLsData(data []byte, log utils.Log) []byte {
	if len(data) == 0 {
		return nil
	}
	// pnpm ls --json returns an array, get the first element
	var result []byte
	_, err := jsonparser.ArrayEach(data, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		if result == nil {
			result = value
		}
	})
	if err != nil {
		log.Debug("Failed to parse pnpm ls output as array, trying as object: " + err.Error())
		// Maybe it's already an object (older pnpm versions)
		return data
	}
	return result
}

func runPnpmLsWithNodeModules(executablePath, srcPath string, pnpmArgs []string, log utils.Log) (data []byte) {
	pnpmArgs = append(pnpmArgs, "--json", "--long", "--depth", "Infinity")
	data, errData, err := RunPnpmCmd(executablePath, srcPath, AppendPnpmCommand(pnpmArgs, "ls"), log)
	if err != nil {
		log.Warn(err.Error())
	} else if len(errData) > 0 {
		log.Warn("Encountered some issues while running 'pnpm ls' command:\n" + strings.TrimSpace(string(errData)))
	}
	return
}

func runPnpmLsWithoutNodeModules(executablePath, srcPath string, pnpmListParams PnpmTreeDepListParam, log utils.Log, pnpmVersion *version.Version, skipInstall bool) ([]byte, error) {
	installRequired, err := isPnpmInstallRequired(srcPath, pnpmListParams, log, skipInstall)
	if err != nil {
		return nil, err
	}

	if installRequired {
		err = installPnpmLockfile(executablePath, srcPath, pnpmListParams.InstallCommandArgs, pnpmListParams.Args, log, pnpmVersion)
		if err != nil {
			return nil, err
		}
	}
	pnpmListParams.Args = append(pnpmListParams.Args, "--json", "--long", "--depth", "Infinity")
	data, errData, err := RunPnpmCmd(executablePath, srcPath, AppendPnpmCommand(pnpmListParams.Args, "ls"), log)
	if err != nil {
		log.Warn(err.Error())
	} else if len(errData) > 0 {
		log.Warn("Encountered some issues while running 'pnpm ls' command:\n" + strings.TrimSpace(string(errData)))
	}
	return data, nil
}

// isPnpmInstallRequired determines whether a project installation is required.
// Checks if the "pnpm-lock.yaml" file exists in the project directory.
func isPnpmInstallRequired(srcPath string, pnpmListParams PnpmTreeDepListParam, log utils.Log, skipInstall bool) (bool, error) {
	isPnpmLockExist, err := utils.IsFileExists(filepath.Join(srcPath, pnpmLockFile), false)
	if err != nil {
		return false, err
	}

	if len(pnpmListParams.InstallCommandArgs) > 0 {
		return true, nil
	}
	if !isPnpmLockExist || (pnpmListParams.OverwritePackageLock && checkIfPnpmLockFileShouldBeUpdated(srcPath, log)) {
		if skipInstall {
			return false, &utils.ErrProjectNotInstalled{UninstalledDir: srcPath}
		}
		return true, nil
	}
	return false, nil
}

func installPnpmLockfile(executablePath, srcPath string, pnpmInstallCommandArgs, pnpmArgs []string, log utils.Log, pnpmVersion *version.Version) error {
	if pnpmVersion.AtLeast(minPnpmVersion) {
		pnpmArgs = append(pnpmArgs, "--lockfile-only")
		pnpmArgs = append(pnpmArgs, filterPnpmUniqueArgs(pnpmInstallCommandArgs, pnpmArgs)...)
		_, _, err := RunPnpmCmd(executablePath, srcPath, AppendPnpmCommand(pnpmArgs, "install"), log)
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("it looks like you're using version " + pnpmVersion.GetVersion() + " of the pnpm client. Versions below " + minPnpmVersion + " require running `pnpm install` before running this command")
}

// filterPnpmUniqueArgs removes any arguments from argsToFilter that are already present in existingArgs.
func filterPnpmUniqueArgs(argsToFilter []string, existingArgs []string) []string {
	var filteredArgs []string
	for _, arg := range argsToFilter {
		if arg == pnpmInstallCommand {
			continue
		}
		if !slices.Contains(existingArgs, arg) {
			filteredArgs = append(filteredArgs, arg)
		}
	}
	return filteredArgs
}

// Check if package.json has been modified compared to pnpm-lock.yaml.
func checkIfPnpmLockFileShouldBeUpdated(srcPath string, log utils.Log) bool {
	packageJsonInfo, err := os.Stat(filepath.Join(srcPath, packageJsonFile))
	if err != nil {
		log.Warn("Failed to get file info for package.json, err: %v", err)
		return false
	}

	packageJsonInfoModTime := packageJsonInfo.ModTime()
	pnpmLockInfo, err := os.Stat(filepath.Join(srcPath, pnpmLockFile))
	if err != nil {
		log.Warn("Failed to get file info for pnpm-lock.yaml, err: %v", err)
		return false
	}
	pnpmLockInfoModTime := pnpmLockInfo.ModTime()
	return packageJsonInfoModTime.After(pnpmLockInfoModTime)
}

func GetPnpmVersion(executablePath string, log utils.Log) (*version.Version, error) {
	versionData, _, err := RunPnpmCmd(executablePath, "", []string{"--version"}, log)
	if err != nil {
		return nil, err
	}
	return version.NewVersion(strings.TrimSpace(string(versionData))), nil
}

type PnpmTreeDepListParam struct {
	// Required for the 'install' and 'ls' commands
	Args []string
	// Optional user-supplied arguments for the 'install' command
	InstallCommandArgs []string
	// Ignore the node_modules folder if exists
	IgnoreNodeModules bool
	// Rewrite pnpm-lock.yaml, if exists
	OverwritePackageLock bool
}

// pnpm ls --json dependency structure
type pnpmLsDependency struct {
	Name        string
	Version     string
	Path        string
	Resolved    string
	Integrity   string
	InBundle    bool
	Dev         bool
	Optional    bool
	Missing     bool
	Problems    []string
	PeerMissing interface{}
}

// Return name:version of a dependency
func (pld *pnpmLsDependency) id() string {
	return pld.Name + ":" + pld.Version
}

func (pld *pnpmLsDependency) getScopes() (scopes []string) {
	if pld.Dev {
		scopes = append(scopes, "dev")
	} else {
		scopes = append(scopes, "prod")
	}
	if strings.HasPrefix(pld.Name, "@") {
		splitValues := strings.Split(pld.Name, "/")
		if len(splitValues) > 2 {
			scopes = append(scopes, splitValues[0])
		}
	}
	return
}

// Parses pnpm dependencies recursively and adds the collected dependencies to the given dependencies map.
func parsePnpmDependencies(data []byte, pathToRoot []string, dependencies map[string]*pnpmDependencyInfo, log utils.Log) error {
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
		if string(value) == "{}" {
			log.Debug(fmt.Sprintf("%s is missing. This may be the result of an optional dependency.", key))
			return nil
		}
		pnpmLsDep, err := parsePnpmLsDependency(value)
		if err != nil {
			return err
		}
		pnpmLsDep.Name = string(key)

		// Handle git dependencies
		if isGitDependency(pnpmLsDep.Version) {
			if hash := extractVersionFromGitUrl(pnpmLsDep.Version); hash != "" {
				pnpmLsDep.Version = hash
			} else {
				checksums, err := crypto.CalcChecksums(strings.NewReader(pnpmLsDep.Version), crypto.SHA1)
				if err != nil {
					return err
				}
				pnpmLsDep.Version = checksums[crypto.SHA1]
			}
		}

		if pnpmLsDep.Version == "" {
			resolvedUrl := pnpmLsDep.Resolved
			if resolvedUrl == "" && pnpmLsDep.Path != "" {
				resolvedUrl = pnpmLsDep.Path
			}
			switch {
			case resolvedUrl != "":
				checksums, err := crypto.CalcChecksums(strings.NewReader(resolvedUrl), crypto.SHA1)
				if err != nil {
					return err
				}
				if ver := extractVersionFromGitUrl(resolvedUrl); ver != "" {
					pnpmLsDep.Version = ver
				} else {
					pnpmLsDep.Version = checksums[crypto.SHA1]
				}
			case pnpmLsDep.Missing || pnpmLsDep.Problems != nil:
				log.Debug(fmt.Sprintf("%s is missing, this may be the result of a peer dependency.", key))
				return nil
			default:
				return errors.New("failed to parse '" + string(value) + "' from pnpm ls output.")
			}
		}
		appendPnpmDependency(dependencies, pnpmLsDep, pathToRoot)
		transitive, _, _, err := jsonparser.Get(value, "dependencies")
		if err != nil && err.Error() != "Key path not found" {
			return err
		}
		if len(transitive) > 0 {
			if err := parsePnpmDependencies(transitive, append([]string{pnpmLsDep.id()}, pathToRoot...), dependencies, log); err != nil {
				return err
			}
		}
		return nil
	})
}

func parsePnpmLsDependency(data []byte) (*pnpmLsDependency, error) {
	pnpmLsDep := new(pnpmLsDependency)
	return pnpmLsDep, json.Unmarshal(data, pnpmLsDep)
}

func appendPnpmDependency(dependencies map[string]*pnpmDependencyInfo, dep *pnpmLsDependency, pathToRoot []string) {
	depId := dep.id()
	scopes := dep.getScopes()
	if dependencies[depId] == nil {
		dependency := &pnpmDependencyInfo{
			Dependency:       entities.Dependency{Id: depId},
			pnpmLsDependency: dep,
		}
		dependencies[depId] = dependency
	}
	if dependencies[depId].Integrity == "" {
		dependencies[depId].Integrity = dep.Integrity
	}
	dependencies[depId].Scopes = appendPnpmScopes(dependencies[depId].Scopes, scopes)
	dependencies[depId].RequestedBy = append(dependencies[depId].RequestedBy, pathToRoot)
}

// Lookup for a dependency's tarball in pnpm store, and calculate checksum.
func calculatePnpmChecksum(cacache *cacache, name, version, integrity string) (md5 string, sha1 string, sha256 string, err error) {
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
func appendPnpmScopes(oldScopes []string, newScopes []string) []string {
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

func RunPnpmCmd(executablePath, srcPath string, pnpmArgs []string, log utils.Log) (stdResult, errResult []byte, err error) {
	args := make([]string, 0)
	for i := 0; i < len(pnpmArgs); i++ {
		if strings.TrimSpace(pnpmArgs[i]) != "" {
			args = append(args, pnpmArgs[i])
		}
	}
	log.Debug("Running 'pnpm " + strings.Join(pnpmArgs, " ") + "' command.")
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
	log.Verbose("pnpm '" + strings.Join(args, " ") + "' standard output is:\n" + strings.TrimSpace(string(stdResult)))
	return
}

// AppendPnpmCommand appends the pnpm command as the first element in pnpmArgs strings array.
func AppendPnpmCommand(pnpmArgs []string, command string) []string {
	tempArgs := []string{command}
	tempArgs = append(tempArgs, pnpmArgs...)
	return tempArgs
}

func GetPnpmVersionAndExecPath(log utils.Log) (*version.Version, string, error) {
	if log == nil {
		log = &utils.NullLog{}
	}
	pnpmExecPath, err := exec.LookPath("pnpm")
	if err != nil {
		return nil, "", err
	}

	if pnpmExecPath == "" {
		return nil, "", errors.New("could not find the 'pnpm' executable in the system PATH")
	}

	log.Debug("Using pnpm executable:", pnpmExecPath)

	versionData, _, err := RunPnpmCmd(pnpmExecPath, "", []string{"--version"}, log)
	if err != nil {
		return nil, "", err
	}
	return version.NewVersion(strings.TrimSpace(string(versionData))), pnpmExecPath, nil
}

// GetPnpmConfigCache returns the pnpm store/cache path.
// pnpm uses a content-addressable store, typically at ~/.local/share/pnpm/store or ~/.pnpm-store
func GetPnpmConfigCache(srcPath, executablePath string, log utils.Log) (string, error) {
	// pnpm store path
	data, errData, err := RunPnpmCmd(executablePath, srcPath, []string{"store", "path"}, log)
	if err != nil {
		return "", err
	} else if len(errData) > 0 {
		log.Warn("Encountered some issues while running 'pnpm store path' command:\n" + string(errData))
	}
	storePath := strings.TrimSpace(string(data))
	// pnpm store has a different structure than npm cache, but it contains a similar cache
	// The store path returned is like /path/to/store/v3
	// We need to find the cache directory
	cachePath := filepath.Join(filepath.Dir(storePath), "cache")
	found, err := utils.IsDirExists(cachePath, true)
	if err != nil {
		return "", err
	}
	if !found {
		// Try the store path itself as some pnpm versions use different layouts
		found, err = utils.IsDirExists(storePath, true)
		if err != nil {
			return "", err
		}
		if !found {
			return "", errors.New("pnpm store is not found in '" + storePath + "'. Hint: Try running 'pnpm install' first.")
		}
		return storePath, nil
	}
	return cachePath, nil
}

func printPnpmMissingDependenciesWarning(dependencyType string, dependencies []string, log utils.Log) {
	log.Debug("The following dependencies will not be included in the build-info, because the 'pnpm ls' command did not return their integrity.\nThe reason why the version wasn't returned may be because the package is a '" + dependencyType + "', which was not manually installed.\nIt is therefore okay to skip this dependency: " + strings.Join(dependencies, ","))
}
