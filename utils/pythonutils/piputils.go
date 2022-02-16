package pythonutils

import (
	"encoding/json"
	"github.com/jfrog/build-info-go/utils"
	"io/ioutil"
	"os"
	"path/filepath"
)

// Executes the pip-dependency-map script and returns a dependency map of all the installed pip packages in the current environment to and another list of the top level dependencies
func RunPipDepTree(pythonExecPath, dependenciesDirName string) (map[string][]string, []string, error) {
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
