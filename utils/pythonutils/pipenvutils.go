package pythonutils

import (
	"encoding/json"
	"fmt"
	"strings"

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
		err = fmt.Errorf("failed to run pipenv graph --json: %s", err.Error())
		return
	}
	// Parse into array.
	packages := make([]pythonDependencyPackage, 0)
	// Sometimes, `pipenv graph --json` command returns output with new line characters in between (not valid json) So, we need to remove them before unmarshalling.
	err = json.Unmarshal([]byte(strings.ReplaceAll(string(output), "\n", "")), &packages)
	if err != nil {
		err = fmt.Errorf("failed to parse pipenv graph --json output: %s", err.Error())
		return
	}
	dependenciesGraph, topLevelDependencies, err = parseDependenciesToGraph(packages)
	return
}
