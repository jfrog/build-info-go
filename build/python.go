package build

import (
	"fmt"
	"os"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils/pythonutils"
)

type PythonModule struct {
	containingBuild            *Build
	tool                       pythonutils.PythonTool
	name                       string
	srcPath                    string
	localDependenciesPath      string
	updateDepsChecksumInfoFunc func(dependenciesMap map[string]entities.Dependency, srcPath string) error
}

func newPythonModule(srcPath string, tool pythonutils.PythonTool, containingBuild *Build) (*PythonModule, error) {
	var err error
	if srcPath == "" {
		srcPath, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	return &PythonModule{srcPath: srcPath, containingBuild: containingBuild, tool: tool}, nil
}

func (pm *PythonModule) RunInstallAndCollectDependencies(commandArgs []string) error {
	dependenciesMap, err := pythonutils.GetPythonDependenciesFiles(pm.tool, commandArgs, pm.containingBuild.logger, pm.srcPath)
	if err != nil {
		return err
	}
	dependenciesGraph, topLevelPackagesList, err := pythonutils.GetPythonDependencies(pm.tool, pm.srcPath, pm.localDependenciesPath)
	if err != nil {
		return fmt.Errorf("failed while attempting to get %s dependencies graph: %s", pm.tool, err.Error())
	}
	// Get package-name.
	packageName, pkgNameErr := pythonutils.GetPackageName(pm.tool, pm.srcPath)
	if pkgNameErr != nil {
		pm.containingBuild.logger.Debug("Couldn't retrieve the package name. Reason:", pkgNameErr.Error())
	}
	// If module-name was set by the command, don't change it.
	if pm.name == "" {
		// If the package name is unknown, set the module name to be the build name.
		pm.name = packageName
		if pm.name == "" {
			pm.name = pm.containingBuild.buildName
			pm.containingBuild.logger.Debug(fmt.Sprintf("Using build name: %s as module name.", pm.name))
		}
	}
	if pm.updateDepsChecksumInfoFunc != nil {
		err = pm.updateDepsChecksumInfoFunc(dependenciesMap, pm.srcPath)
		if err != nil {
			return err
		}
	}
	pythonutils.UpdateDepsIdsAndRequestedBy(dependenciesMap, dependenciesGraph, topLevelPackagesList, packageName, pm.name)
	buildInfoModule := entities.Module{Id: pm.name, Type: entities.Python, Dependencies: dependenciesMapToList(dependenciesMap)}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}

	return pm.containingBuild.SaveBuildInfo(buildInfo)
}

// Run install command while parsing the logs for downloaded packages.
// Populates 'downloadedDependencies' with downloaded package-name and its actual downloaded file (wheel/egg/zip...).
func (pm *PythonModule) InstallWithLogParsing(commandArgs []string) (map[string]entities.Dependency, error) {
	return pythonutils.InstallWithLogParsing(pm.tool, commandArgs, pm.containingBuild.logger, pm.srcPath)
}

func (pm *PythonModule) SetName(name string) {
	pm.name = name
}

func (pm *PythonModule) SetLocalDependenciesPath(localDependenciesPath string) {
	pm.localDependenciesPath = localDependenciesPath
}

func (pm *PythonModule) SetUpdateDepsChecksumInfoFunc(updateDepsChecksumInfoFunc func(dependenciesMap map[string]entities.Dependency, srcPath string) error) {
	pm.updateDepsChecksumInfoFunc = updateDepsChecksumInfoFunc
}
