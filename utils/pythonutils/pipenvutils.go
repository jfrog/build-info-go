package pythonutils

import (
	"encoding/json"
	"github.com/jfrog/build-info-go/utils"
)

// Executes pipenv graph.
// Returns a dependency map of all the installed pip packages in the current environment to and another list of the top level dependencies
// 'dependenciesGraph' - map between all parent modules and their child dependencies
// 'topLevelPackagesList' - list of all top level dependencies ( root dependencies only)
func getPipenvDependencies() (dependenciesGraph map[string][]string, topLevelDependencies []string, err error) {
	// Run pipenv graph
	packages, err := runPipenvGraph()
	if err != nil {
		return nil, nil, err
	}
	return parseDependenciesToGraph(packages)
}

// Executes pipenv graph
// Returns a dependency map of all the installed pipenv packages and another list of the top level dependencies
func runPipenvGraph() ([]pythonDependencyPackage, error) {
	output, err := utils.RunCommandWithOutput("pipenv", []string{"graph", "--json"})
	if err != nil {
		return nil, err
	}
	// Parse into array.
	packages := make([]pythonDependencyPackage, 0)
	if err := json.Unmarshal(output, &packages); err != nil {
		return nil, err
	}
	return packages, nil
}
