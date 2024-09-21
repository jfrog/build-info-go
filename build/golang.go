package build

import (
	"errors"
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/crypto"
	"path/filepath"
	"strings"
	"unicode"
)

type GoModule struct {
	containingBuild *Build
	name            string
	srcPath         string
}

func newGoModule(srcPath string, containingBuild *Build) (*GoModule, error) {
	var err error
	if srcPath == "" {
		srcPath, err = utils.GetProjectRoot()
		if err != nil {
			return nil, err
		}
	}

	// Read module name
	name, err := utils.GetModuleNameByDir(srcPath, containingBuild.logger)
	if err != nil {
		return nil, err
	}

	return &GoModule{name: name, srcPath: srcPath, containingBuild: containingBuild}, nil
}

func (gm *GoModule) CalcDependencies() error {
	if !gm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to collect the project's dependencies")
	}
	buildInfoDependencies, err := gm.loadDependencies()
	if err != nil {
		return err
	}

	buildInfoModule := entities.Module{Id: gm.name, Type: entities.Go, Dependencies: buildInfoDependencies}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}

	return gm.containingBuild.SaveBuildInfo(buildInfo)
}

func (gm *GoModule) SetName(name string) {
	gm.name = name
}

func (gm *GoModule) AddArtifacts(artifacts ...entities.Artifact) error {
	return gm.containingBuild.AddArtifacts(gm.name, entities.Go, artifacts...)
}

func (gm *GoModule) loadDependencies() ([]entities.Dependency, error) {
	cachePath, err := utils.GetCachePath()
	if err != nil {
		return nil, err
	}
	dependenciesGraph, err := utils.GetDependenciesGraph(gm.srcPath, gm.containingBuild.logger)
	if err != nil {
		return nil, err
	}
	dependenciesMap, err := gm.getGoDependencies(cachePath)
	if err != nil {
		return nil, err
	}
	emptyRequestedBy := [][]string{{}}
	populateRequestedByField(gm.name, emptyRequestedBy, dependenciesMap, dependenciesGraph)
	return dependenciesMapToList(dependenciesMap), nil
}

func (gm *GoModule) getGoDependencies(cachePath string) (map[string]entities.Dependency, error) {
	modulesMap, err := utils.GetDependenciesList(gm.srcPath, gm.containingBuild.logger, nil)
	if err != nil || len(modulesMap) == 0 {
		return nil, err
	}
	// Create a map from dependency to parents
	buildInfoDependencies := make(map[string]entities.Dependency)
	for moduleId := range modulesMap {
		// If the path includes capital letters, the Go convention is to use "!" before the letter. The letter itself is in lowercase.
		encodedDependencyId := goModEncode(moduleId)

		// We first check if this dependency has a zip in the local Go cache.
		// If it does not, nil is returned. This seems to be a bug in Go.
		zipPath, err := gm.getPackageZipLocation(cachePath, encodedDependencyId)
		if err != nil {
			return nil, err
		}
		if zipPath == "" {
			continue
		}
		zipDependency, err := populateZip(encodedDependencyId, zipPath)
		if err != nil {
			return nil, err
		}
		buildInfoDependencies[moduleId] = zipDependency
	}
	return buildInfoDependencies, nil
}

// Returns the actual path to the dependency.
// If the path includes capital letters, the Go convention is to use "!" before the letter.
// The letter itself is in lowercase.
func goModEncode(name string) string {
	path := ""
	for _, letter := range name {
		if unicode.IsUpper(letter) {
			path += "!" + strings.ToLower(string(letter))
		} else {
			path += string(letter)
		}
	}
	return path
}

// Returns the path to the package zip file if exists.
func (gm *GoModule) getPackageZipLocation(cachePath, encodedDependencyId string) (string, error) {
	zipPath, err := gm.getPackagePathIfExists(cachePath, encodedDependencyId)
	if err != nil {
		return "", err
	}

	if zipPath != "" {
		return zipPath, nil
	}

	return gm.getPackagePathIfExists(filepath.Dir(cachePath), encodedDependencyId)
}

// Validates that the package zip file exists and returns its path.
func (gm *GoModule) getPackagePathIfExists(cachePath, encodedDependencyId string) (zipPath string, err error) {
	moduleInfo := strings.Split(encodedDependencyId, ":")
	if len(moduleInfo) != 2 {
		gm.containingBuild.logger.Debug("The encoded dependency Id syntax should be 'name:version' but instead got:", encodedDependencyId)
		return "", nil
	}
	dependencyName := moduleInfo[0]
	version := moduleInfo[1]
	zipPath = filepath.Join(cachePath, dependencyName, "@v", version+".zip")
	fileExists, err := utils.IsFileExists(zipPath, true)
	if err != nil {
		return "", fmt.Errorf("could not find zip binary for dependency '%s' at %s: %s", dependencyName, zipPath, err)
	}
	// Zip binary does not exist, so we skip it by returning a nil dependency.
	if !fileExists {
		gm.containingBuild.logger.Debug("The following file is missing:", zipPath)
		return "", nil
	}
	return zipPath, nil
}

// populateZip adds the zip file as build-info dependency
func populateZip(packageId, zipPath string) (zipDependency entities.Dependency, err error) {
	// Zip file dependency for the build-info
	zipDependency = entities.Dependency{Id: packageId}
	checksums, err := crypto.GetFileChecksums(zipPath)
	if err != nil {
		return
	}
	zipDependency.Type = "zip"
	zipDependency.Checksum = entities.Checksum{Sha1: checksums[crypto.SHA1], Md5: checksums[crypto.MD5], Sha256: checksums[crypto.SHA256]}
	return
}

func populateRequestedByField(parentId string, parentRequestedBy [][]string, dependenciesMap map[string]entities.Dependency, dependenciesGraph map[string][]string) {
	for _, childName := range dependenciesGraph[parentId] {
		if childDep, ok := dependenciesMap[childName]; ok {
			if childDep.NodeHasLoop() || len(childDep.RequestedBy) >= entities.RequestedByMaxLength {
				continue
			}
			// Update RequestedBy field from parent's RequestedBy.
			childDep.UpdateRequestedBy(parentId, parentRequestedBy)
			// Reassign map entry with new entry copy
			dependenciesMap[childName] = childDep
			// Run recursive call on child dependencies
			populateRequestedByField(childName, childDep.RequestedBy, dependenciesMap, dependenciesGraph)
		}
	}
}
