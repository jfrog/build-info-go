package dependencies

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
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

// ProjectVersion returns the project's own version as recorded in project.assets.json.
func (extractor *assetsExtractor) ProjectVersion() string {
	return extractor.assets.Project.Version
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
	for tfm, dependencies := range assets.Targets {
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
				// rare in real assets.json but kept for safety/back-compat. RequestedBy lookup
				// may miss in this case because dependenciesMap is keyed by resolved version,
				// so log the fallback for downstream debugging.
				childKey, ok := resolvedInTfm[strings.ToLower(transitiveName)]
				if !ok {
					childKey = strings.ToLower(transitiveName + ":" + transitiveVersion)
					log.Debug(fmt.Sprintf("getChildrenMap: no resolved library entry for transitive %q (declared %q) under parent %q in TFM %q; falling back to declared version — RequestedBy may not link.", transitiveName, transitiveVersion, dependencyId, tfm))
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
	// project.assets.json is the source of truth for the dependency graph. Dependencies are
	// recorded even when their .nupkg is absent from the local cache (e.g. a custom
	// NUGET_PACKAGES path or the SDK fallback folder); checksums are added when the file is
	// available. This prevents dependencies from being silently dropped (jfrog-cli#600, #1796).
	privateDeps := assets.getPrivateDependencyNames()
	for dependencyId, library := range assets.Libraries {
		if library.Type == "project" {
			continue
		}
		dependencyKey := strings.ToLower(getDependencyIdForBuildInfo(dependencyId))
		dependency := &buildinfo.Dependency{Id: getDependencyIdForBuildInfo(dependencyId), Type: nupkgType}

		checksum, err := assets.dependencyChecksum(packagesPath, library, log)
		if err != nil {
			return nil, err
		}
		if checksum != nil {
			dependency.Checksum = *checksum
		}

		// Map PrivateAssets=all references (suppressParent="all" in project.assets.json) to the
		// "private" build-info scope; all other packages are compile-time dependencies.
		if privateDeps[getDependencyName(dependencyId)] {
			dependency.Scopes = []string{privateScope}
		} else {
			dependency.Scopes = []string{compileScope}
		}

		dependencies[dependencyKey] = dependency
	}

	return dependencies, nil
}

const (
	privateScope = "private"
	compileScope = "compile"
	nupkgType    = "nupkg"
)

// dependencyChecksum computes the SHA1/SHA256/MD5 for a library's .nupkg when it exists in the
// local cache. It returns nil (without error) when the package file cannot be located, so the
// dependency is still recorded from project.assets.json but without checksums.
func (assets *assets) dependencyChecksum(packagesPath string, library library, log utils.Log) (*buildinfo.Checksum, error) {
	nupkgFileName, err := library.getNupkgFileName()
	if err != nil {
		// The library entry does not reference a .nupkg file name; record without checksums.
		log.Debug("Could not determine nupkg file name for", library.Path, "-", err.Error())
		return nil, nil
	}
	nupkgFilePath := filepath.Join(packagesPath, library.Path, nupkgFileName)
	rel, err := filepath.Rel(packagesPath, nupkgFilePath)
	if err != nil {
		return nil, fmt.Errorf("computing relative path for package %s: %w", library.Path, err)
	}
	if strings.HasPrefix(rel, "..") {
		log.Warn("Skipping library with path outside packages directory:", nupkgFilePath)
		return nil, nil
	}
	exists, err := utils.IsFileExists(nupkgFilePath, false)
	if err != nil {
		return nil, err
	}
	if !exists {
		log.Warn("The file", nupkgFilePath, "doesn't exist in the NuGet cache directory."+absentNupkgWarnMsg)
		return nil, nil
	}
	fileDetails, err := crypto.GetFileDetails(nupkgFilePath, true)
	if err != nil {
		return nil, err
	}
	return &buildinfo.Checksum{
		Sha1:   fileDetails.Checksum.Sha1,
		Sha256: fileDetails.Checksum.Sha256,
		Md5:    fileDetails.Checksum.Md5,
	}, nil
}

// getPrivateDependencyNames returns the set of direct dependency names that are declared with
// PrivateAssets=all, recorded as suppressParent="All" on the framework dependency entries.
func (assets *assets) getPrivateDependencyNames() map[string]bool {
	private := map[string]bool{}
	for _, framework := range assets.Project.Frameworks {
		for name, dep := range framework.Dependencies {
			if strings.EqualFold(dep.SuppressParent, "all") {
				private[strings.ToLower(name)] = true
			}
		}
	}
	return private
}

// Dependencies-id in assets is built in form of: <package-name>/<version>.
// The Build-info format of dependency id is: <package-name>:<version>.
func getDependencyIdForBuildInfo(dependencyAssetId string) string {
	return strings.Replace(dependencyAssetId, "/", ":", 1)
}

func getDependencyName(dependencyId string) string {
	name, _, _ := strings.Cut(dependencyId, "/")
	return strings.ToLower(name)
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
	// SuppressParent is set to "All" in project.assets.json when the PackageReference uses
	// PrivateAssets=all; it is mapped to the "private" build-info dependency scope.
	SuppressParent string `json:"suppressParent,omitempty"`
}
