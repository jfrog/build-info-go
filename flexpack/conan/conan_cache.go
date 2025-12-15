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

// ConanDependenciesCache represents cached dependency information for Conan projects.
// It stores checksums for dependencies to avoid recalculating them on every build.
type ConanDependenciesCache struct {
	Version     int                            `json:"version,omitempty"`
	DepsMap     map[string]entities.Dependency `json:"dependencies,omitempty"`
	LastUpdated time.Time                      `json:"lastUpdated,omitempty"`
	ProjectPath string                         `json:"projectPath,omitempty"`
}

// GetConanDependenciesCache reads the JSON cache file of project's dependencies.
// Returns nil if cache doesn't exist (not an error condition).
func GetConanDependenciesCache(projectPath string) (*ConanDependenciesCache, error) {
	cacheFilePath, exists := getConanDependenciesCacheFilePath(projectPath)
	if !exists {
		log.Debug("Conan dependencies cache not found: " + cacheFilePath)
		return nil, nil
	}

	data, err := os.ReadFile(cacheFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	cache := new(ConanDependenciesCache)
	if err := json.Unmarshal(data, cache); err != nil {
		return nil, fmt.Errorf("failed to parse cache file: %w", err)
	}

	log.Debug(fmt.Sprintf("Loaded Conan dependencies cache with %d entries", len(cache.DepsMap)))
	return cache, nil
}

// UpdateConanDependenciesCache writes the updated project's dependencies cache
func UpdateConanDependenciesCache(dependenciesMap map[string]entities.Dependency, projectPath string) error {
	log.Debug(fmt.Sprintf("Updating Conan cache with %d dependencies", len(dependenciesMap)))

	updatedCache := ConanDependenciesCache{
		Version:     conanCacheLatestVersion,
		DepsMap:     dependenciesMap,
		LastUpdated: time.Now(),
		ProjectPath: projectPath,
	}

	content, err := json.MarshalIndent(&updatedCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	cacheFilePath, _ := getConanDependenciesCacheFilePath(projectPath)

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := os.WriteFile(cacheFilePath, content, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	log.Debug(fmt.Sprintf("Cache saved to %s", cacheFilePath))
	return nil
}

// GetDependency returns a dependency from cache by its ID
func (cache *ConanDependenciesCache) GetDependency(dependencyId string) (entities.Dependency, bool) {
	if cache == nil || cache.DepsMap == nil {
		return entities.Dependency{}, false
	}

	dependency, found := cache.DepsMap[dependencyId]
	if found {
		log.Debug("Found cached dependency: " + dependencyId)
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
		log.Debug(fmt.Sprintf("Cache version mismatch: expected %d, got %d", conanCacheLatestVersion, cache.Version))
		return false
	}

	// Check if cache is too old
	if maxAge > 0 && time.Since(cache.LastUpdated) > maxAge {
		log.Debug(fmt.Sprintf("Cache expired: last updated %v ago", time.Since(cache.LastUpdated)))
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
