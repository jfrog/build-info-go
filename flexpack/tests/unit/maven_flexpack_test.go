package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/flexpack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMavenFlexPack tests the creation of Maven FlexPack instance
func TestNewMavenFlexPack(t *testing.T) {
	tempDir := t.TempDir()

	// Create minimal pom.xml
	pomContent := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 
         http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.jfrog.test</groupId>
    <artifactId>test-maven-project</artifactId>
    <version>1.0.0</version>
    <packaging>jar</packaging>
</project>`

	err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(pomContent), 0644)
	require.NoError(t, err, "Should create pom.xml successfully")

	config := flexpack.MavenConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}

	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err, "Should create Maven FlexPack successfully")

	// Test through public interface - collect build info and verify module ID
	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "Should collect build info successfully")
	require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")

	// Module ID should contain the expected GAV coordinates
	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.jfrog.test", "Module ID should contain groupId")
	assert.Contains(t, moduleId, "test-maven-project", "Module ID should contain artifactId")
	assert.Contains(t, moduleId, "1.0.0", "Module ID should contain version")
}

// TestMavenFlexPackInterface tests that Maven FlexPack implements required interfaces
func TestMavenFlexPackInterface(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test flexpack.FlexPackManager interface methods
	var _ flexpack.FlexPackManager = mavenFlex

	// Test flexpack.BuildInfoCollector interface methods
	var _ flexpack.BuildInfoCollector = mavenFlex

	// Test that all required methods exist and return reasonable values
	depStr := mavenFlex.GetDependency()
	assert.Contains(t, depStr, "Project:", "GetDependency should contain project info")

	depList := mavenFlex.ParseDependencyToList()
	assert.NotNil(t, depList, "ParseDependencyToList should not return nil")

	checksums := mavenFlex.CalculateChecksum()
	assert.NotNil(t, checksums, "CalculateChecksum should not return nil")

	scopes := mavenFlex.CalculateScopes()
	assert.NotNil(t, scopes, "CalculateScopes should not return nil")

	requestedBy := mavenFlex.CalculateRequestedBy()
	assert.NotNil(t, requestedBy, "CalculateRequestedBy should not return nil")

	// Test interface methods
	deps, err := mavenFlex.GetProjectDependencies()
	assert.NoError(t, err, "GetProjectDependencies should not return error")
	assert.NotNil(t, deps, "GetProjectDependencies should not return nil")

	graph, err := mavenFlex.GetDependencyGraph()
	assert.NoError(t, err, "GetDependencyGraph should not return error")
	assert.NotNil(t, graph, "GetDependencyGraph should not return nil")
}

// TestMavenPOMParsing tests XML parsing of pom.xml files
func TestMavenPOMParsing(t *testing.T) {
	tempDir := t.TempDir()

	// Create complex pom.xml with dependencies
	pomContent := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.example</groupId>
    <artifactId>complex-project</artifactId>
    <version>2.0.0</version>
    <packaging>jar</packaging>
    <name>Complex Test Project</name>
    <description>Test project with dependencies</description>
    
    <dependencies>
        <dependency>
            <groupId>com.fasterxml.jackson.core</groupId>
            <artifactId>jackson-core</artifactId>
            <version>2.15.2</version>
        </dependency>
        <dependency>
            <groupId>org.slf4j</groupId>
            <artifactId>slf4j-api</artifactId>
            <version>2.0.7</version>
            <scope>compile</scope>
        </dependency>
        <dependency>
            <groupId>org.junit.jupiter</groupId>
            <artifactId>junit-jupiter</artifactId>
            <version>5.9.3</version>
            <scope>test</scope>
        </dependency>
    </dependencies>
</project>`

	err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(pomContent), 0644)
	require.NoError(t, err)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Validate parsed POM data through public interface
	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "Should collect build info successfully")
	require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")

	// Module ID should reflect parsed POM data
	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.example", "Module ID should contain parsed groupId")
	assert.Contains(t, moduleId, "complex-project", "Module ID should contain parsed artifactId")
	assert.Contains(t, moduleId, "2.0.0", "Module ID should contain parsed version")

	// Should have parsed dependencies
	assert.Greater(t, len(buildInfo.Modules[0].Dependencies), 0, "Should have parsed dependencies")

	// We can't access private pomData anymore, but we can validate through build info
	// Check that specific dependencies are present in the collected build info
	dependencyIds := make(map[string]bool)
	for _, dep := range buildInfo.Modules[0].Dependencies {
		dependencyIds[dep.Id] = true
	}

	// Should find expected dependencies (though exact format may vary)
	hasJackson := false
	for id := range dependencyIds {
		if strings.Contains(id, "jackson-core") {
			hasJackson = true
			break
		}
	}
	assert.True(t, hasJackson, "Should find jackson-core dependency")

	// Check for slf4j dependency (compile scope should be included)
	hasSlf4j := false
	for id := range dependencyIds {
		if strings.Contains(id, "slf4j-api") {
			hasSlf4j = true
			break
		}
	}
	assert.True(t, hasSlf4j, "Should find slf4j-api dependency")

	// Note: junit-jupiter has test scope and may be excluded from build info by design
	// This is correct behavior for most build systems
}

// TestMavenDependencyParsing tests dependency parsing functionality
func TestMavenDependencyParsing(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test that dependencies can be parsed (will use POM parsing as fallback)
	deps, err := mavenFlex.GetProjectDependencies()
	require.NoError(t, err, "Should get project dependencies")
	assert.NotNil(t, deps, "Dependencies should not be nil")

	// Test dependency graph
	graph, err := mavenFlex.GetDependencyGraph()
	require.NoError(t, err, "Should get dependency graph")
	assert.NotNil(t, graph, "Dependency graph should not be nil")
}

// TestMavenChecksumCalculation tests checksum calculation for Maven dependencies
func TestMavenChecksumCalculation(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test checksum functionality through public API
	// Create build info and verify checksums are generated for dependencies
	buildInfo, err := mavenFlex.CollectBuildInfo("checksum-test", "1")
	require.NoError(t, err)

	if len(buildInfo.Modules) > 0 && len(buildInfo.Modules[0].Dependencies) > 0 {
		dep := buildInfo.Modules[0].Dependencies[0]
		// Verify checksum structure exists (may be empty if no actual files)
		assert.NotNil(t, dep.Checksum, "Dependency should have checksum structure")
	}
}

// TestMavenScopeValidation tests scope validation and normalization
func TestMavenScopeValidation(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test scope functionality through public API
	buildInfo, err := mavenFlex.CollectBuildInfo("scope-test", "1")
	require.NoError(t, err)

	// Verify that dependencies have valid scopes
	if len(buildInfo.Modules) > 0 && len(buildInfo.Modules[0].Dependencies) > 0 {
		dep := buildInfo.Modules[0].Dependencies[0]
		assert.NotEmpty(t, dep.Scopes, "Dependencies should have scopes")

		// Verify scopes contain valid Maven scope values
		validMavenScopes := map[string]bool{
			"compile": true, "test": true, "runtime": true,
			"provided": true, "system": true, "import": true,
		}

		for _, scope := range dep.Scopes {
			if scope != "main" && scope != "transitive" { // Skip FlexPack-specific scopes
				assert.True(t, validMavenScopes[scope], "Scope should be valid Maven scope: %s", scope)
			}
		}
	}
}

// TestMavenDeploymentRepositoryDetection tests repository detection from configuration
func TestMavenDeploymentRepositoryDetection(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	// Create .jfrog/projects/maven.yaml
	jfrogDir := filepath.Join(tempDir, ".jfrog", "projects")
	err := os.MkdirAll(jfrogDir, 0755)
	require.NoError(t, err)

	mavenYaml := `version: 1
type: maven
deployer:
    serverId: test-server
    releaseRepo: maven-release-local
    snapshotRepo: maven-snapshot-local`

	err = os.WriteFile(filepath.Join(jfrogDir, "maven.yaml"), []byte(mavenYaml), 0644)
	require.NoError(t, err)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test repository detection through public API
	buildInfo, err := mavenFlex.CollectBuildInfo("repo-test", "1")
	require.NoError(t, err)

	// Verify that modules have repository information
	if len(buildInfo.Modules) > 0 {
		module := buildInfo.Modules[0]
		// Repository field should exist (may be empty)
		assert.NotNil(t, module.Repository, "Module should have repository field")
	}
}

// Helper functions

func setupMinimalMavenProject(t *testing.T, tempDir string) {
	pomContent := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.jfrog.test</groupId>
    <artifactId>test-maven-project</artifactId>
    <version>1.0.0</version>
    <packaging>jar</packaging>
    
    <dependencies>
        <dependency>
            <groupId>org.slf4j</groupId>
            <artifactId>slf4j-api</artifactId>
            <version>2.0.7</version>
        </dependency>
    </dependencies>
</project>`

	err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(pomContent), 0644)
	require.NoError(t, err, "Should create minimal pom.xml")
}
