package build

import (
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"regexp"
	"strings"
)

type PipenvModule struct {
	containingBuild *Build
	name            string
	srcPath         string
}

func newPipenvModule(srcPath string, containingBuild *Build) (*PipenvModule, error) {
	var err error
	if srcPath == "" {
		srcPath, err = utils.GetProjectRoot()
		if err != nil {
			return nil, err
		}
	}

	// Read module name
	name, err := utils.GetModuleNameByDir(srcPath, containingBuild.logger)
	if err != nil {
		return nil, err
	}

	return &PipenvModule{name: name, srcPath: srcPath, containingBuild: containingBuild}, nil
}

func (pem *PipenvModule) RunCommandAndCollectDependencies(commandArgs []string) error {
	if commandArgs[0] == "install" {
		pipenvCmd, err := utils.NewCmd("pipenv", commandArgs)
		if err != nil {
			return err
		}
		// Run pip install command and parse logs to get a map of downloaded dependency files
		dependenciesList, err := pem.InstallWithLogParsing(pipenvCmd)
		if err != nil {
			return err
		}
		pythonExecutablePath, err := getExecutablePath("python")
		if err != nil {
			return err
		}
		dependenciesGraph, topLevelPackagesList, err := piputils.RunPipDepTree(pythonExecutablePath)
		if err != nil {
			return err
		}
		if err := pic.collectBuildInfo(projectsDirPath, pythonExecutablePath, dependenciesList, dependenciesGraph, topLevelPackagesList); err != nil {
			return err
		}
	}
	//buildInfoDependencies, err := pm.loadDependencies()
	//if err != nil {
	//	return err
	//}
	//
	//buildInfoModule := entities.Module{Id: pm.name, Type: entities.Go, Dependencies: buildInfoDependencies}
	//buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}
	//
	//return pm.containingBuild.SaveBuildInfo(buildInfo)
	return nil
}

// Run pip-install command while parsing the logs for downloaded packages.
// Supports running pip either in non-verbose and verbose mode.
// Populates 'dependencyToFileMap' with downloaded package-name and its actual downloaded file (wheel/egg/zip...).
func (pem *PipenvModule) InstallWithLogParsing(cmd *utils.Cmd) (map[string]*entities.Dependency, error) {
	log := pem.containingBuild.logger
	// Create regular expressions for log parsing.
	collectingPackageRegexp, err := regexp.Compile(`^Collecting\s(\w[\w-\.]+)`)
	if err != nil {
		return nil, err
	}
	downloadFileRegexp, err := regexp.Compile(`^\s\sDownloading\s(\S*)\s\(`)
	if err != nil {
		return nil, err
	}
	installedPackagesRegexp, err := regexp.Compile(`^\s\sUsing\scached\s([\S]+)\s\(`)
	if err != nil {
		return nil, err
	}

	downloadedDependencies := make(map[string]string)
	expectingPackageFilePath := false
	packageName := ""
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

			// Save dependency information
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

	// Extract already installed packages names.
	installedPackagesParser := gofrogcmd.CmdOutputPattern{
		RegExp: installedPackagesRegexp,
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 < 0 {
				log.Debug(fmt.Sprintf("Failed extracting package name from line: %s", pattern.Line))
				return pattern.Line, nil
			}

			filePath := pattern.MatchedResults[1]
			lastSlashIndex := strings.LastIndex(filePath, "/")
			var fileName string
			if lastSlashIndex == -1 {
				fileName = filePath
			} else {
				fileName = filePath[lastSlashIndex+1:]
			}
			// Save dependency with empty file name.
			downloadedDependencies[strings.ToLower(packageName)] = fileName
			expectingPackageFilePath = false
			log.Debug(fmt.Sprintf("Found package: %s already installed", fileName))
			return pattern.Line, nil
		},
	}

	// Execute command.
	_, _, _, err = gofrogcmd.RunCmdWithOutputParser(cmd, true, &dependencyNameParser, &dependencyFileParser, &installedPackagesParser)
	if err != nil {
		return nil, err
	}

	dependenciesMap := make(map[string]*entities.Dependency, len(downloadedDependencies))
	for depId := range allDepsList {
		depName := depId[0:strings.Index(depId, ":")]
		dependenciesMap[depName] = &entities.Dependency{Id: downloadedDependencies[depName]}
	}
	return dependenciesMap, nil
}

func (pm *PipModule) SetName(name string) {
	pm.name = name
}
