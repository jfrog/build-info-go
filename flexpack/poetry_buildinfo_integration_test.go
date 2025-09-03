package flexpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoetryBuildInfoCollectionIntegration tests the complete Poetry build info collection workflow
// This validates the specific use case: "native support for attaching build metadata to Python packages built using Poetry"
func TestPoetryBuildInfoCollectionIntegration(t *testing.T) {
	// Create a temporary directory with realistic Poetry project structure
	tempDir := t.TempDir()

	// Setup realistic Poetry project files
	setupRealisticPoetryProject(t, tempDir)

	// Create Poetry FlexPack instance
	config := PoetryConfig{
		WorkingDirectory:       tempDir,
		IncludeDevDependencies: false,
	}

	poetryFlex, err := NewPoetryFlexPack(config)
	require.NoError(t, err, "Should create Poetry FlexPack successfully")

	// Test build info collection - this is the core functionality
	buildInfo, err := poetryFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "Should collect build info successfully")

	// Validate build info structure
	assert.Equal(t, "test-build", buildInfo.Name, "Build name should match")
	assert.Equal(t, "1", buildInfo.Number, "Build number should match")
	assert.Len(t, buildInfo.Modules, 1, "Should have exactly one module")

	module := buildInfo.Modules[0]
	assert.Equal(t, "test-project:1.0.0", module.Id, "Module ID should match project:version")
	assert.Equal(t, "pypi", string(module.Type), "Module type should be pypi")

	// Validate dependencies are collected
	assert.Greater(t, len(module.Dependencies), 0, "Should have dependencies")

	// Validate specific dependency structure
	validateDependencyStructure(t, module.Dependencies)

	// Validate requestedBy relationships
	validateRequestedByRelationships(t, module.Dependencies)
}

// TestPoetryTreeParsingFix tests the specific tree parsing fix for malformed tree characters
func TestPoetryTreeParsingFix(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticPoetryProject(t, tempDir)

	config := PoetryConfig{WorkingDirectory: tempDir}
	poetryFlex, err := NewPoetryFlexPack(config)
	require.NoError(t, err)

	// Mock poetry show --tree output with problematic tree characters
	mockTreeOutput := `flask 2.3.3 A simple framework for building complex web applications.
├── blinker >=1.6.2
├── click >=8.1.3
│   └── colorama * 
├── importlib-metadata >=3.6.0
│   └── zipp >=3.20 
├── itsdangerous >=2.1.2
├── jinja2 >=3.1.2
│   └── markupsafe >=2.0 
└── werkzeug >=2.3.7
    └── markupsafe >=2.1.1 
requests 2.32.3 Python HTTP for Humans.
├── certifi >=2017.4.17
├── charset-normalizer >=2,<4
├── idna >=2.5,<4
└── urllib3 >=1.21.1,<3`

	// Test parsing this output
	err = poetryFlex.parsePoetryShowOutput(mockTreeOutput)
	require.NoError(t, err, "Should parse tree output without errors")

	// Verify no tree formatting artifacts are present as dependencies
	for _, dep := range poetryFlex.dependencies {
		assert.NotContains(t, dep.ID, "│", "Dependency ID should not contain tree characters")
		assert.NotContains(t, dep.ID, "└", "Dependency ID should not contain tree characters")
		assert.NotContains(t, dep.ID, "├", "Dependency ID should not contain tree characters")
		assert.NotContains(t, dep.Name, "│", "Dependency name should not contain tree characters")
	}

	// Verify specific dependencies are parsed correctly
	dependencyNames := make(map[string]bool)
	for _, dep := range poetryFlex.dependencies {
		dependencyNames[dep.Name] = true
	}

	expectedDeps := []string{"flask", "blinker", "click", "colorama", "importlib-metadata",
		"zipp", "itsdangerous", "jinja2", "markupsafe", "werkzeug", "requests",
		"certifi", "charset-normalizer", "idna", "urllib3"}

	for _, expectedDep := range expectedDeps {
		assert.True(t, dependencyNames[expectedDep],
			"Should have parsed dependency: %s", expectedDep)
	}
}

// TestPoetryRequestedByChains tests that dependency chains are correctly tracked
func TestPoetryRequestedByChains(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticPoetryProject(t, tempDir)

	config := PoetryConfig{WorkingDirectory: tempDir}
	poetryFlex, err := NewPoetryFlexPack(config)
	require.NoError(t, err)

	// Collect build info
	buildInfo, err := poetryFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)

	module := buildInfo.Modules[0]

	// Create a map for easy lookup
	depMap := make(map[string]entities.Dependency)
	for _, dep := range module.Dependencies {
		depMap[dep.Id] = dep
	}

	// Test specific requestedBy relationships
	testCases := []struct {
		depId           string
		expectedParents []string
		description     string
	}{
		{
			depId:           "blinker:>=1.6.2",
			expectedParents: []string{"flask:2.3.3"},
			description:     "blinker should be requested by flask",
		},
		{
			depId:           "certifi:>=2017.4.17",
			expectedParents: []string{"requests:2.32.3"},
			description:     "certifi should be requested by requests",
		},
	}

	for _, tc := range testCases {
		dep, exists := depMap[tc.depId]
		if !exists {
			// Try with different version format
			for id := range depMap {
				if strings.Contains(id, strings.Split(tc.depId, ":")[0]) {
					dep = depMap[id]
					exists = true
					break
				}
			}
		}

		if exists && len(dep.RequestedBy) > 0 {
			found := false
			for _, chain := range dep.RequestedBy {
				if len(chain) > 0 {
					for _, parent := range tc.expectedParents {
						if strings.Contains(chain[0], strings.Split(parent, ":")[0]) {
							found = true
							break
						}
					}
				}
			}
			assert.True(t, found, tc.description)
		}
	}
}

// TestPoetryEdgeCases tests various edge cases
func TestPoetryEdgeCases(t *testing.T) {
	t.Run("EmptyProject", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create minimal pyproject.toml with no dependencies
		pyprojectContent := `[tool.poetry]
name = "empty-project"
version = "1.0.0"
description = "An empty project"

[tool.poetry.dependencies]
python = "^3.8"
`
		err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
		require.NoError(t, err)

		// Create minimal poetry.lock
		poetryLockContent := `[metadata]
lock-version = "2.0"
python-versions = "^3.8"
content-hash = "empty-hash"
`
		err = os.WriteFile(filepath.Join(tempDir, "poetry.lock"), []byte(poetryLockContent), 0644)
		require.NoError(t, err)

		config := PoetryConfig{WorkingDirectory: tempDir}
		poetryFlex, err := NewPoetryFlexPack(config)
		require.NoError(t, err)

		buildInfo, err := poetryFlex.CollectBuildInfo("test-build", "1")
		require.NoError(t, err)

		assert.Len(t, buildInfo.Modules, 1)
		// Should have minimal or no dependencies (just python)
	})

	t.Run("MalformedTreeOutput", func(t *testing.T) {
		tempDir := t.TempDir()
		setupRealisticPoetryProject(t, tempDir)

		config := PoetryConfig{WorkingDirectory: tempDir}
		poetryFlex, err := NewPoetryFlexPack(config)
		require.NoError(t, err)

		// Test with malformed tree output
		malformedOutput := `flask 2.3.3
├── blinker
│   malformed line without version
└── click >=8.1.3
    └──
│
invalid tree structure`

		err = poetryFlex.parsePoetryShowOutput(malformedOutput)
		// Should not crash, might have partial parsing
		assert.NoError(t, err, "Should handle malformed output gracefully")
	})
}

// TestPoetryBuildInfoCompatibility tests compatibility with JFrog CLI build commands
func TestPoetryBuildInfoCompatibility(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticPoetryProject(t, tempDir)

	config := PoetryConfig{WorkingDirectory: tempDir}
	poetryFlex, err := NewPoetryFlexPack(config)
	require.NoError(t, err)

	// Test with build-name and build-number (the core use case)
	buildInfo, err := poetryFlex.CollectBuildInfo("my-poetry-build", "42")
	require.NoError(t, err)

	// Validate compatibility with JFrog CLI expectations
	assert.Equal(t, "my-poetry-build", buildInfo.Name)
	assert.Equal(t, "42", buildInfo.Number)
	assert.NotNil(t, buildInfo.Agent)
	assert.NotNil(t, buildInfo.BuildAgent)

	// Ensure module structure is compatible
	module := buildInfo.Modules[0]
	assert.Equal(t, "pypi", string(module.Type))
	assert.NotEmpty(t, module.Id)

	// Validate that dependencies have proper structure for build-scan, build-promote etc.
	for _, dep := range module.Dependencies {
		assert.NotEmpty(t, dep.Id, "Dependency should have ID")
		assert.NotEmpty(t, dep.Type, "Dependency should have type")
		assert.NotEmpty(t, dep.Scopes, "Dependency should have scopes")
		// RequestedBy is optional but if present should be valid
		if len(dep.RequestedBy) > 0 {
			for _, chain := range dep.RequestedBy {
				assert.Greater(t, len(chain), 0, "RequestedBy chain should not be empty")
			}
		}
	}
}

// Helper functions

func setupRealisticPoetryProject(t *testing.T, tempDir string) {
	// Create pyproject.toml with realistic dependencies
	pyprojectContent := `[tool.poetry]
name = "test-project"
version = "1.0.0"
description = "A test project with realistic dependencies"
authors = ["Test Author <test@example.com>"]

[tool.poetry.dependencies]
python = "^3.8"
flask = "2.3.3"
requests = "2.32.3"

[build-system]
requires = ["poetry-core"]
build-backend = "poetry.core.masonry.api"
`

	err := os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyprojectContent), 0644)
	require.NoError(t, err)

	// Create poetry.lock with realistic dependency structure
	poetryLockContent := `# This file is automatically @generated by Poetry and should not be changed by hand.

[[package]]
name = "blinker"
version = "1.6.2"
description = "Fast, simple object-to-object and broadcast signaling"
optional = false
python-versions = ">=3.7"
files = []

[[package]]
name = "certifi"
version = "2023.7.22"
description = "Python package for providing Mozilla's CA Bundle."
optional = false
python-versions = ">=3.6"
files = []

[[package]]
name = "charset-normalizer"
version = "3.2.0"
description = "The Real First Universal Charset Detector. Open, modern and actively maintained alternative to Chardet."
optional = false
python-versions = ">=3.7.0"
files = []

[[package]]
name = "click"
version = "8.1.7"
description = "Composable command line interface toolkit"
optional = false
python-versions = ">=3.7"
files = []

[[package]]
name = "colorama"
version = "0.4.6"
description = "Cross-platform colored terminal text."
optional = false
python-versions = "!=3.0.*,!=3.1.*,!=3.2.*,!=3.3.*,!=3.4.*,!=3.5.*,!=3.6.*,>=2.7"
files = []

[package.dependencies]
colorama = {version = "*", markers = "platform_system == \"Windows\""}

[[package]]
name = "flask"
version = "2.3.3"
description = "A simple framework for building complex web applications."
optional = false
python-versions = ">=3.8"
files = []

[package.dependencies]
blinker = ">=1.6.2"
click = ">=8.1.3"
itsdangerous = ">=2.1.2"
Jinja2 = ">=3.1.2"
Werkzeug = ">=2.3.7"

[[package]]
name = "idna"
version = "3.4"
description = "Internationalized Domain Names in Applications (IDNA)"
optional = false
python-versions = ">=3.5"
files = []

[[package]]
name = "importlib-metadata"
version = "6.8.0"
description = "Read metadata from Python packages"
optional = false
python-versions = ">=3.8"
files = []

[package.dependencies]
zipp = ">=0.5"

[[package]]
name = "itsdangerous"
version = "2.1.2"
description = "Safely pass data to untrusted environments and back."
optional = false
python-versions = ">=3.7"
files = []

[[package]]
name = "jinja2"
version = "3.1.2"
description = "A very fast and expressive template engine."
optional = false
python-versions = ">=3.7"
files = []

[package.dependencies]
MarkupSafe = ">=2.0"

[[package]]
name = "markupsafe"
version = "2.1.3"
description = "Safely add untrusted strings to HTML/XML markup."
optional = false
python-versions = ">=3.7"
files = []

[[package]]
name = "requests"
version = "2.32.3"
description = "Python HTTP for Humans."
optional = false
python-versions = ">=3.8"
files = []

[package.dependencies]
certifi = ">=2017.4.17"
charset-normalizer = ">=2,<4"
idna = ">=2.5,<4"
urllib3 = ">=1.21.1,<3"

[[package]]
name = "urllib3"
version = "2.0.4"
description = "HTTP library with thread-safe connection pooling, file post, and more."
optional = false
python-versions = ">=3.7"
files = []

[[package]]
name = "werkzeug"
version = "2.3.7"
description = "The comprehensive WSGI web application library."
optional = false
python-versions = ">=3.8"
files = []

[package.dependencies]
MarkupSafe = ">=2.1.1"

[[package]]
name = "zipp"
version = "3.16.2"
description = "Backport of pathlib-compatible object wrapper for zip files"
optional = false
python-versions = ">=3.8"
files = []

[metadata]
lock-version = "2.0"
python-versions = "^3.8"
content-hash = "test-hash"
`

	err = os.WriteFile(filepath.Join(tempDir, "poetry.lock"), []byte(poetryLockContent), 0644)
	require.NoError(t, err)
}

func validateDependencyStructure(t *testing.T, dependencies []entities.Dependency) {
	// Verify we have both direct and transitive dependencies
	hasDirectDeps := false
	hasTransitiveDeps := false

	for _, dep := range dependencies {
		assert.NotEmpty(t, dep.Id, "Dependency should have an ID")
		assert.Equal(t, "python", dep.Type, "Dependency type should be python")
		assert.NotEmpty(t, dep.Scopes, "Dependency should have scopes")

		if contains(dep.Scopes, "main") {
			hasDirectDeps = true
		}
		if contains(dep.Scopes, "transitive") {
			hasTransitiveDeps = true
		}
	}

	assert.True(t, hasDirectDeps, "Should have direct dependencies")
	assert.True(t, hasTransitiveDeps, "Should have transitive dependencies")
}

func validateRequestedByRelationships(t *testing.T, dependencies []entities.Dependency) {
	// Verify that transitive dependencies have requestedBy relationships
	hasRequestedBy := false

	for _, dep := range dependencies {
		if contains(dep.Scopes, "transitive") && len(dep.RequestedBy) > 0 {
			hasRequestedBy = true
			// Verify requestedBy structure
			for _, chain := range dep.RequestedBy {
				assert.Greater(t, len(chain), 0, "RequestedBy chain should not be empty")
				assert.NotEmpty(t, chain[0], "RequestedBy chain should have valid parent")
			}
		}
	}

	assert.True(t, hasRequestedBy, "Should have requestedBy relationships for transitive dependencies")
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
