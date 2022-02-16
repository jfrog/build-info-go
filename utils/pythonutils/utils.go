package pythonutils

import (
	"errors"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Parse pythonDependencyPackage list to dependencies map (mapping dependency to his child deps)
// also returns a list of top level dependencies
func parseDependenciesToGraph(packages []pythonDependencyPackage) (map[string][]string, []string, error) {
	// Create packages map.
	packagesMap := map[string][]string{}
	allSubPackages := map[string]bool{}
	for _, pkg := range packages {
		var subPackages []string
		for _, subPkg := range pkg.Dependencies {
			subPkgFullName := subPkg.Key + ":" + subPkg.InstalledVersion
			subPackages = append(subPackages, subPkgFullName)
			allSubPackages[subPkgFullName] = true
		}
		packagesMap[pkg.Package.Key+":"+pkg.Package.InstalledVersion] = subPackages
	}

	var topLevelPackagesList []string
	for pkgName := range packagesMap {
		if allSubPackages[pkgName] == false {
			topLevelPackagesList = append(topLevelPackagesList, pkgName)
		}
	}
	return packagesMap, topLevelPackagesList, nil
}

// Structs for parsing the pip-dependency-map result.
type pythonDependencyPackage struct {
	Package      packageType  `json:"package,omitempty"`
	Dependencies []dependency `json:"dependencies,omitempty"`
}

type packageType struct {
	Key              string `json:"key,omitempty"`
	PackageName      string `json:"package_name,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

type dependency struct {
	Key              string `json:"key,omitempty"`
	PackageName      string `json:"package_name,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

// Before running this function, dependency IDs may be the file names of the resolved python packages.
// Update build info dependency IDs and the requestedBy field.
// allDependencies      - Dependency name to Dependency map
// dependenciesGraph    - Dependency graph as built by 'pipdeptree' or 'pipenv graph'
// topLevelPackagesList - The direct dependencies
// packageName          - The resolved package name of the Python project, may be empty if we couldn't resolve it
// moduleName           - The input module name from the user, or the packageName
func UpdateDepsIdsAndRequestedBy(dependenciesMap map[string]buildinfo.Dependency, packageName, moduleName, dependencyLocalPath, pythonExecPath string) error {
	dependenciesGraph, topLevelPackagesList, err := RunPipDepTree(pythonExecPath, dependencyLocalPath)
	if err != nil {
		return err
	}
	if packageName == "" {
		// Projects without setup.py
		dependenciesGraph[moduleName] = topLevelPackagesList
	} else {
		// Projects with setup.py
		dependenciesGraph[moduleName] = dependenciesGraph[packageName]
	}
	rootModule := buildinfo.Dependency{Id: moduleName, RequestedBy: [][]string{{}}}
	updateDepsIdsAndRequestedBy(rootModule, dependenciesMap, dependenciesGraph)
	return nil
}

func updateDepsIdsAndRequestedBy(parentDependency buildinfo.Dependency, dependenciesMap map[string]buildinfo.Dependency, dependenciesGraph map[string][]string) {
	childrenList := dependenciesGraph[parentDependency.Id]
	for _, childName := range childrenList {
		childKey := childName[0:strings.Index(childName, ":")]
		if childDep, ok := dependenciesMap[childKey]; ok {
			for _, parentRequestedBy := range parentDependency.RequestedBy {
				childRequestedBy := append([]string{parentDependency.Id}, parentRequestedBy...)
				childDep.RequestedBy = append(childDep.RequestedBy, childRequestedBy)
			}
			if childDep.NodeHasLoop() {
				continue
			}
			childDep.Id = childName
			// Reassign map entry with new entry copy
			dependenciesMap[childKey] = childDep
			// Run recursive call on child dependencies
			updateDepsIdsAndRequestedBy(childDep, dependenciesMap, dependenciesGraph)
		}
	}
}

func GetPackageNameFromSetuppy(pythonExecutablePath string) (string, error) {
	filePath, err := getSetupPyFilePath()
	if err != nil || filePath == "" {
		// Error was returned or setup.py does not exist in directory.
		return "", errors.New("Got error trying to get setup.py file: " + err.Error())
	}

	// Extract package name from setup.py.
	packageName, err := ExtractPackageNameFromSetupPy(filePath, pythonExecutablePath)
	if err != nil {
		// If setup.py egg_info command failed we use build name as module name and continue to pip-install execution
		return "", errors.New("Couldn't determine module-name after running the 'egg_info' command: " + err.Error())
	}
	return packageName, nil
}

// Look for 'setup.py' file in current work dir.
// If found, return its absolute path.
func getSetupPyFilePath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	filePath := filepath.Join(wd, "setup.py")
	// Check if setup.py exists.
	validPath, err := utils.IsFileExists(filePath, false)
	if err != nil {
		return "", err
	}
	if !validPath {
		return "", nil
	}

	return filePath, nil
}

// Get the project-name by running 'egg_info' command on setup.py and extracting it from 'PKG-INFO' file.
func ExtractPackageNameFromSetupPy(setuppyFilePath, pythonExecutablePath string) (string, error) {
	// Execute egg_info command and return PKG-INFO content.
	content, err := getEgginfoPkginfoContent(setuppyFilePath, pythonExecutablePath)
	if err != nil {
		return "", err
	}

	// Extract project name from file content.
	return getProjectIdFromFileContent(content)
}

// Run egg-info command on setup.py, the command generates metadata files.
// Return the content of the 'PKG-INFO' file.
func getEgginfoPkginfoContent(setuppyFilePath, pythonExecutablePath string) (output []byte, err error) {
	eggBase, err := utils.CreateTempDir()
	if err != nil {
		return nil, err
	}
	defer func() {
		e := utils.RemoveTempDir(eggBase)
		if err == nil {
			err = e
		}
	}()

	// Run python 'egg_info --egg-base <eggBase>' command.
	if err = exec.Command(pythonExecutablePath, setuppyFilePath, "egg_info", "--egg-base", eggBase).Run(); err != nil {
		return nil, err
	}

	// Read PKG_INFO under <eggBase>/*.egg-info/PKG-INFO.
	return extractPackageNameFromEggBase(eggBase)
}

// Parse the output of 'python egg_info' command, in order to find the path of generated file 'PKG-INFO'.
func extractPackageNameFromEggBase(eggBase string) ([]byte, error) {
	files, err := os.ReadDir(eggBase)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".egg-info") {
			pkginfoPath := filepath.Join(eggBase, file.Name(), "PKG-INFO")
			// Read PKG-INFO file.
			pkginfoFileExists, err := utils.IsFileExists(pkginfoPath, false)
			if err != nil {
				return nil, err
			}
			if !pkginfoFileExists {
				return nil, errors.New("File 'PKG-INFO' couldn't be found in its designated location: " + pkginfoPath)
			}

			return os.ReadFile(pkginfoPath)
		}
	}

	return nil, errors.New("couldn't find pkg info files")
}

// Get package ID from PKG-INFO file content.
// If pattern of package name of version not found, return an error.
func getProjectIdFromFileContent(content []byte) (string, error) {
	// Create package-name regexp.
	packageNameRegexp, err := regexp.Compile(`(?m)^Name:\s(\w[\w-.]+)`)
	if err != nil {
		return "", err
	}

	// Find first nameMatch of packageNameRegexp.
	nameMatch := packageNameRegexp.FindStringSubmatch(string(content))
	if len(nameMatch) < 2 {
		return "", errors.New("Failed extracting package name from content.")
	}

	// Create package-version regexp.
	packageVersionRegexp, err := regexp.Compile(`(?m)^Version:\s(\w[\w-.]+)`)
	if err != nil {
		return "", err
	}

	// Find first match of packageNameRegexp.
	versionMatch := packageVersionRegexp.FindStringSubmatch(string(content))
	if len(versionMatch) < 2 {
		return "", errors.New("Failed extracting package version from content.")
	}

	return nameMatch[1] + ":" + versionMatch[1], nil
}
