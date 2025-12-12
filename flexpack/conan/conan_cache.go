package conan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

const conanCacheLatestVersion = 1

// ConanDependenciesCache represents cached dependency information for Conan projects
type ConanDependenciesCache struct {
	Version     int                            `json:"version,omitempty"`
	DepsMap     map[string]entities.Dependency `json:"dependencies,omitempty"`
	LastUpdated time.Time                      `json:"lastUpdated,omitempty"`
	ProjectPath string                         `json:"projectPath,omitempty"`
}

// GetConanDependenciesCache reads the JSON cache file of recent used project's dependencies
func GetConanDependenciesCache(projectPath string) (cache *ConanDependenciesCache, err error) {
	cache = new(ConanDependenciesCache)
	cacheFilePath, exists := getConanDependenciesCacheFilePath(projectPath)
	if !exists {
		log.Debug("Conan dependencies cache not found: " + cacheFilePath)
		return nil, nil
	}

	data, err := os.ReadFile(cacheFilePath)
	if err != nil {
		log.Debug("Failed to read Conan cache file: " + err.Error())
		return nil, err
	}

	err = json.Unmarshal(data, cache)
	if err != nil {
		log.Debug("Failed to parse Conan cache file: " + err.Error())
		return nil, err
	}

	log.Debug(fmt.Sprintf("Loaded Conan dependencies cache with %d entries", len(cache.DepsMap)))
	return cache, nil
}

// UpdateConanDependenciesCache writes the updated project's dependencies cache
func UpdateConanDependenciesCache(dependenciesMap map[string]entities.Dependency, projectPath string) error {
	updatedCache := ConanDependenciesCache{
		Version:     conanCacheLatestVersion,
		DepsMap:     dependenciesMap,
		LastUpdated: time.Now(),
		ProjectPath: projectPath,
	}

	content, err := json.MarshalIndent(&updatedCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Conan cache: %w", err)
	}

	cacheFilePath, _ := getConanDependenciesCacheFilePath(projectPath)

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	err = os.WriteFile(cacheFilePath, content, 0644)
	if err != nil {
		return fmt.Errorf("failed to write Conan cache file: %w", err)
	}

	log.Debug(fmt.Sprintf("Updated Conan dependencies cache with %d entries at %s", len(dependenciesMap), cacheFilePath))
	return nil
}

// GetDependency returns required dependency from cache
func (cache *ConanDependenciesCache) GetDependency(dependencyName string) (dependency entities.Dependency, found bool) {
	if cache == nil || cache.DepsMap == nil {
		return entities.Dependency{}, false
	}

	dependency, found = cache.DepsMap[dependencyName]
	if found {
		log.Debug("Found cached dependency: " + dependencyName)
	}
	return dependency, found
}

// IsValid checks if the cache is valid and not expired
func (cache *ConanDependenciesCache) IsValid(maxAge time.Duration) bool {
	if cache == nil {
		return false
	}

	// Check version compatibility
	if cache.Version != conanCacheLatestVersion {
		log.Debug(fmt.Sprintf("Conan cache version mismatch: expected %d, got %d", conanCacheLatestVersion, cache.Version))
		return false
	}

	// Check if cache is too old
	if maxAge > 0 && time.Since(cache.LastUpdated) > maxAge {
		log.Debug(fmt.Sprintf("Conan cache expired: last updated %v ago", time.Since(cache.LastUpdated)))
		return false
	}

	return true
}

// getConanDependenciesCacheFilePath returns the path to Conan dependencies cache file
func getConanDependenciesCacheFilePath(projectPath string) (cacheFilePath string, exists bool) {
	projectsDirPath := filepath.Join(projectPath, ".jfrog", "projects")
	cacheFilePath = filepath.Join(projectsDirPath, "conan-deps.cache.json")
	_, err := os.Stat(cacheFilePath)
	exists = !os.IsNotExist(err)
	return cacheFilePath, exists
}

// UpdateDependenciesWithCache enhances dependencies with cached information
func (cf *ConanFlexPack) UpdateDependenciesWithCache() error {
	cache, err := cf.loadCache()
	if err != nil {
		cache = nil
	}

	dependenciesMap := make(map[string]entities.Dependency)
	var missingDeps []string

	for i, dep := range cf.dependencies {
		depKey := formatDependencyKey(dep.Name, dep.Version)

		if cachedDep, found := cf.tryGetFromCache(cache, depKey); found {
			dependenciesMap[depKey] = cachedDep
			cf.updateDependencyFromCache(&cf.dependencies[i], cachedDep)
			continue
		}

		if entityDep, ok := cf.calculateAndStoreDependency(dep, depKey); ok {
			dependenciesMap[depKey] = entityDep
			cf.dependencies[i].SHA1 = entityDep.Sha1
			cf.dependencies[i].SHA256 = entityDep.Sha256
			cf.dependencies[i].MD5 = entityDep.Md5
		} else {
			missingDeps = append(missingDeps, depKey)
		}
	}

	cf.reportMissingDependencies(missingDeps)
	cf.saveCache(dependenciesMap)

	return nil
}

// loadCache loads and validates the dependency cache
func (cf *ConanFlexPack) loadCache() (*ConanDependenciesCache, error) {
	cache, err := GetConanDependenciesCache(cf.config.WorkingDirectory)
	if err != nil {
		log.Debug("No existing Conan cache found, will create new one")
		return nil, err
	}

	maxCacheAge := 24 * time.Hour
	if cache != nil && !cache.IsValid(maxCacheAge) {
		log.Debug("Conan cache is invalid or expired, ignoring")
		return nil, nil
	}

	return cache, nil
}

// tryGetFromCache attempts to get a dependency from cache
func (cf *ConanFlexPack) tryGetFromCache(cache *ConanDependenciesCache, depKey string) (entities.Dependency, bool) {
	if cache == nil {
		return entities.Dependency{}, false
	}

	cachedDep, found := cache.GetDependency(depKey)
	if found && !cachedDep.IsEmpty() {
		log.Debug("Using cached checksums for " + depKey)
		return cachedDep, true
	}

	return entities.Dependency{}, false
}

// updateDependencyFromCache updates a dependency with cached values
func (cf *ConanFlexPack) updateDependencyFromCache(dep *DependencyInfo, cachedDep entities.Dependency) {
	dep.SHA1 = cachedDep.Sha1
	dep.SHA256 = cachedDep.Sha256
	dep.MD5 = cachedDep.Md5
}

// calculateAndStoreDependency calculates checksums for a dependency
func (cf *ConanFlexPack) calculateAndStoreDependency(dep DependencyInfo, depKey string) (entities.Dependency, bool) {
	checksumMap := cf.calculateChecksumWithFallback(dep)
	if checksumMap == nil {
		return entities.Dependency{}, false
	}

	sha1, ok := checksumMap["sha1"].(string)
	if !ok || sha1 == "" {
		return entities.Dependency{}, false
	}

	sha256, _ := checksumMap["sha256"].(string)
	md5, _ := checksumMap["md5"].(string)

	entityDep := entities.Dependency{
		Id:     depKey,
		Type:   "conan",
		Scopes: dep.Scopes,
		Checksum: entities.Checksum{
			Sha1:   sha1,
			Sha256: sha256,
			Md5:    md5,
		},
	}

	log.Debug("Calculated new checksums for " + depKey)
	return entityDep, true
}

// reportMissingDependencies logs warnings for dependencies that couldn't be resolved
func (cf *ConanFlexPack) reportMissingDependencies(missingDeps []string) {
	if len(missingDeps) == 0 {
		return
	}

	log.Warn("The following Conan packages could not be found or checksums calculated:")
	for _, dep := range missingDeps {
		log.Warn("  - " + dep)
	}
	log.Warn("This may happen if packages are not in Conan cache. Run 'conan install' to populate the cache.")
}

// saveCache saves the dependency cache
func (cf *ConanFlexPack) saveCache(dependenciesMap map[string]entities.Dependency) {
	if len(dependenciesMap) == 0 {
		return
	}

	err := UpdateConanDependenciesCache(dependenciesMap, cf.config.WorkingDirectory)
	if err != nil {
		log.Warn("Failed to update Conan dependencies cache: " + err.Error())
	}
}

// formatDependencyKey creates a consistent dependency key
func formatDependencyKey(name, version string) string {
	return fmt.Sprintf("%s:%s", name, version)
}

