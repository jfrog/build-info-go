package build

import (
	"errors"
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
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
	partial := &entities.Partial{ModuleId: gm.name, ModuleType: entities.Go, Artifacts: artifacts}
	return gm.containingBuild.SavePartialBuildInfo(partial)
}

func (gm *GoModule) loadDependencies() ([]entities.Dependency, error) {
	cachePath, err := utils.GetCachePath()
	if err != nil {
		return nil, err
	}
	modulesMap, err := utils.GetDependenciesList(gm.srcPath, gm.containingBuild.logger)
	if err != nil || len(modulesMap) == 0 {
		return nil, err
	}
	return gm.getGoDependencies(cachePath, modulesMap)
}

func (gm *GoModule) getGoDependencies(cachePath string, moduleSlice map[string]bool) ([]entities.Dependency, error) {
	var buildInfoDependencies []entities.Dependency
	for module := range moduleSlice {
		moduleInfo := strings.Split(module, "@")
		name := goModEncode(moduleInfo[0])
		version := goModEncode(moduleInfo[1])
		packageId := strings.Join([]string{name, version}, ":")

		// We first check if this dependency has a zip in the local Go cache.
		// If it does not, nil is returned. This seems to be a bug in Go.
		zipPath, err := gm.getPackageZipLocation(cachePath, name, version)
		if err != nil {
			return nil, err
		}
		if zipPath == "" {
			continue
		}
		zipDependency, err := populateZip(packageId, zipPath)
		if err != nil {
			return nil, err
		}
		buildInfoDependencies = append(buildInfoDependencies, *zipDependency)
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
func (gm *GoModule) getPackageZipLocation(cachePath, dependencyName, version string) (string, error) {
	zipPath, err := gm.getPackagePathIfExists(cachePath, dependencyName, version)
	if err != nil {
		return "", err
	}

	if zipPath != "" {
		return zipPath, nil
	}

	return gm.getPackagePathIfExists(filepath.Dir(cachePath), dependencyName, version)
}

// Validates that the package zip file exists and returns its path.
func (gm *GoModule) getPackagePathIfExists(cachePath, dependencyName, version string) (zipPath string, err error) {
	zipPath = filepath.Join(cachePath, dependencyName, "@v", version+".zip")
	fileExists, err := utils.IsFileExists(zipPath)
	if err != nil {
		return "", errors.New(fmt.Sprintf("Could not find zip binary for dependency '%s' at %s: %s", dependencyName, zipPath, err))
	}
	// Zip binary does not exist, so we skip it by returning a nil dependency.
	if !fileExists {
		gm.containingBuild.logger.Debug("The following file is missing:", zipPath)
		return "", nil
	}
	return zipPath, nil
}

// populateZip adds the zip file as build-info dependency
func populateZip(packageId, zipPath string) (zipDependency *entities.Dependency, err error) {
	// Zip file dependency for the build-info
	zipDependency = &entities.Dependency{Id: packageId}
	checksums, err := utils.GetFileChecksums(zipPath)
	if err != nil {
		return nil, err
	}
	zipDependency.Type = "zip"
	zipDependency.Checksum = &entities.Checksum{Sha1: checksums.Sha1, Md5: checksums.Md5}
	return
}
