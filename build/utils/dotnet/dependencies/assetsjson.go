package dependencies

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/crypto"
)

const (
	AssetFileName = "project.assets.json"
	AssetDirName  = "obj"
)

// Register project.assets.json extractor
func init() {
	register(&assetsExtractor{})
}

// project.assets.json dependency extractor
type assetsExtractor struct {
	assets *assets
}

func (extractor *assetsExtractor) IsCompatible(projectName, dependenciesSource string, log utils.Log) bool {
	if strings.HasSuffix(dependenciesSource, AssetFileName) {
		log.Debug("Found", dependenciesSource, "file for project:", projectName)
		return true
	}
	return false
}

func (extractor *assetsExtractor) DirectDependencies() ([]string, error) {
	return extractor.assets.getDirectDependencies(), nil
}

func (extractor *assetsExtractor) AllDependencies(log utils.Log) (map[string]*buildinfo.Dependency, error) {
	return extractor.assets.getAllDependencies(log)
}

func (extractor *assetsExtractor) ChildrenMap() (map[string][]string, error) {
	return extractor.assets.getChildrenMap(), nil
}

// Create new assets json extractor.
func (extractor *assetsExtractor) new(dependenciesSource string, log utils.Log) (Extractor, error) {
	newExtractor := &assetsExtractor{}
	content, err := os.ReadFile(dependenciesSource)
	if err != nil {
		return nil, err
	}

	assets := &assets{}
	err = json.Unmarshal(content, assets)
	if err != nil {
		return nil, err
	}
	newExtractor.assets = assets
	return newExtractor, nil
}

func (assets *assets) getChildrenMap() map[string][]string {
	// Key by name:version to preserve per-TFM entries; use a set to deduplicate children across TFMs.
	// Transitive version strings in targets/dependencies are the declared constraint (e.g. "[1.12.9, )"),
	// not the resolved version. Build a per-TFM name->resolved-name:version map so child keys match
	// the resolved versions used as keys in dependenciesMap (getAllDependencies).
	dependenciesRelations := map[string]map[string]struct{}{}
	for _, dependencies := range assets.Targets {
		resolvedInTfm := map[string]string{}
		for depId := range dependencies {
			if idx := strings.Index(depId, "/"); idx != -1 {
				resolvedInTfm[strings.ToLower(depId[:idx])] = strings.ToLower(getDependencyIdForBuildInfo(depId))
			}
		}
		for dependencyId, targetDependencies := range dependencies {
			dependencyKey := strings.ToLower(getDependencyIdForBuildInfo(dependencyId))
			if _, ok := dependenciesRelations[dependencyKey]; !ok {
				dependenciesRelations[dependencyKey] = map[string]struct{}{}
			}
			for transitiveName, transitiveVersion := range targetDependencies.Dependencies {
				// Prefer per-TFM resolved version (from library entry in the same target).
				// Fall back to declared constraint when no library entry exists in this TFM —
				// rare in real assets.json but kept for safety/back-compat.
				childKey, ok := resolvedInTfm[strings.ToLower(transitiveName)]
				if !ok {
					childKey = strings.ToLower(transitiveName + ":" + transitiveVersion)
				}
				dependenciesRelations[dependencyKey][childKey] = struct{}{}
			}
		}
	}
	// Convert sets to sorted slices for deterministic output
	result := make(map[string][]string, len(dependenciesRelations))
	for dependencyKey, transitiveSet := range dependenciesRelations {
		result[dependencyKey] = setToSortedSlice(transitiveSet)
	}
	return result
}

func setToSortedSlice(values map[string]struct{}) []string {
	sortedValues := make([]string, 0, len(values))
	for value := range values {
		sortedValues = append(sortedValues, value)
	}
	sort.Strings(sortedValues)
	return sortedValues
}

func (assets *assets) getDirectDependencies() []string {
	// Collect direct dep names from all frameworks
	directNames := map[string]bool{}
	for _, framework := range assets.Project.Frameworks {
		for depName := range framework.Dependencies {
			directNames[strings.ToLower(depName)] = true
		}
	}
	// Cross-reference with Libraries to resolve name:version for each direct dep.
	// A single package name may resolve to multiple versions across TFMs — each is kept.
	seen := map[string]struct{}{}
	for libId, library := range assets.Libraries {
		if library.Type == "project" {
			continue
		}
		if directNames[getDependencyName(libId)] {
			seen[strings.ToLower(getDependencyIdForBuildInfo(libId))] = struct{}{}
		}
	}
	return setToSortedSlice(seen)
}

func (assets *assets) getAllDependencies(log utils.Log) (map[string]*buildinfo.Dependency, error) {
	dependencies := map[string]*buildinfo.Dependency{}
	packagesPath := assets.Project.Restore.PackagesPath
	for dependencyId, library := range assets.Libraries {
		if library.Type == "project" {
			continue
		}
		nupkgFileName, err := library.getNupkgFileName()
		if err != nil {
			return nil, err
		}
		nupkgFilePath := filepath.Join(packagesPath, library.Path, nupkgFileName)
		exists, err := utils.IsFileExists(nupkgFilePath, false)
		if err != nil {
			return nil, err
		}
		if !exists {
			if assets.isPackagePartOfTargetDependencies(library.Path) {
				log.Warn("The file", nupkgFilePath, "doesn't exist in the NuGet cache directory but it does exist as a target in the assets files."+absentNupkgWarnMsg)
				continue
			}
			return nil, errors.New("The file " + nupkgFilePath + " doesn't exist in the NuGet cache directory.")
		}
		fileDetails, err := crypto.GetFileDetails(nupkgFilePath, true)
		if err != nil {
			return nil, err
		}

		dependencyKey := strings.ToLower(getDependencyIdForBuildInfo(dependencyId))
		dependencies[dependencyKey] = &buildinfo.Dependency{Id: getDependencyIdForBuildInfo(dependencyId), Checksum: buildinfo.Checksum{Sha1: fileDetails.Checksum.Sha1, Md5: fileDetails.Checksum.Md5}}
	}

	return dependencies, nil
}

// If the package is included in the targets section of the assets.json file,
// then this is a .NET dependency that shouldn't be included in the build-info dependencies list
// (it come with the SDK).
// Those files are located in the following path: C:\Program Files\dotnet\sdk\NuGetFallbackFolder
func (assets *assets) isPackagePartOfTargetDependencies(nugetPackageName string) bool {
	for _, dependencies := range assets.Targets {
		for dependencyId := range dependencies {
			// The package names in the targets section of the assets.json file are
			// case insensitive.
			if strings.EqualFold(dependencyId, nugetPackageName) {
				return true
			}
		}
	}
	return false
}

// Dependencies-id in assets is built in form of: <package-name>/<version>.
// The Build-info format of dependency id is: <package-name>:<version>.
func getDependencyIdForBuildInfo(dependencyAssetId string) string {
	return strings.Replace(dependencyAssetId, "/", ":", 1)
}

func getDependencyName(dependencyId string) string {
	return strings.ToLower(dependencyId)[0:strings.Index(dependencyId, "/")]
}

// Assets json objects for unmarshalling
type assets struct {
	Version   int
	Targets   map[string]map[string]targetDependency `json:"targets,omitempty"`
	Libraries map[string]library                     `json:"libraries,omitempty"`
	Project   project                                `json:"project"`
}

type targetDependency struct {
	Dependencies map[string]string `json:"dependencies,omitempty"` // Transitive dependencies
}

type library struct {
	Type  string   `json:"type,omitempty"`
	Path  string   `json:"path,omitempty"`
	Files []string `json:"files,omitempty"`
}

func (library *library) getNupkgFileName() (string, error) {
	for _, fileName := range library.Files {
		if strings.HasSuffix(fileName, "nupkg.sha512") {
			return strings.TrimSuffix(fileName, ".sha512"), nil
		}
	}
	return "", fmt.Errorf("could not find nupkg file name for: %s", library.Path)
}

type project struct {
	Version    string               `json:"version,omitempty"`
	Restore    restore              `json:"restore"`
	Frameworks map[string]framework `json:"frameworks,omitempty"`
}

type restore struct {
	PackagesPath string `json:"packagesPath"`
}

type framework struct {
	Dependencies map[string]dependency `json:"dependencies,omitempty"` // Direct dependencies
}

type dependency struct {
	Target  string `json:"target"`
	Version string `json:"version,omitempty"`
}
