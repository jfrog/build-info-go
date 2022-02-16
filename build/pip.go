package build

import (
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/build-info-go/utils/pythonutils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type PipModule struct {
	containingBuild     *Build
	name                string
	srcPath             string
	dependencyLocalPath string
	// GetDependenciesChecksumsFunc
	UpdateDepsChecksumInfoFunc func(dependenciesMap map[string]entities.Dependency) error
}

func newPipModule(srcPath string, containingBuild *Build) (*PipModule, error) {
	var err error
	if srcPath == "" {
		srcPath, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dependencyLocalPath := filepath.Join(home, dependenciesDirName, "pip")
	return &PipModule{srcPath: srcPath, containingBuild: containingBuild, dependencyLocalPath: dependencyLocalPath}, nil
}

func (pm *PipModule) RunCommandAndCollectDependencies(commandArgs []string) error {
	if commandArgs[0] == "install" {
		downloadedDependencies, err := pm.InstallWithLogParsing(commandArgs)
		if err != nil {
			return err
		}

		dependenciesMap := make(map[string]entities.Dependency, len(downloadedDependencies))
		for depName, fileName := range downloadedDependencies {
			dependenciesMap[depName] = entities.Dependency{Id: fileName}
		}
		if pm.UpdateDepsChecksumInfoFunc != nil {
			err = pm.UpdateDepsChecksumInfoFunc(dependenciesMap)
			if err != nil {
				return err
			}
		}

		pythonExecPath, err := utils.GetExecutablePath("python3")
		if err != nil {
			return err
		}

		// Get package-name.
		packageName, pkgNameErr := pythonutils.GetPackageNameFromSetuppy(pythonExecPath)
		if pkgNameErr != nil {
			pm.containingBuild.logger.Debug("Couldn't retrieve the package name from Setup.py. Reason: ", pkgNameErr.Error())
		}
		// If module-name was set by the command, don't change it.
		if pm.name == "" {
			// If the package name is unknown, set the module name to be the build name.
			if packageName == "" {
				pm.name = pm.containingBuild.buildName
				pm.containingBuild.logger.Debug(fmt.Sprintf("Using build name: %s as module name.", pm.name))
			} else {
				pm.name = packageName
			}
		}
		err = pythonutils.UpdateDepsIdsAndRequestedBy(dependenciesMap, packageName, pm.name, pm.dependencyLocalPath, pythonExecPath)
		if err != nil {
			return err
		}
		buildInfoModule := entities.Module{Id: pm.name, Type: entities.Python, Dependencies: dependenciesMapToList(dependenciesMap)}
		buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}

		return pm.containingBuild.SaveBuildInfo(buildInfo)
	}
	return nil
}

// Run pip-install command while parsing the logs for downloaded packages.
// Supports running pip either in non-verbose and verbose mode.
// Populates 'dependencyToFileMap' with downloaded package-name and its actual downloaded file (wheel/egg/zip...).
func (pm *PipModule) InstallWithLogParsing(commandArgs []string) (map[string]string, error) {
	log := pm.containingBuild.logger

	pipCmd, err := utils.NewCmd("pip", commandArgs)
	if err != nil {
		return nil, err
	}

	// Create regular expressions for log parsing.
	collectingPackageRegexp, err := regexp.Compile(`^Collecting\s(\w[\w-.]+)`)
	if err != nil {
		return nil, err
	}
	downloadFileRegexp, err := regexp.Compile(`^\s*Downloading\s([^\s]*)\s\(`)
	if err != nil {
		return nil, err
	}
	installedPackagesRegexp, err := regexp.Compile(`^Requirement\salready\ssatisfied:\s(\w[\w-.]+)`)
	if err != nil {
		return nil, err
	}

	downloadedDependencies := make(map[string]string)
	var packageName string
	expectingPackageFilePath := false

	// Extract downloaded package name.
	dependencyNameParser := gofrogcmd.CmdOutputPattern{
		RegExp: collectingPackageRegexp,
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
	dependencyFileParser := gofrogcmd.CmdOutputPattern{
		RegExp: downloadFileRegexp,
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
			downloadedDependencies[strings.ToLower(packageName)] = filePath
			expectingPackageFilePath = false

			log.Debug(fmt.Sprintf("Found package: %s installed with: %s", packageName, filePath))
			return pattern.Line, nil
		},
	}

	// Extract already installed packages names.
	installedPackagesParser := gofrogcmd.CmdOutputPattern{
		RegExp: installedPackagesRegexp,
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
	_, _, _, err = gofrogcmd.RunCmdWithOutputParser(pipCmd, true, &dependencyNameParser, &dependencyFileParser, &installedPackagesParser)
	if err != nil {
		return nil, err
	}

	return downloadedDependencies, nil
}

func (pm *PipModule) SetName(name string) {
	pm.name = name
}
