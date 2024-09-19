package pythonutils

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/io"
	gofrogcmd "github.com/jfrog/gofrog/io"
)

const (
	Pip    PythonTool = "pip"
	Pipenv PythonTool = "pipenv"
	Poetry PythonTool = "poetry"
	Twine  PythonTool = "twine"

	startDownloadingPattern = `^\s*Downloading\s`
	downloadingCaptureGroup = `[^\s]*`
	startUsingCachedPattern = `^\s*Using\scached\s`
	usingCacheCaptureGroup  = `[\S]+`
	endPattern              = `\s\(`
	packageNameRegexp       = `(\w[\w-.]+)`
)

type PythonTool string

var (
	credentialsInUrlRegexp = regexp.MustCompile(utils.CredentialsInUrlRegexp)
	catchAllRegexp         = regexp.MustCompile(".*")
)

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

func GetPythonDependenciesFiles(tool PythonTool, args []string, buildName, buildNumber string, log utils.Log, srcPath string) (map[string]entities.Dependency, error) {
	switch tool {
	case Pip, Pipenv:
		return InstallWithLogParsing(tool, args, log, srcPath)
	case Poetry:
		if buildName != "" && buildNumber != "" {
			log.Warn("Poetry commands are not supporting collecting dependencies files")
		}
		return make(map[string]entities.Dependency), nil
	default:
		return nil, errors.New(string(tool) + " commands are not supported.")
	}
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
	case Pip, Pipenv, Twine:
		return getPipProjectId(srcPath)
	case Poetry:
		packageName, _, err = getPoetryPackageFromPyProject(srcPath)
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
func UpdateDepsIdsAndRequestedBy(dependenciesMap map[string]entities.Dependency, dependenciesGraph map[string][]string, topLevelPackagesList []string, packageName, moduleName string) {
	if packageName == "" {
		// Projects without setup.py
		dependenciesGraph[moduleName] = topLevelPackagesList
	} else if packageName != moduleName {
		// Projects with setup.py
		dependenciesGraph[moduleName] = dependenciesGraph[packageName]
	}
	rootModule := entities.Dependency{Id: moduleName, RequestedBy: [][]string{{}}}
	updateDepsIdsAndRequestedBy(rootModule, dependenciesMap, dependenciesGraph)
}

func updateDepsIdsAndRequestedBy(parentDependency entities.Dependency, dependenciesMap map[string]entities.Dependency, dependenciesGraph map[string][]string) {
	for _, childId := range dependenciesGraph[parentDependency.Id] {
		childName := childId[0:strings.Index(childId, ":")]
		if childDep, ok := dependenciesMap[childName]; ok {
			if childDep.NodeHasLoop() || len(childDep.RequestedBy) >= entities.RequestedByMaxLength {
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
			// Convert ID field from filename to dependency id
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

// Create the CmdOutputPattern objects that can capture group content that may span multiple lines for logs that have line size limitations.
// Since the log parser parse line by line, we need to create a parser that can capture group content that may span multiple lines.
func getMultilineSplitCaptureOutputPattern(startCollectingPattern, captureGroup, endCollectingPattern string, handler func(pattern *gofrogcmd.CmdOutputPattern) (string, error)) (parsers []*gofrogcmd.CmdOutputPattern) {
	// Prepare regex patterns.
	oneLineRegex := regexp.MustCompile(startCollectingPattern + `(` + captureGroup + `)` + endCollectingPattern)
	startCollectionRegexp := regexp.MustCompile(startCollectingPattern)
	endCollectionRegexp := regexp.MustCompile(endCollectingPattern)

	// Create a parser for single line pattern matches.
	parsers = append(parsers, &gofrogcmd.CmdOutputPattern{RegExp: oneLineRegex, ExecFunc: handler})

	// Create a parser for multi line pattern matches.
	lineBuffer := ""
	collectingMultiLineValue := false
	parsers = append(parsers, &gofrogcmd.CmdOutputPattern{RegExp: catchAllRegexp, ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
		// Check if the line matches the startCollectingPattern.
		if !collectingMultiLineValue && startCollectionRegexp.MatchString(pattern.Line) {
			// Start collecting lines.
			collectingMultiLineValue = true
			lineBuffer = pattern.Line
			// We assume that the content is multiline so no need to check end at this point.
			// Single line will be handled and matched by the other parser.
			return pattern.Line, nil
		}
		if !collectingMultiLineValue {
			return pattern.Line, nil
		}
		// Add the line content to the buffer.
		lineBuffer += pattern.Line
		// Check if the line matches the endCollectingPattern.
		if endCollectionRegexp.MatchString(pattern.Line) {
			collectingMultiLineValue = false
			// Simulate a one line content check to make sure we have regex match.
			if oneLineRegex.MatchString(lineBuffer) {
				return handler(&gofrogcmd.CmdOutputPattern{Line: pattern.Line, MatchedResults: oneLineRegex.FindStringSubmatch(lineBuffer)})
			}
		}

		return pattern.Line, nil
	}})

	return
}

// Mask the pre-known credentials that are provided as command arguments from logs.
// This function creates a log parser for each credentials' argument.
func maskPreKnownCredentials(args []string) (parsers []*gofrogcmd.CmdOutputPattern) {
	for _, arg := range args {
		// If this argument is a credentials argument, create a log parser that masks it.
		if credentialsInUrlRegexp.MatchString(arg) {
			parsers = append(parsers, maskCredentialsArgument(arg, credentialsInUrlRegexp)...)
		}
	}
	return
}

// Creates a log parser that masks a pre-known credentials argument from logs.
// Support both multiline (using the line buffer) and single line credentials.
func maskCredentialsArgument(credentialsArgument string, credentialsRegex *regexp.Regexp) (parsers []*gofrogcmd.CmdOutputPattern) {
	lineBuffer := ""
	parsers = append(parsers, &gofrogcmd.CmdOutputPattern{RegExp: catchAllRegexp, ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
		return handlePotentialCredentialsInLogLine(pattern.Line, credentialsArgument, &lineBuffer, credentialsRegex)
	}})

	return
}

func handlePotentialCredentialsInLogLine(patternLine, credentialsArgument string, lineBuffer *string, credentialsRegex *regexp.Regexp) (string, error) {
	patternLine = strings.TrimSpace(patternLine)
	if patternLine == "" {
		return patternLine, nil
	}

	*lineBuffer += patternLine
	// If the accumulated line buffer is not a prefix of the credentials argument, reset the buffer and return the line unchanged.
	if !strings.HasPrefix(credentialsArgument, *lineBuffer) {
		*lineBuffer = ""
		return patternLine, nil
	}

	// When the whole credential was found (aggregated multiline or single line), return it filtered.
	if credentialsRegex.MatchString(*lineBuffer) {
		filteredLine, err := utils.RemoveCredentials(&gofrogcmd.CmdOutputPattern{Line: *lineBuffer, MatchedResults: credentialsRegex.FindStringSubmatch(*lineBuffer)})
		*lineBuffer = ""
		return filteredLine, err
	}

	// Avoid logging parts of the credentials till they are fully found.
	return "", nil
}

func InstallWithLogParsing(tool PythonTool, commandArgs []string, log utils.Log, srcPath string) (map[string]entities.Dependency, error) {
	if tool == Pipenv {
		// Add verbosity flag to pipenv commands to collect necessary data
		commandArgs = append(commandArgs, "-v")
	}
	installCmd := io.NewCommand(string(tool), "install", commandArgs)
	installCmd.Dir = srcPath

	dependenciesMap := map[string]entities.Dependency{}
	var parsers []*gofrogcmd.CmdOutputPattern

	var packageName string
	expectingPackageFilePath := false

	// Extract downloaded package name.
	parsers = append(parsers, &gofrogcmd.CmdOutputPattern{
		RegExp: regexp.MustCompile(`^Collecting\s` + packageNameRegexp),
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// If this pattern matched a second time before downloaded-file-name was found, prompt a message.
			if expectingPackageFilePath {
				// This may occur when a package-installation file is saved in pip-cache-dir, thus not being downloaded during the installation.
				// Re-running pip-install with 'no-cache-dir' fixes this issue.
				log.Debug(fmt.Sprintf("Could not resolve download path for package: %s, continuing...", packageName))

				// Save package with empty file path.
				dependenciesMap[strings.ToLower(packageName)] = entities.Dependency{Id: ""}
			}

			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 <= 0 {
				log.Debug(fmt.Sprintf("Failed extracting package name from line: %s", pattern.Line))
				return pattern.Line, nil
			}

			// Save dependency information.
			expectingPackageFilePath = true
			packageName = pattern.MatchedResults[1]

			return pattern.Line, nil
		},
	})

	saveCaptureGroupAsDependencyInfo := func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
		fileName := extractFileNameFromRegexCaptureGroup(pattern)
		if fileName == "" {
			log.Debug(fmt.Sprintf("Failed extracting download path from line: %s", pattern.Line))
			return pattern.Line, nil
		}
		// If this pattern matched before package-name was found, do not collect this path.
		if !expectingPackageFilePath {
			log.Debug(fmt.Sprintf("Could not resolve package name for download path: %s , continuing...", packageName))
			return pattern.Line, nil
		}
		// Save dependency information.
		dependenciesMap[strings.ToLower(packageName)] = entities.Dependency{Id: fileName}
		expectingPackageFilePath = false
		log.Debug(fmt.Sprintf("Found package: %s installed with: %s", packageName, fileName))
		return pattern.Line, nil
	}

	// Extract downloaded file, stored in Artifactory. (value at log may be split into multiple lines)
	parsers = append(parsers, getMultilineSplitCaptureOutputPattern(startDownloadingPattern, downloadingCaptureGroup, endPattern, saveCaptureGroupAsDependencyInfo)...)
	// Extract cached file, stored in Artifactory. (value at log may be split into multiple lines)
	parsers = append(parsers, getMultilineSplitCaptureOutputPattern(startUsingCachedPattern, usingCacheCaptureGroup, endPattern, saveCaptureGroupAsDependencyInfo)...)

	parsers = append(parsers, maskPreKnownCredentials(commandArgs)...)

	// Extract already installed packages names.
	parsers = append(parsers, &gofrogcmd.CmdOutputPattern{
		RegExp: regexp.MustCompile(`^Requirement\salready\ssatisfied:\s` + packageNameRegexp),
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 < 0 {
				log.Debug(fmt.Sprintf("Failed extracting package name from line: %s", pattern.Line))
				return pattern.Line, nil
			}

			// Save dependency with empty file name.
			dependenciesMap[strings.ToLower(pattern.MatchedResults[1])] = entities.Dependency{Id: ""}
			log.Debug(fmt.Sprintf("Found package: %s already installed", pattern.MatchedResults[1]))
			return pattern.Line, nil
		},
	})

	// Execute command.
	_, errorOut, _, err := gofrogcmd.RunCmdWithOutputParser(installCmd, true, parsers...)
	if err != nil {
		return nil, fmt.Errorf("failed running %s command with error: '%s - %s'", string(tool), err.Error(), errorOut)
	}
	return dependenciesMap, nil
}

func extractFileNameFromRegexCaptureGroup(pattern *gofrogcmd.CmdOutputPattern) (fileName string) {
	// Check for out of bound results (no captures).
	if len(pattern.MatchedResults) <= 1 {
		return ""
	}
	// Extract file information from capture group.
	filePath := pattern.MatchedResults[1]
	lastSlashIndex := strings.LastIndex(filePath, "/")
	if lastSlashIndex == -1 {
		return filePath
	}
	lastComponent := filePath[lastSlashIndex+1:]
	// Unescape the last component, for example 'PyYAML-5.1.2%2Bsp1.tar.gz' -> 'PyYAML-5.1.2+sp1.tar.gz'.
	unescapedComponent, _ := url.QueryUnescape(lastComponent)
	if unescapedComponent == "" {
		// Couldn't escape, will use the raw string
		return lastComponent
	}
	return unescapedComponent
}
