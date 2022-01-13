package pythonutils

import (
	"fmt"
	"github.com/jfrog/build-info-go/utils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"regexp"
	"strings"
)

type PipCommand struct {
	*utils.Cmd
}

func NewPipCommand() (*PipCommand, error) {
	pipCmd, err := utils.NewCmd("pip")
	if err != nil {
		return nil, err
	}
	return &PipCommand{Cmd: pipCmd}, nil
}

// Run pip-install command while parsing the logs for downloaded packages.
// Supports running pip either in non-verbose and verbose mode.
// Populates 'dependencyToFileMap' with downloaded package-name and its actual downloaded file (wheel/egg/zip...).
func (pec *PipenvCommand) PipInstallWithLogParsing(log utils.Log) (map[string]string, error) {
	// Create regular expressions for log parsing.
	collectingPackageRegexp, err := regexp.Compile(`^Collecting\s(\w[\w-\.]+)`)
	if err != nil {
		return nil, err
	}
	downloadFileRegexp, err := regexp.Compile(`^\s\sDownloading\s[^\s]*\/([^\s]*)`)
	if err != nil {
		return nil, err
	}
	installedPackagesRegexp, err := regexp.Compile(`^Requirement\salready\ssatisfied\:\s(\w[\w-\.]+)`)
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
	_, _, _, err = gofrogcmd.RunCmdWithOutputParser(pec.Cmd, true, &dependencyNameParser, &dependencyFileParser, &installedPackagesParser)
	if err != nil {
		return nil, err
	}

	return downloadedDependencies, nil
}
