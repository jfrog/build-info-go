package pythonutils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"golang.org/x/exp/maps"
)

type PyprojectToml struct {
	Tool map[string]PoetryPackage
}
type PoetryPackage struct {
	Name            string
	Version         string
	Dependencies    map[string]interface{}
	DevDependencies map[string]interface{} `toml:"dev-dependencies"`
}

type PoetryLock struct {
	Package []*PoetryPackage
}

// Extract all poetry dependencies from the pyproject.toml and poetry.lock files.
// Returns a dependency map of all the installed poetry packages in the current environment and another list of the top level dependencies.
func getPoetryDependencies(srcPath string) (graph map[string][]string, directDependencies []string, err error) {
	filePath, err := getPoetryLockFilePath(srcPath)
	if err != nil || filePath == "" {
		// Error was returned or poetry.lock does not exist in directory.
		return map[string][]string{}, []string{}, err
	}
	projectName, directDependencies, err := getPackageNameFromPyproject(srcPath)
	if err != nil {
		return map[string][]string{}, []string{}, err
	}
	// Extract packages names from poetry.lock
	dependencies, dependenciesVersions, err := extractPackagesFromPoetryLock(filePath)
	if err != nil {
		return map[string][]string{}, []string{}, err
	}
	graph = make(map[string][]string)
	// Add the root node - the project itself.
	for _, directDependency := range directDependencies {
		directDependencyName := directDependency + ":" + dependenciesVersions[strings.ToLower(directDependency)]
		graph[projectName] = append(graph[projectName], directDependencyName)
	}
	// Add versions to all dependencies
	for dependency, transitiveDependencies := range dependencies {
		for _, transitiveDependency := range transitiveDependencies {
			transitiveDependencyName := transitiveDependency + ":" + dependenciesVersions[strings.ToLower(transitiveDependency)]
			graph[dependency] = append(graph[dependency], transitiveDependencyName)
		}
	}
	return graph, graph[projectName], nil
}

func getPackageNameFromPyproject(srcPath string) (string, []string, error) {
	filePath, err := getPyprojectFilePath(srcPath)
	if err != nil || filePath == "" {
		// Error was returned or pyproject.toml does not exist in directory.
		return "", []string{}, err
	}
	// Extract package name from pyproject.toml.
	project, err := extractProjectFromPyproject(filePath)
	if err != nil {
		return "", []string{}, err
	}
	return project.Name, append(maps.Keys(project.Dependencies), maps.Keys(project.DevDependencies)...), nil
}

// Look for 'pyproject.toml' file in current work dir.
// If found, return its absolute path.
func getPyprojectFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "pyproject.toml")
}

// Look for 'poetry.lock' file in current work dir.
// If found, return its absolute path.
func getPoetryLockFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "poetry.lock")
}

// Get the project-name by parsing the pyproject.toml file.
func extractProjectFromPyproject(pyprojectFilePath string) (project PoetryPackage, err error) {
	content, err := os.ReadFile(pyprojectFilePath)
	if err != nil {
		return
	}
	var pyprojectFile PyprojectToml
	_, err = toml.Decode(string(content), &pyprojectFile)
	if err != nil {
		return
	}
	if poetryProject, ok := pyprojectFile.Tool["poetry"]; ok {
		// Extract project name from file content.
		poetryProject.Name = poetryProject.Name + ":" + poetryProject.Version
		return poetryProject, nil
	}
	return PoetryPackage{}, errors.New("Couldn't find project name and version in " + pyprojectFilePath)
}

// Get the project-name by parsing the poetry.lock file
func extractPackagesFromPoetryLock(lockFilePath string) (dependencies map[string][]string, dependenciesVersions map[string]string, err error) {
	content, err := os.ReadFile(lockFilePath)
	if err != nil {
		return
	}
	var poetryLockFile PoetryLock

	_, err = toml.Decode(string(content), &poetryLockFile)
	if err != nil {
		return
	}
	dependenciesVersions = make(map[string]string)
	dependencies = make(map[string][]string)
	for _, dependency := range poetryLockFile.Package {
		dependenciesVersions[strings.ToLower(dependency.Name)] = dependency.Version
		dependencyName := dependency.Name + ":" + dependency.Version
		dependencies[dependencyName] = maps.Keys(dependency.Dependencies)
	}
	return
}

// Get the project dependencies files (.whl or .tar.gz) by searching the Python package site.
// extractPoetryDependenciesFiles returns a dictionary where the key is the dependency name and the value is a dependency file struct.
func extractPoetryDependenciesFiles(srcPath string, cmdArgs []string, log utils.Log) (dependenciesFiles map[string]entities.Dependency, err error) {
	// Run poetry install and extract the site-packages location
	sitePackagesPath, err := getSitePackagesPath(cmdArgs, srcPath)
	if err != nil {
		return
	}
	// Extract packages names from poetry.lock
	filePath, err := getPoetryLockFilePath(srcPath)
	if err != nil || filePath == "" {
		// Error was returned or poetry.lock does not exist in directory.
		return nil, err
	}
	_, dependenciesVersions, err := extractPackagesFromPoetryLock(filePath)
	if err != nil {
		return nil, err
	}
	dependenciesFiles = map[string]entities.Dependency{}
	for dependency, version := range dependenciesVersions {
		directUrlPath := fmt.Sprintf("%s%s-%s.dist-info%sdirect_url.json", sitePackagesPath, dependency, version, string(os.PathSeparator))
		directUrlFile, err := os.ReadFile(directUrlPath)
		if err != nil {
			log.Debug(fmt.Sprintf("Could not resolve download path for package: %s, error: %s \ncontinuing...", dependency, err))
			continue
		}
		directUrl := packagedDirectUrl{}
		err = json.Unmarshal(directUrlFile, &directUrl)
		if err != nil {
			log.Debug(fmt.Sprintf("Could not resolve download path for package: %s, error: %s \ncontinuing...", dependency, err))
			continue
		}
		lastSeparatorIndex := strings.LastIndex(directUrl.Url, string(os.PathSeparator))
		var fileName string
		if lastSeparatorIndex == -1 {
			fileName = directUrl.Url
		} else {
			fileName = directUrl.Url[lastSeparatorIndex+1:]
		}
		dependenciesFiles[strings.ToLower(dependency)] = entities.Dependency{Id: fileName}
		log.Debug(fmt.Sprintf("Found package: %s installed with: %s", dependency, fileName))
	}
	return
}
func getSitePackagesPath(commandArgs []string, srcPath string) (sitePackagesPath string, err error) {
	// First run poetry install with verbose logging
	commandArgs = append(commandArgs, "-vv")
	installCmd := utils.NewCommand("poetry", "install", commandArgs)
	installCmd.Dir = srcPath
	// Extract the virtuL env path
	virtualEnvRegexp := regexp.MustCompile(`^Using\svirtualenv:\s(.*)$`)
	virtualEnvNameParser := gofrogcmd.CmdOutputPattern{
		RegExp: virtualEnvRegexp,
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 < 0 {
				return "", nil
			}
			// If found, return the virtual env path
			return pattern.MatchedResults[1], nil
		},
	}
	virtualEnvPath, errorOut, _, err := gofrogcmd.RunCmdWithOutputParser(installCmd, true, &virtualEnvNameParser)
	if err != nil {
		return "", fmt.Errorf("failed running poetry command with error: '%s - %s'", err.Error(), errorOut)
	}
	if virtualEnvPath != "" {
		// Take the first line matches the virtualEnvRegexp
		sitePackagesPath = strings.Split(virtualEnvPath, "\n")[0]
		// Extract from poetry env(i.e PROJECT-9SrbZw5z-py3.9) the env python version
		pythonVersionIndex := strings.LastIndex(sitePackagesPath, "-py")
		if pythonVersionIndex == -1 {
			return "", fmt.Errorf("failed extracting python site package form the following virtual env %q", sitePackagesPath)
		}
		pythonVersion := sitePackagesPath[pythonVersionIndex+3:]
		// add /lib/python3.10/site-packages
		sitePackagesPath = filepath.Join(sitePackagesPath, "lib", "python"+pythonVersion, "site-packages") + string(os.PathSeparator)
	} else {
		// If no virtuL env is use, return the local python installation site-packages path
		siteCmd := utils.NewCommand("python", "site", []string{"-m", "--user-site"})
		sitePackagesPath, err = gofrogcmd.RunCmdOutput(siteCmd)
		if err != nil {
			return "", fmt.Errorf("failed running python -m site --user-site with error: '%s'", err.Error())
		}
	}
	return
}

type packagedDirectUrl struct {
	Url string
}
