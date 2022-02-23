package pythonutils

import (
	"errors"
	buildinfo "github.com/jfrog/build-info-go/entities"
)

const (
	Pip    = "pip"
	Pipenv = "pipenv"
)

type PythonTool string

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

func GetPythonDependencies(tool PythonTool, pythonExecPath, localDependenciesPath string) (dependenciesGraph map[string][]string, topLevelDependencies []string, err error) {
	switch tool {
	case Pip:
		return getPipDependencies(pythonExecPath, localDependenciesPath)
	case Pipenv:
		return getPipenvDependencies()
	default:
		return nil, nil, errors.New(string(tool) + " commands are not supported.")
	}
}

// Before running this function, dependency IDs may be the file names of the resolved python packages.
// Update build info dependency IDs and the requestedBy field.
// allDependencies      - Dependency name to Dependency map
// dependenciesGraph    - Dependency graph as built by 'pipdeptree' or 'pipenv graph'
// topLevelPackagesList - The direct dependencies
// packageName          - The resolved package name of the Python project, may be empty if we couldn't resolve it
// moduleName           - The input module name from the user, or the packageName
func UpdateDepsIdsAndRequestedBy(dependenciesMap map[string]buildinfo.Dependency, dependenciesGraph map[string][]string, topLevelPackagesList []string, packageName, moduleName string) error {
	if packageName == "" {
		// Projects without setup.py
		dependenciesGraph[moduleName] = topLevelPackagesList
	} else {
		// Projects with setup.py
		dependenciesGraph[moduleName] = dependenciesGraph[packageName]
	}
	rootModule := buildinfo.Dependency{Id: moduleName, RequestedBy: [][]string{{}}}
	updateDepsIdsAndRequestedBy(moduleName, rootModule, dependenciesMap, dependenciesGraph)
	return nil
}

func updateDepsIdsAndRequestedBy(parentName string, parentDependency buildinfo.Dependency, dependenciesMap map[string]buildinfo.Dependency, dependenciesGraph map[string][]string) {
	for _, childName := range dependenciesGraph[parentName] {
		//childKey := childName[0:strings.Index(childName, ":")]
		if childDep, ok := dependenciesMap[childName]; ok {
			if childDep.NodeHasLoop() || len(childDep.RequestedBy) >= buildinfo.RequestedByMaxLength {
				continue
			}
			for _, parentRequestedBy := range parentDependency.RequestedBy {
				childRequestedBy := append([]string{parentName}, parentRequestedBy...)
				childDep.RequestedBy = append(childDep.RequestedBy, childRequestedBy)
			}
			childDep.Id = childName
			// Reassign map entry with new entry copy
			dependenciesMap[childName] = childDep
			// Run recursive call on child dependencies
			updateDepsIdsAndRequestedBy(childName, childDep, dependenciesMap, dependenciesGraph)
		}
	}
}
