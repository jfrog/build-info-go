package pythonutils

import (
	"errors"
	"path/filepath"
	"strings"

	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const (
	Pip    = "pip"
	Pipenv = "pipenv"
	Poetry = "poetry"
)

type PythonTool string

// Parse pythonDependencyPackage list to dependencies map. (mapping dependency to his child deps)
// Also returns a list of project's root dependencies
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
		if !allSubPackages[pkgName] {
			topLevelPackagesList = append(topLevelPackagesList, pkgName)
		}
	}
	return packagesMap, topLevelPackagesList, nil
}

// Structs for parsing the pip-dependency-map result.
type pythonDependencyPackage struct {
	Package      packageType   `json:"package,omitempty"`
	Dependencies []packageType `json:"dependencies,omitempty"`
}

type packageType struct {
	Key              string `json:"key,omitempty"`
	PackageName      string `json:"package_name,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

func GetPythonDependencies(tool PythonTool, srcPath, localDependenciesPath string) (dependenciesGraph map[string][]string, topLevelDependencies []string, err error) {
	switch tool {
	case Pip:
		return getPipDependencies(srcPath, localDependenciesPath)
	case Pipenv:
		return getPipenvDependencies(srcPath)
	case Poetry:
		return getPoetryDependencies(srcPath)
	default:
		return nil, nil, errors.New(string(tool) + " commands are not supported.")
	}
}

func GetPackageName(tool PythonTool, srcPath string) (packageName string, err error) {
	switch tool {
	case Pip, Pipenv:
		return getPackageNameFromSetuppy(srcPath)
	case Poetry:
		packageName, _, err = getPackageNameFromPyproject(srcPath)
		return
	default:
		return "", errors.New(string(tool) + " commands are not supported.")
	}
}

// Before running this function, dependency IDs may be the file names of the resolved python packages.
// Update build info dependency IDs and the requestedBy field.
// allDependencies      - Dependency name to Dependency map
// dependenciesGraph    - Dependency graph as built by 'pipdeptree' or 'pipenv graph'
// topLevelPackagesList - The direct dependencies
// packageName          - The resolved package name of the Python project, may be empty if we couldn't resolve it
// moduleName           - The input module name from the user, or the packageName
func UpdateDepsIdsAndRequestedBy(dependenciesMap map[string]buildinfo.Dependency, dependenciesGraph map[string][]string, topLevelPackagesList []string, packageName, moduleName string) {
	if packageName == "" {
		// Projects without setup.py
		dependenciesGraph[moduleName] = topLevelPackagesList
	} else if packageName != moduleName {
		// Projects with setup.py
		dependenciesGraph[moduleName] = dependenciesGraph[packageName]
	}
	rootModule := buildinfo.Dependency{Id: moduleName, RequestedBy: [][]string{{}}}
	updateDepsIdsAndRequestedBy(rootModule, dependenciesMap, dependenciesGraph)
}

func updateDepsIdsAndRequestedBy(parentDependency buildinfo.Dependency, dependenciesMap map[string]buildinfo.Dependency, dependenciesGraph map[string][]string) {
	for _, childId := range dependenciesGraph[parentDependency.Id] {
		childName := childId[0:strings.Index(childId, ":")]
		if childDep, ok := dependenciesMap[childName]; ok {
			if childDep.NodeHasLoop() || len(childDep.RequestedBy) >= buildinfo.RequestedByMaxLength {
				continue
			}
			// Update RequestedBy field from parent's RequestedBy.
			childDep.UpdateRequestedBy(parentDependency.Id, parentDependency.RequestedBy)

			// Set dependency type
			if childDep.Type == "" {
				fileType := ""
				if i := strings.LastIndex(childDep.Id, ".tar."); i != -1 {
					fileType = childDep.Id[i+1:]
				} else if i := strings.LastIndex(childDep.Id, "."); i != -1 {
					fileType = childDep.Id[i+1:]
				}
				childDep.Type = fileType
			}
			// Convert Id field from filename to dependency id
			childDep.Id = childId
			// Reassign map entry with new entry copy
			dependenciesMap[childName] = childDep
			// Run recursive call on child dependencies
			updateDepsIdsAndRequestedBy(childDep, dependenciesMap, dependenciesGraph)
		}
	}
}

func getFilePath(srcPath, fileName string) (string, error) {
	filePath := filepath.Join(srcPath, fileName)
	// Check if fileName exists.
	validPath, err := utils.IsFileExists(filePath, false)
	if err != nil || !validPath {
		return "", err
	}
	return filePath, nil
}
