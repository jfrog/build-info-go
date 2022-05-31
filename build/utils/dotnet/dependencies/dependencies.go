package dependencies

import (
	"fmt"
	deptree "github.com/jfrog/build-info-go/build/utils/dotnet/dependenciestree"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const absentNupkgWarnMsg = " Skipping adding this dependency to the build info. This might be because the package already exists in a different NuGet cache," +
	" possibly the SDK's NuGetFallbackFolder cache. Removing the package from this cache may resolve the issue."

var extractors []Extractor

// Register dependency extractor
func register(dependencyType Extractor) {
	extractors = append(extractors, dependencyType)
}

// The extractor responsible to calculate the project dependencies.
type Extractor interface {
	// Check whether the extractor is compatible with the current dependency resolution method
	IsCompatible(projectName, dependenciesSource string, log utils.Log) bool
	// Get all the dependencies for the project
	AllDependencies(log utils.Log) (map[string]*buildinfo.Dependency, error)
	// Get all the root dependencies of the project
	DirectDependencies() ([]string, error)
	// Dependencies relations map
	ChildrenMap() (map[string][]string, error)

	new(dependenciesSource string, log utils.Log) (Extractor, error)
}

func CreateCompatibleExtractor(projectName, dependenciesSource string, log utils.Log) (Extractor, error) {
	extractor, err := getCompatibleExtractor(projectName, dependenciesSource, log)
	if err != nil {
		return nil, err
	}
	return extractor, nil
}

func CreateDependencyTree(extractor Extractor, log utils.Log) (deptree.Root, error) {
	rootDependencies, err := extractor.DirectDependencies()
	if err != nil {
		return nil, err
	}
	allDependencies, err := extractor.AllDependencies(log)
	if err != nil {
		return nil, err
	}
	childrenMap, err := extractor.ChildrenMap()
	if err != nil {
		return nil, err
	}
	return deptree.CreateDependencyTree(rootDependencies, allDependencies, childrenMap), nil
}

// Find suitable registered dependencies extractor.
func getCompatibleExtractor(projectName, dependenciesSource string, log utils.Log) (Extractor, error) {
	for _, extractor := range extractors {
		if extractor.IsCompatible(projectName, dependenciesSource, log) {
			return extractor.new(dependenciesSource, log)
		}
	}
	log.Debug(fmt.Sprintf("Unsupported project dependencies for project: %s", projectName))
	return nil, nil
}
