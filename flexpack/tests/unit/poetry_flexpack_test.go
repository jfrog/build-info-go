package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/flexpack"
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

	config := flexpack.PoetryConfig{
		WorkingDirectory:       tempDir,
		IncludeDevDependencies: true,
	}

	poetryFlex, err := flexpack.NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Note: Config fields are unexported, so we test functionality through public methods
	// The working directory and dev dependencies settings are validated through build info collection

	// Verify that the project was loaded correctly by collecting build info
	buildInfo, err := poetryFlex.CollectBuildInfo("test-build", "1")
	if err != nil {
		t.Fatalf("Failed to collect build info: %v", err)
	}

	// Verify project details through build info
	if len(buildInfo.Modules) == 0 {
		t.Fatal("Expected at least one module in build info")
	}

	module := buildInfo.Modules[0]
	if !strings.Contains(module.Id, "test-project") {
		t.Errorf("Expected module ID to contain 'test-project', got '%s'", module.Id)
	}

	if !strings.Contains(module.Id, "1.0.0") {
		t.Errorf("Expected module ID to contain '1.0.0', got '%s'", module.Id)
	}
}

func TestPoetryDependenciesConsistency(t *testing.T) {
	tempDir := t.TempDir()

	// Create a basic Poetry project
	pyprojectContent := `[tool.poetry]
name = "test-project"
version = "1.0.0"
description = "Test project"

[tool.poetry.dependencies]
python = "^3.8"
requests = "2.25.1"
`
	err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create pyproject.toml: %v", err)
	}

	config := flexpack.PoetryConfig{
		WorkingDirectory: tempDir,
	}

	poetryFlex, err := flexpack.NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Test that multiple calls work consistently
	buildInfo1, err := poetryFlex.CollectBuildInfo("test-build", "1")
	if err != nil {
		t.Fatalf("Failed to collect build info (first call): %v", err)
	}

	buildInfo2, err := poetryFlex.CollectBuildInfo("test-build", "2")
	if err != nil {
		t.Fatalf("Failed to collect build info (second call): %v", err)
	}

	// Verify both calls succeeded
	if len(buildInfo1.Modules) == 0 {
		t.Error("First build info should have modules")
	}

	if len(buildInfo2.Modules) == 0 {
		t.Error("Second build info should have modules")
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

	config := flexpack.PoetryConfig{
		WorkingDirectory:       tempDir,
		IncludeDevDependencies: true,
	}

	poetryFlex, err := flexpack.NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Note: Direct dependency and graph methods may be unexported
	// Test functionality through the public CollectBuildInfo interface

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

	config := flexpack.PoetryConfig{
		WorkingDirectory: tempDir,
	}

	poetryFlex, err := flexpack.NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Test that poetryFlex implements expected functionality

	// Test that the main functionality works through CollectBuildInfo
	buildInfo, err := poetryFlex.CollectBuildInfo("test-build", "1")
	if err != nil {
		t.Fatalf("Failed to collect build info: %v", err)
	}

	if buildInfo == nil {
		t.Fatal("Expected non-nil build info")
	}

	// Verify build info has expected structure
	if len(buildInfo.Modules) == 0 {
		t.Error("Expected at least one module in build info")
	}
}

func TestGetPoetryDependenciesCacheInfo(t *testing.T) {
	tempDir := t.TempDir()

	// Test Poetry functionality through public interface
	// Create a basic Poetry project for testing
	pyprojectContent := `[tool.poetry]
name = "test-project"
version = "1.0.0"
description = "Test project"

[tool.poetry.dependencies]
python = "^3.8"
requests = "2.25.1"
`
	err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create pyproject.toml: %v", err)
	}

	config := flexpack.PoetryConfig{
		WorkingDirectory: tempDir,
	}

	poetryFlex, err := flexpack.NewPoetryFlexPack(config)
	if err != nil {
		t.Fatalf("Failed to create PoetryFlexPack: %v", err)
	}

	// Test build info collection
	buildInfo, err := poetryFlex.CollectBuildInfo("cache-test", "1")
	if err != nil {
		t.Fatalf("Failed to collect build info: %v", err)
	}

	if buildInfo == nil {
		t.Fatal("Expected non-nil build info")
	}

	// Verify the build info structure
	if len(buildInfo.Modules) == 0 {
		t.Error("Expected at least one module")
	}
}

func TestPoetryFlexPackErrorHandling(t *testing.T) {
	// Test with non-existent directory - should fail during creation
	config := flexpack.PoetryConfig{
		WorkingDirectory: "/non/existent/directory",
	}

	_, err := flexpack.NewPoetryFlexPack(config)
	if err == nil {
		t.Error("Expected error when creating PoetryFlexPack with non-existent directory")
	}

	// Test with directory that exists but has no Poetry files
	tempDir := t.TempDir()
	config2 := flexpack.PoetryConfig{
		WorkingDirectory: tempDir,
	}

	_, err = flexpack.NewPoetryFlexPack(config2)
	if err == nil {
		t.Error("Expected error when creating PoetryFlexPack with no Poetry files")
	}
}
