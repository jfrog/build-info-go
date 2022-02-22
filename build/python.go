package build

import (
	"errors"
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/build-info-go/utils/pythonutils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"os"
	"regexp"
	"strings"
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

func (pm *PythonModule) RunCommandAndCollectDependencies(cmdName string, commandArgs []string) error {
	if cmdName == "install" {
		downloadedFilesMap, err := pm.InstallWithLogParsing(cmdName, commandArgs)
		if err != nil {
			return err
		}
		pythonExecPath, err := utils.GetExecutablePath("python")
		if err != nil {
			return err
		}
		dependenciesGraph, topLevelPackagesList, err := pythonutils.GetPythonDependencies(pm.tool, pythonExecPath, pm.localDependenciesPath)
		if err != nil {
			return err
		}
		dependenciesMap := make(map[string]entities.Dependency, len(dependenciesGraph))
		for depId := range dependenciesGraph {
			depName := depId[0:strings.Index(depId, ":")]
			dependenciesMap[depId] = entities.Dependency{Id: downloadedFilesMap[depName]}
		}
		// Get package-name.
		packageName, pkgNameErr := pythonutils.GetPackageNameFromSetuppy(pythonExecPath)
		if pkgNameErr != nil {
			pm.containingBuild.logger.Debug("Couldn't retrieve the package name from Setup.py. Reason: ", pkgNameErr.Error())
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
		err = pythonutils.UpdateDepsIdsAndRequestedBy(dependenciesMap, dependenciesGraph, topLevelPackagesList, packageName, pm.name)
		if err != nil {
			return err
		}
		buildInfoModule := entities.Module{Id: pm.name, Type: entities.Python, Dependencies: dependenciesMapToList(dependenciesMap)}
		buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}

		return pm.containingBuild.SaveBuildInfo(buildInfo)
	}
	return nil
}

// Run install command while parsing the logs for downloaded packages.
// Supports running pip either in non-verbose and verbose mode.
// Populates 'dependencyToFileMap' with downloaded package-name and its actual downloaded file (wheel/egg/zip...).
func (pm *PythonModule) InstallWithLogParsing(cmdName string, commandArgs []string) (map[string]string, error) {
	log := pm.containingBuild.logger
	if pm.tool == pythonutils.Pipenv {
		// Add verbosity flag to pipenv commands to collect neccecery data
		commandArgs = append(commandArgs, "-v")
	}
	pipCmd, err := utils.NewCmd(string(pm.tool), cmdName, commandArgs)
	if err != nil {
		return nil, err
	}

	// Create regular expressions for log parsing.
	collectingRegexp, err := regexp.Compile(`^Collecting\s(\w[\w-.]+)`)
	if err != nil {
		return nil, err
	}
	downloadingRegexp, err := regexp.Compile(`^\s*Downloading\s([^\s]*)\s\(`)
	if err != nil {
		return nil, err
	}
	usingCachedRegexp, err := regexp.Compile(`^\s*Using\scached\s([\S]+)\s\(`)
	if err != nil {
		return nil, err
	}
	alreadySatisfiedRegexp, err := regexp.Compile(`^Requirement\salready\ssatisfied:\s(\w[\w-.]+)`)
	if err != nil {
		return nil, err
	}

	downloadedDependencies := make(map[string]string)
	var packageName string
	expectingPackageFilePath := false

	// Extract downloaded package name.
	dependencyNameParser := gofrogcmd.CmdOutputPattern{
		RegExp: collectingRegexp,
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// If this pattern matched a second time before downloaded-file-name was found, prompt a message.
			if expectingPackageFilePath {
				// This may occur when a package-installation file is saved in pip-cache-dir, thus not being downloaded during the installation.
				// Re-running pip-install with 'no-cache-dir' fixes this issue.
				log.Debug(fmt.Sprintf("Could not resolve download path for package: %s, continuing...", packageName))

				// Save package with empty file path.
				downloadedDependencies[strings.ToLower(packageName)] = ""
			}

			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 < 0 {
				log.Debug(fmt.Sprintf("Failed extracting package name from line: %s", pattern.Line))
				return pattern.Line, nil
			}

			// Save dependency information.
			expectingPackageFilePath = true
			packageName = pattern.MatchedResults[1]

			return pattern.Line, nil
		},
	}

	// Extract downloaded file, stored in Artifactory.
	downloadedFileParser := gofrogcmd.CmdOutputPattern{
		RegExp: downloadingRegexp,
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 < 0 {
				log.Debug(fmt.Sprintf("Failed extracting download path from line: %s", pattern.Line))
				return pattern.Line, nil
			}

			// If this pattern matched before package-name was found, do not collect this path.
			if !expectingPackageFilePath {
				log.Debug(fmt.Sprintf("Could not resolve package name for download path: %s , continuing...", packageName))
				return pattern.Line, nil
			}

			// Save dependency information.
			filePath := pattern.MatchedResults[1]
			lastSlashIndex := strings.LastIndex(filePath, "/")
			var fileName string
			if lastSlashIndex == -1 {
				fileName = filePath
			} else {
				fileName = filePath[lastSlashIndex+1:]
			}
			downloadedDependencies[strings.ToLower(packageName)] = fileName
			expectingPackageFilePath = false

			log.Debug(fmt.Sprintf("Found package: %s installed with: %s", packageName, fileName))
			return pattern.Line, nil
		},
	}

	cachedFileParser := gofrogcmd.CmdOutputPattern{
		RegExp:   usingCachedRegexp,
		ExecFunc: downloadedFileParser.ExecFunc,
	}

	// Extract already installed packages names.
	installedPackagesParser := gofrogcmd.CmdOutputPattern{
		RegExp: alreadySatisfiedRegexp,
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 < 0 {
				log.Debug(fmt.Sprintf("Failed extracting package name from line: %s", pattern.Line))
				return pattern.Line, nil
			}

			// Save dependency with empty file name.
			downloadedDependencies[strings.ToLower(pattern.MatchedResults[1])] = ""

			log.Debug(fmt.Sprintf("Found package: %s already installed", pattern.MatchedResults[1]))
			return pattern.Line, nil
		},
	}

	// Execute command.
	var errorOut string
	_, errorOut, _, err = gofrogcmd.RunCmdWithOutputParser(pipCmd, true, &dependencyNameParser, &downloadedFileParser, &cachedFileParser, &installedPackagesParser)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed running %s command with error: '%s - %s'", string(pm.tool), err.Error(), errorOut))
	}

	return downloadedDependencies, nil
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
