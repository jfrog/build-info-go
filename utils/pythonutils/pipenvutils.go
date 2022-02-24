package pythonutils

import (
	"encoding/json"
	"github.com/jfrog/build-info-go/utils"
)

// Executes pipenv graph.
// Returns a dependency map of all the installed pip packages in the current environment to and another list of the top level dependencies
// 'dependenciesGraph' - map between all parent modules and their child dependencies
// 'topLevelPackagesList' - list of all top level dependencies ( root dependencies only)
func getPipenvDependencies(srcPath string) (dependenciesGraph map[string][]string, topLevelDependencies []string, err error) {
	// Run pipenv graph
	pipenvGraphCmd := utils.NewCommand("pipenv", "graph", []string{"--json"})
	pipenvGraphCmd.Dir = srcPath
	output, err := pipenvGraphCmd.RunWithOutput()
	if err != nil {
		return
	}
	// Parse into array.
	packages := make([]pythonDependencyPackage, 0)
	err = json.Unmarshal(output, &packages)
	if err != nil {
		return
	}
	dependenciesGraph, topLevelDependencies, err = parseDependenciesToGraph(packages)
	return
}
