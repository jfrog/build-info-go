package pythonutils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jfrog/build-info-go/utils"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Execute virtualenv command: "virtualenv venvdir" / "python3 -m venv venvdir"
func RunVirtualEnv() (venvPath string, err error) {
	var cmdArgs []string
	execPath, err := exec.LookPath("virtualenv")
	if err != nil || execPath == "" {
		// If virtualenv not installed try "venv"
		if runtime.GOOS == "windows" {
			// If the OS is Windows try using Py Launcher: "py -3 -m venv"
			execPath, err = exec.LookPath("py")
			cmdArgs = append(cmdArgs, "-3", "-m", "venv")
		} else {
			// If the OS is Linux try using python3 executable: "python3 -m venv"
			execPath, err = exec.LookPath("python3")
			cmdArgs = append(cmdArgs, "-m", "venv")
		}
		if err != nil {
			return "", err
		}
		if execPath == "" {
			return "", errors.New("Could not find python3 or virtualenv executable in PATH")
		}
	}
	cmdArgs = append(cmdArgs, "venvdir")
	var stderr bytes.Buffer
	pipVenv := exec.Command(execPath, cmdArgs...)
	pipVenv.Stderr = &stderr
	err = pipVenv.Run()
	if err != nil {
		return "", errors.New(fmt.Sprintf("pipenv install command failed: %s - %s", err.Error(), stderr.String()))
	}
	binDir := "bin"
	if runtime.GOOS == "windows" {
		binDir = "Scripts"
	}
	return filepath.Join("venvdir", binDir), nil
}

// Executes the pip-dependency-map script and returns a dependency map of all the installed pip packages in the current environment to and another list of the top level dependencies
func getPipDependencies(pythonExecPath, dependenciesDirName string) (map[string][]string, []string, error) {
	pipDependencyMapScriptPath, err := GetDepTreeScriptPath(dependenciesDirName)
	if err != nil {
		return nil, nil, err
	}
	data, err := utils.RunCommandWithOutput(pythonExecPath, []string{pipDependencyMapScriptPath, "--json"})
	if err != nil {
		return nil, nil, err
	}
	// Parse into array.
	packages := make([]pythonDependencyPackage, 0)
	if err = json.Unmarshal(data, &packages); err != nil {
		return nil, nil, err
	}

	return parseDependenciesToGraph(packages)
}

// Return path to the dependency-tree script, if not exists it creates the file.
func GetDepTreeScriptPath(dependenciesDirName string) (string, error) {
	if dependenciesDirName == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dependenciesDirName = filepath.Join(home, dependenciesDirName, "pip")
	}
	depTreeScriptName := "pipdeptree.py"
	pipDependenciesPath := filepath.Join(dependenciesDirName, "pip", pipDepTreeVersion)
	depTreeScriptPath := filepath.Join(pipDependenciesPath, depTreeScriptName)
	err := writeScriptIfNeeded(pipDependenciesPath, depTreeScriptName)
	if err != nil {
		return "", err
	}
	return depTreeScriptPath, err
}

// Creates local python script on jfrog dependencies path folder if such not exists
func writeScriptIfNeeded(targetDirPath, scriptName string) error {
	scriptPath := filepath.Join(targetDirPath, scriptName)
	exists, err := utils.IsFileExists(scriptPath, false)
	if err != nil {
		return err
	}
	if !exists {
		err = os.MkdirAll(targetDirPath, os.ModeDir|os.ModePerm)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(scriptPath, pipDepTreeContent, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}

func GetPackageNameFromSetuppy(pythonExecutablePath string) (string, error) {
	filePath, err := getSetupPyFilePath()
	if err != nil || filePath == "" {
		// Error was returned or setup.py does not exist in directory.
		return "", err
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
