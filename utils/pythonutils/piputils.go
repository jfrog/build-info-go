package pythonutils

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/io"
)

// Executes the pip-dependency-map script and returns a dependency map of all the installed pip packages in the current environment to and another list of the top level dependencies
func getPipDependencies(srcPath, dependenciesDirName string) (map[string][]string, []string, error) {
	localPipdeptreeScript, err := getDepTreeScriptPath(dependenciesDirName)
	if err != nil {
		return nil, nil, err
	}
	localPipdeptree := io.NewCommand("python", "", []string{localPipdeptreeScript, "--json"})
	localPipdeptree.Dir = srcPath
	output, err := localPipdeptree.RunWithOutput()
	if err != nil {
		return nil, nil, err
	}
	// Parse into array.
	packages := make([]pythonDependencyPackage, 0)
	if err = json.Unmarshal(output, &packages); err != nil {
		return nil, nil, err
	}

	return parseDependenciesToGraph(packages)
}

// Return path to the dependency-tree script, If it doesn't exist, it creates the file.
func getDepTreeScriptPath(dependenciesDirName string) (string, error) {
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
		err = os.WriteFile(scriptPath, pipDepTreeContent, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}

func getPackageNameFromSetuppy(srcPath string) (string, error) {
	filePath, err := getSetupPyFilePath(srcPath)
	if err != nil || filePath == "" {
		// Error was returned or setup.py does not exist in directory.
		return "", err
	}

	// Extract package name from setup.py.
	packageName, err := ExtractPackageNameFromSetupPy(filePath)
	if err != nil {
		// If setup.py egg_info command failed we use build name as module name and continue to pip-install execution
		return "", errors.New("couldn't determine module-name after running the 'egg_info' command: " + err.Error())
	}
	return packageName, nil
}

// Look for 'setup.py' file in current work dir.
// If found, return its absolute path.
func getSetupPyFilePath(srcPath string) (string, error) {
	return getFilePath(srcPath, "setup.py")
}

// Get the project-name by running 'egg_info' command on setup.py and extracting it from 'PKG-INFO' file.
func ExtractPackageNameFromSetupPy(setuppyFilePath string) (string, error) {
	// Execute egg_info command and return PKG-INFO content.
	content, err := getEgginfoPkginfoContent(setuppyFilePath)
	if err != nil {
		return "", err
	}

	// Extract project name from file content.
	return getProjectIdFromFileContent(content)
}

// Run egg-info command on setup.py. The command generates metadata files.
// Return the content of the 'PKG-INFO' file.
func getEgginfoPkginfoContent(setuppyFilePath string) (output []byte, err error) {
	eggBase, err := utils.CreateTempDir()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, utils.RemoveTempDir(eggBase))
	}()

	// Run python 'egg_info --egg-base <eggBase>' command.
	var args []string
	pythonExecutable, windowsPyArg := GetPython3Executable()
	if windowsPyArg != "" {
		args = append(args, windowsPyArg)
	}
	args = append(args, setuppyFilePath, "egg_info", "--egg-base", eggBase)
	if err != nil {
		return nil, err
	}
	if err = exec.Command(pythonExecutable, args...).Run(); err != nil {
		return nil, err
	}

	// Read PKG_INFO under <eggBase>/*.egg-info/PKG-INFO.
	return extractPackageNameFromEggBase(eggBase)
}

func GetPython3Executable() (string, string) {
	windowsPyArg := ""
	pythonExecutable, _ := exec.LookPath("python3")
	if pythonExecutable == "" {
		if utils.IsWindows() {
			// If the OS is Windows try using Py Launcher: 'py -3'
			pythonExecutable, _ = exec.LookPath("py")
			if pythonExecutable != "" {
				windowsPyArg = "-3"
			}
		}
		// Try using 'python' if 'python3'/'py' couldn't be found
		if pythonExecutable == "" {
			pythonExecutable = "python"
		}
	}
	return pythonExecutable, windowsPyArg
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
				return nil, errors.New("file 'PKG-INFO' couldn't be found in its designated location: " + pkginfoPath)
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
	packageNameRegexp := regexp.MustCompile(`(?m)^Name:\s(\w[\w-.]+)`)

	// Find first nameMatch of packageNameRegexp.
	nameMatch := packageNameRegexp.FindStringSubmatch(string(content))
	if len(nameMatch) < 2 {
		return "", errors.New("failed extracting package name from content")
	}

	// Create package-version regexp.
	packageVersionRegexp := regexp.MustCompile(`(?m)^Version:\s(\w[\w-.]+)`)

	// Find first match of packageNameRegexp.
	versionMatch := packageVersionRegexp.FindStringSubmatch(string(content))
	if len(versionMatch) < 2 {
		return "", errors.New("failed extracting package version from content")
	}

	return nameMatch[1] + ":" + versionMatch[1], nil
}
