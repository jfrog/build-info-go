package build

import (
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/jfrog-client-go/utils/io/fileutils"
	"github.com/jfrog/jfrog-client-go/utils/log"
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
	log.SetLogger(containingBuild.logger)

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
	if err != nil {
		return nil, err
	}
	if modulesMap == nil {
		return nil, nil
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
		zipPath, err := getPackageZipLocation(cachePath, name, version)
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
// If in the path there are capital letters, the Go convention is to use "!" before the letter.
// The letter itself in lowercase.
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
func getPackageZipLocation(cachePath, dependencyName, version string) (string, error) {
	zipPath, err := getPackagePathIfExists(cachePath, dependencyName, version)
	if err != nil {
		return "", err
	}

	if zipPath != "" {
		return zipPath, nil
	}

	zipPath, err = getPackagePathIfExists(filepath.Dir(cachePath), dependencyName, version)

	if err != nil {
		return "", err
	}

	return zipPath, nil
}

// Validates if the package zip file exists.
func getPackagePathIfExists(cachePath, dependencyName, version string) (zipPath string, err error) {
	zipPath = filepath.Join(cachePath, dependencyName, "@v", version+".zip")
	fileExists, err := utils.IsFileExists(zipPath)
	if err != nil {
		log.Warn(fmt.Sprintf("Could not find zip binary for dependency '%s' at %s.", dependencyName, zipPath))
		return "", err
	}
	// Zip binary does not exist, so we skip it by returning a nil dependency.
	if !fileExists {
		log.Debug("The following file is missing:", zipPath)
		return "", nil
	}
	return zipPath, nil
}

// populateZip adds the zip file as build-info dependency
func populateZip(packageId, zipPath string) (*entities.Dependency, error) {
	// Zip file dependency for the build-info
	zipDependency := &entities.Dependency{Id: packageId}
	fileDetails, err := fileutils.GetFileDetails(zipPath, true)
	if err != nil {
		return nil, err
	}
	zipDependency.Type = "zip"
	zipDependency.Checksum = &entities.Checksum{Sha1: fileDetails.Checksum.Sha1, Md5: fileDetails.Checksum.Md5}
	return zipDependency, nil
}
