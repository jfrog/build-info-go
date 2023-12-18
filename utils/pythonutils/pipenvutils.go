package pythonutils

import (
	"encoding/json"

	"github.com/jfrog/gofrog/io"
)

// Executes pipenv graph.
// Returns a dependency map of all the installed pipenv packages in the current environment and also another list of the top level dependencies
// 'dependenciesGraph' - map between all parent modules and their child dependencies
// 'topLevelPackagesList' - list of all top level dependencies ( root dependencies only)
func getPipenvDependencies(srcPath string) (dependenciesGraph map[string][]string, topLevelDependencies []string, err error) {
	// Run pipenv graph
	pipenvGraphCmd := io.NewCommand("pipenv", "graph", []string{"--json"})
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
