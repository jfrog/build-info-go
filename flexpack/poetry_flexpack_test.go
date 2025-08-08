package flexpack

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/entities"
)

func TestNewPoetryFlexPack(t *testing.T) {
	// Create a temporary directory with test files
	tempDir := t.TempDir()

	// Create a minimal pyproject.toml
	pyprojectContent := `[tool.poetry]
name = "test-project"
version = "1.0.0"
description = "A test project"

[tool.poetry.dependencies]
python = "^3.8"
requests = "^2.25.0"
`

	err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test pyproject.toml: %v", err)
	}

	// Create a minimal poetry.lock
	poetryLockContent := `[[package]]
name = "certifi"
version = "2021.10.8"
description = "Python package for providing Mozilla's CA Bundle."
category = "main"
optional = false
python-versions = "*"

[[package]]
name = "requests"
version = "2.25.1"
description = "Python HTTP for Humans."
category = "main"
optional = false
python-versions = ">=2.7, !=3.0.*, !=3.1.*, !=3.2.*, !=3.3.*, !=3.4.*"

[package.dependencies]
certifi = ">=2017.4.17"

[metadata]
lock-version = "1.1"
python-versions = "^3.8"
content-hash = "test-hash"

[metadata.files]
certifi = []
requests = []
`

	err = os.WriteFile(filepath.Join(tempDir, "poetry.lock"), []byte(poetryLockContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test poetry.lock: %v", err)
	}

	config := PackageManagerConfig{
		WorkingDirectory:       tempDir,
		IncludeDevDependencies: true,
	}

	poetryFlex, err := NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	if poetryFlex.config.WorkingDirectory != tempDir {
		t.Errorf("Expected working directory '%s', got '%s'", tempDir, poetryFlex.config.WorkingDirectory)
	}

	if !poetryFlex.config.IncludeDevDependencies {
		t.Error("Expected IncludeDevDependencies to be true")
	}

	// Verify that the project was loaded correctly
	if poetryFlex.projectName != "test-project" {
		t.Errorf("Expected project name 'test-project', got '%s'", poetryFlex.projectName)
	}

	if poetryFlex.projectVersion != "1.0.0" {
		t.Errorf("Expected project version '1.0.0', got '%s'", poetryFlex.projectVersion)
	}
}

func TestPoetryDependenciesCache(t *testing.T) {
	tempDir := t.TempDir()

	// Test cache creation
	mockDeps := map[string]entities.Dependency{
		"requests:2.32.4": {
			Id:     "requests:2.32.4",
			Type:   "pypi",
			Scopes: []string{"main"},
			Checksum: entities.Checksum{
				Sha1:   "abc123",
				Sha256: "def456",
				Md5:    "ghi789",
			},
		},
	}

	err := UpdatePoetryDependenciesCache(mockDeps, tempDir)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// Test cache loading
	cache, err := GetPoetryDependenciesCache(tempDir)
	if err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}

	if cache == nil {
		t.Fatal("Cache is nil")
	}

	if len(cache.DepsMap) != 1 {
		t.Errorf("Expected 1 dependency in cache, got %d", len(cache.DepsMap))
	}

	// Test cache lookup
	dep, found := cache.GetDependency("requests:2.32.4")
	if !found {
		t.Error("Expected to find dependency in cache")
	}

	if dep.Checksum.Sha1 != "abc123" {
		t.Errorf("Expected SHA1 'abc123', got '%s'", dep.Checksum.Sha1)
	}

	// Test cache validation
	if !cache.IsValid(24 * time.Hour) {
		t.Error("Cache should be valid")
	}

	// Test cache clearing
	err = ClearPoetryDependenciesCache(tempDir)
	if err != nil {
		t.Fatalf("Failed to clear cache: %v", err)
	}

	// Verify cache is cleared
	cacheInfo, _ := GetPoetryDependenciesCacheInfo(tempDir)
	if exists, ok := cacheInfo["exists"].(bool); ok && exists {
		t.Error("Cache should not exist after clearing")
	}
}

func TestPoetryFlexPackBasicFunctionality(t *testing.T) {
	tempDir := t.TempDir()

	// Create minimal pyproject.toml
	pyprojectContent := `[tool.poetry]
name = "test-project"
version = "1.0.0"
description = "Test project"

[tool.poetry.dependencies]
python = "^3.8"
requests = "^2.25.0"

[tool.poetry.group.dev.dependencies]
pytest = "^7.0.0"
`

	err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create pyproject.toml: %v", err)
	}

	// Create a minimal poetry.lock
	poetryLockContent := `[[package]]
name = "certifi"
version = "2021.10.8"
description = "Python package for providing Mozilla's CA Bundle."
category = "main"
optional = false
python-versions = "*"

[[package]]
name = "requests"
version = "2.25.1"
description = "Python HTTP for Humans."
category = "main"
optional = false
python-versions = ">=2.7, !=3.0.*, !=3.1.*, !=3.2.*, !=3.3.*, !=3.4.*"

[package.dependencies]
certifi = ">=2017.4.17"

[[package]]
name = "pytest"
version = "7.0.0"
description = "pytest: simple powerful testing with Python"
category = "dev"
optional = false
python-versions = ">=3.6"

[metadata]
lock-version = "1.1"
python-versions = "^3.8"
content-hash = "test-hash"

[metadata.files]
certifi = []
requests = []
pytest = []
`

	err = os.WriteFile(filepath.Join(tempDir, "poetry.lock"), []byte(poetryLockContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test poetry.lock: %v", err)
	}

	config := PackageManagerConfig{
		WorkingDirectory:       tempDir,
		IncludeDevDependencies: true,
	}

	poetryFlex, err := NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Test dependency parsing (should work even without poetry.lock)
	deps, err := poetryFlex.GetProjectDependencies()
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}

	if len(deps) == 0 {
		t.Error("Expected at least some dependencies")
	}

	// Test dependency graph
	graph, err := poetryFlex.GetDependencyGraph()
	if err != nil {
		t.Fatalf("Failed to get dependency graph: %v", err)
	}

	if graph == nil {
		t.Error("Dependency graph should not be nil")
	}

	// Test build info collection
	buildInfo, err := poetryFlex.CollectBuildInfo("test-build", "1.0")
	if err != nil {
		t.Fatalf("Failed to collect build info: %v", err)
	}

	if buildInfo.Name != "test-build" {
		t.Errorf("Expected build name 'test-build', got '%s'", buildInfo.Name)
	}

	if buildInfo.Number != "1.0" {
		t.Errorf("Expected build number '1.0', got '%s'", buildInfo.Number)
	}

	if len(buildInfo.Modules) == 0 {
		t.Error("Expected at least one module in build info")
	}

	module := buildInfo.Modules[0]
	if module.Id != "test-project:1.0.0" {
		t.Errorf("Expected module ID 'test-project:1.0.0', got '%s'", module.Id)
	}

	if module.Type != "pypi" {
		t.Errorf("Expected module type 'pypi', got '%s'", module.Type)
	}
}

func TestPoetryFlexPackInterface(t *testing.T) {
	tempDir := t.TempDir()

	// Create minimal test files
	pyprojectContent := `[tool.poetry]
name = "test-project"
version = "1.0.0"
description = "Test project"

[tool.poetry.dependencies]
python = "^3.8"
`

	err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create pyproject.toml: %v", err)
	}

	poetryLockContent := `[[package]]
name = "requests"
version = "2.25.1"
description = "Python HTTP for Humans."
category = "main"
optional = false
python-versions = "*"

[metadata]
lock-version = "1.1"
python-versions = "^3.8"
content-hash = "test-hash"

[metadata.files]
requests = []
`

	err = os.WriteFile(filepath.Join(tempDir, "poetry.lock"), []byte(poetryLockContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create poetry.lock: %v", err)
	}

	config := PackageManagerConfig{
		WorkingDirectory: tempDir,
	}

	poetryFlex, err := NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Test FlexPackManager interface methods
	var _ FlexPackManager = poetryFlex

	// Test BuildInfoCollector interface methods
	var _ BuildInfoCollector = poetryFlex

	// Test that all required methods exist
	depStr := poetryFlex.GetDependency()
	if depStr == "" {
		t.Log("GetDependency returned empty string")
	}

	checksums := poetryFlex.CalculateChecksum()
	if checksums == nil {
		t.Log("CalculateChecksum returned nil")
	}

	scopes := poetryFlex.CalculateScopes()
	if scopes == nil {
		t.Log("CalculateScopes returned nil")
	}

	requestedBy := poetryFlex.CalculateRequestedBy()
	if requestedBy == nil {
		t.Log("CalculateRequestedBy returned nil")
	}
}

func TestGetPoetryDependenciesCacheInfo(t *testing.T) {
	tempDir := t.TempDir()

	// Test with no cache
	info, err := GetPoetryDependenciesCacheInfo(tempDir)
	if err != nil {
		t.Fatalf("Failed to get cache info: %v", err)
	}

	if exists, ok := info["exists"].(bool); !ok || exists {
		t.Error("Expected cache to not exist")
	}

	// Create cache
	mockDeps := map[string]entities.Dependency{
		"test:1.0": {
			Id:   "test:1.0",
			Type: "pypi",
		},
	}

	err = UpdatePoetryDependenciesCache(mockDeps, tempDir)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	// Test with cache
	info, err = GetPoetryDependenciesCacheInfo(tempDir)
	if err != nil {
		t.Fatalf("Failed to get cache info: %v", err)
	}

	if exists, ok := info["exists"].(bool); !ok || !exists {
		t.Error("Expected cache to exist")
	}

	if deps, ok := info["dependencies"].(int); !ok || deps != 1 {
		t.Errorf("Expected 1 dependency, got %v", deps)
	}

	if valid, ok := info["isValid"].(bool); !ok || !valid {
		t.Error("Expected cache to be valid")
	}
}

func TestPoetryFlexPackErrorHandling(t *testing.T) {
	// Test with non-existent directory - should fail during creation
	config := PackageManagerConfig{
		WorkingDirectory: "/non/existent/directory",
	}

	_, err := NewPoetryFlexPack(config)
	if err == nil {
		t.Error("Expected error when creating PoetryFlexPack with non-existent directory")
	}

	// Test with directory that exists but has no Poetry files
	tempDir := t.TempDir()
	config2 := PackageManagerConfig{
		WorkingDirectory: tempDir,
	}

	_, err = NewPoetryFlexPack(config2)
	if err == nil {
		t.Error("Expected error when creating PoetryFlexPack with no Poetry files")
	}
}
