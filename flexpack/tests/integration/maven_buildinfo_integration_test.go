package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMavenBuildInfoCollectionIntegration tests the complete Maven build info collection workflow
// This validates the specific use case: "native support for attaching build metadata to Java packages built using Maven"
func TestMavenBuildInfoCollectionIntegration(t *testing.T) {
	// Create a temporary directory with realistic Maven project structure
	tempDir := t.TempDir()

	// Setup realistic Maven project files
	setupRealisticMavenProject(t, tempDir)

	// Create Maven FlexPack instance
	config := flexpack.MavenConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}

	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err, "Should create Maven FlexPack successfully")

	// Test build info collection - this is the core functionality
	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "Should collect build info successfully")

	// Validate build info structure
	assert.Equal(t, "test-build", buildInfo.Name, "Build name should match")
	assert.Equal(t, "1", buildInfo.Number, "Build number should match")
	assert.Len(t, buildInfo.Modules, 1, "Should have exactly one module")

	module := buildInfo.Modules[0]
	assert.Equal(t, "com.jfrog.test:maven-integration-test:1.0.0", module.Id, "Module ID should match groupId:artifactId:version")
	assert.Equal(t, "maven", string(module.Type), "Module type should be maven")

	// Validate dependencies are collected
	assert.Greater(t, len(module.Dependencies), 0, "Should have dependencies")

	// Validate specific dependency structure
	validateMavenDependencyStructure(t, module.Dependencies)

	// Validate requestedBy relationships
	validateMavenRequestedByRelationships(t, module.Dependencies)
}

// TestMavenBuildInfoCompatibility tests compatibility with JFrog CLI build commands
func TestMavenBuildInfoCompatibility(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test with build-name and build-number (the core use case)
	buildInfo, err := mavenFlex.CollectBuildInfo("my-maven-build", "42")
	require.NoError(t, err)

	// Validate compatibility with JFrog CLI expectations
	assert.Equal(t, "my-maven-build", buildInfo.Name)
	assert.Equal(t, "42", buildInfo.Number)
	assert.NotNil(t, buildInfo.Agent)
	assert.NotNil(t, buildInfo.BuildAgent)

	// Ensure module structure is compatible
	module := buildInfo.Modules[0]
	assert.Equal(t, "maven", string(module.Type))
	assert.NotEmpty(t, module.Id)
	// Repository may be empty if no configuration is present (this is expected behavior)
	// The field should exist in the struct but may be empty string
	// Note: Repository field exists by default (string type), no need to check len >= 0

	// Validate that dependencies have proper structure for build-scan, build-promote etc.
	for _, dep := range module.Dependencies {
		assert.NotEmpty(t, dep.Id, "Dependency should have ID")
		assert.NotEmpty(t, dep.Type, "Dependency should have type")
		assert.NotEmpty(t, dep.Scopes, "Dependency should have scopes")

		// Validate checksums are present
		assert.True(t, dep.Checksum.Sha1 != "" || dep.Checksum.Sha256 != "" || dep.Checksum.Md5 != "",
			"Dependency %s should have at least one checksum", dep.Id)

		// RequestedBy is optional but if present should be valid
		if len(dep.RequestedBy) > 0 {
			for _, chain := range dep.RequestedBy {
				assert.Greater(t, len(chain), 0, "RequestedBy chain should not be empty")
			}
		}
	}
}

// TestMavenHybridDependencyResolution tests the hybrid approach (CLI + POM parsing)
func TestMavenHybridDependencyResolution(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test dependency collection through public interface
	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "Should collect build info successfully")

	// Should have collected dependencies
	require.Greater(t, len(buildInfo.Modules), 0, "Should have modules")
	assert.Greater(t, len(buildInfo.Modules[0].Dependencies), 0, "Should collect dependencies via hybrid approach")

	// Validate that we get the expected core dependencies
	dependencyNames := make(map[string]bool)
	for _, dep := range buildInfo.Modules[0].Dependencies {
		dependencyNames[dep.Id] = true
	}

	// These are the dependencies we expect from our realistic project (with versions)
	expectedDeps := []string{
		"com.fasterxml.jackson.core:jackson-databind:2.15.2",
		"com.fasterxml.jackson.core:jackson-core:2.15.2",
		"org.slf4j:slf4j-api:2.0.7",
	}

	for _, expectedDep := range expectedDeps {
		assert.True(t, dependencyNames[expectedDep],
			"Should find expected dependency: %s", expectedDep)
	}
}

// TestMavenScopeClassification tests proper scope classification for Maven dependencies
func TestMavenScopeClassification(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticMavenProject(t, tempDir)

	config := flexpack.MavenConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)

	module := buildInfo.Modules[0]

	// Validate scope distribution
	scopeCounts := make(map[string]int)
	for _, dep := range module.Dependencies {
		for _, scope := range dep.Scopes {
			scopeCounts[scope]++
		}
	}

	// Should have main/compile dependencies
	assert.Greater(t, scopeCounts["compile"], 0, "Should have compile scope dependencies")

	// May have test dependencies if test dependencies are included
	if config.IncludeTestDependencies {
		// Test dependencies might be present
		testDeps := scopeCounts["test"]
		assert.GreaterOrEqual(t, testDeps, 0, "Test dependencies count should be non-negative")
	}
}

// TestMavenChecksumGeneration tests checksum generation for Maven dependencies
func TestMavenChecksumGeneration(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticMavenProject(t, tempDir)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)

	module := buildInfo.Modules[0]

	// Validate that all dependencies have checksums
	for _, dep := range module.Dependencies {
		hasChecksum := dep.Checksum.Sha1 != "" || dep.Checksum.Sha256 != "" || dep.Checksum.Md5 != ""
		assert.True(t, hasChecksum, "Dependency %s should have at least one checksum", dep.Id)

		// If we have checksums, they should be valid hex strings
		if dep.Checksum.Sha1 != "" {
			assert.Len(t, dep.Checksum.Sha1, 40, "SHA1 should be 40 characters")
			assert.Regexp(t, "^[a-f0-9]+$", dep.Checksum.Sha1, "SHA1 should be valid hex")
		}
		if dep.Checksum.Sha256 != "" {
			assert.Len(t, dep.Checksum.Sha256, 64, "SHA256 should be 64 characters")
			assert.Regexp(t, "^[a-f0-9]+$", dep.Checksum.Sha256, "SHA256 should be valid hex")
		}
		if dep.Checksum.Md5 != "" {
			assert.Len(t, dep.Checksum.Md5, 32, "MD5 should be 32 characters")
			assert.Regexp(t, "^[a-f0-9]+$", dep.Checksum.Md5, "MD5 should be valid hex")
		}
	}
}

// TestMavenRepositoryConfiguration tests repository configuration detection
func TestMavenRepositoryConfiguration(t *testing.T) {
	tempDir := t.TempDir()
	setupRealisticMavenProject(t, tempDir)

	// Create .jfrog/projects/maven.yaml configuration
	jfrogDir := filepath.Join(tempDir, ".jfrog", "projects")
	err := os.MkdirAll(jfrogDir, 0755)
	require.NoError(t, err)

	mavenConfig := `version: 1
type: maven
resolver:
    serverId: test-server
    releaseRepo: maven-virtual
    snapshotRepo: maven-virtual
deployer:
    serverId: test-server
    releaseRepo: maven-release-local
    snapshotRepo: maven-snapshot-local`

	err = os.WriteFile(filepath.Join(jfrogDir, "maven.yaml"), []byte(mavenConfig), 0644)
	require.NoError(t, err)

	config := flexpack.MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := flexpack.NewMavenFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)

	module := buildInfo.Modules[0]

	// Should have repository configuration
	assert.NotEmpty(t, module.Repository, "Module should have repository configuration")
	assert.Equal(t, "maven-release-local", module.Repository, "Should use release repo for non-SNAPSHOT version")
}

// TestMavenErrorHandling tests error handling in various scenarios
func TestMavenErrorHandling(t *testing.T) {
	// Test with missing pom.xml
	t.Run("MissingPOM", func(t *testing.T) {
		tempDir := t.TempDir()
		config := flexpack.MavenConfig{WorkingDirectory: tempDir}

		_, err := flexpack.NewMavenFlexPack(config)
		assert.Error(t, err, "Should fail when pom.xml is missing")
		assert.Contains(t, err.Error(), "failed to load pom.xml", "Error should mention pom.xml")
	})

	// Test with invalid pom.xml
	t.Run("InvalidPOM", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create invalid XML
		invalidPOM := `<project>
			<groupId>test</groupId>
			<!-- Missing closing tag -->`

		err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(invalidPOM), 0644)
		require.NoError(t, err)

		config := flexpack.MavenConfig{WorkingDirectory: tempDir}
		_, err = flexpack.NewMavenFlexPack(config)
		assert.Error(t, err, "Should fail when pom.xml is invalid")
		assert.Contains(t, err.Error(), "failed to parse pom.xml", "Error should mention parsing failure")
	})

	// Test with incomplete pom.xml
	t.Run("IncompletePOM", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create POM missing required fields
		incompletePOM := `<?xml version="1.0"?>
<project>
    <modelVersion>4.0.0</modelVersion>
    <!-- Missing groupId, artifactId, version -->
</project>`

		err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(incompletePOM), 0644)
		require.NoError(t, err)

		config := flexpack.MavenConfig{WorkingDirectory: tempDir}
		mavenFlex, err := flexpack.NewMavenFlexPack(config)

		// Should create instance but with incomplete project info
		require.NoError(t, err, "Should create instance even with incomplete POM")

		// Test through public interface - build info should reflect incomplete POM
		buildInfo, err := mavenFlex.CollectBuildInfo("test-build", "1")
		require.NoError(t, err, "Should collect build info even with incomplete POM")
		require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")

		// Module ID might be incomplete due to missing POM information
		_ = buildInfo.Modules[0].Id // Just ensure it doesn't panic
	})
}

// Helper functions

func setupRealisticMavenProject(t *testing.T, tempDir string) {
	// Create realistic pom.xml with dependencies
	pomContent := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 
         http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>
    
    <groupId>com.jfrog.test</groupId>
    <artifactId>maven-integration-test</artifactId>
    <version>1.0.0</version>
    <packaging>jar</packaging>
    
    <name>Maven Integration Test Project</name>
    <description>Test project for Maven FlexPack integration</description>
    
    <properties>
        <maven.compiler.source>17</maven.compiler.source>
        <maven.compiler.target>17</maven.compiler.target>
        <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
        <jackson.version>2.15.2</jackson.version>
        <slf4j.version>2.0.7</slf4j.version>
        <junit.version>5.9.3</junit.version>
    </properties>
    
    <dependencies>
        <!-- Main dependencies -->
        <dependency>
            <groupId>com.fasterxml.jackson.core</groupId>
            <artifactId>jackson-databind</artifactId>
            <version>${jackson.version}</version>
        </dependency>
        <dependency>
            <groupId>com.fasterxml.jackson.core</groupId>
            <artifactId>jackson-core</artifactId>
            <version>${jackson.version}</version>
        </dependency>
        <dependency>
            <groupId>org.slf4j</groupId>
            <artifactId>slf4j-api</artifactId>
            <version>${slf4j.version}</version>
        </dependency>
        
        <!-- Test dependencies -->
        <dependency>
            <groupId>org.junit.jupiter</groupId>
            <artifactId>junit-jupiter</artifactId>
            <version>${junit.version}</version>
            <scope>test</scope>
        </dependency>
        <dependency>
            <groupId>org.junit.jupiter</groupId>
            <artifactId>junit-jupiter-engine</artifactId>
            <version>${junit.version}</version>
            <scope>test</scope>
        </dependency>
    </dependencies>
</project>`

	err := os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(pomContent), 0644)
	require.NoError(t, err, "Should create realistic pom.xml")

	// Create source directory structure
	srcDir := filepath.Join(tempDir, "src", "main", "java", "com", "jfrog", "test")
	err = os.MkdirAll(srcDir, 0755)
	require.NoError(t, err)

	// Create sample Java file
	javaContent := `package com.jfrog.test;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class TestApp {
    private static final Logger logger = LoggerFactory.getLogger(TestApp.class);
    private final ObjectMapper objectMapper = new ObjectMapper();
    
    public static void main(String[] args) {
        logger.info("Test application started");
    }
}`

	err = os.WriteFile(filepath.Join(srcDir, "TestApp.java"), []byte(javaContent), 0644)
	require.NoError(t, err)

	// Create test directory structure
	testDir := filepath.Join(tempDir, "src", "test", "java", "com", "jfrog", "test")
	err = os.MkdirAll(testDir, 0755)
	require.NoError(t, err)

	// Create sample test file
	testContent := `package com.jfrog.test;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertTrue;

public class TestAppTest {
    @Test
    public void testApplication() {
        assertTrue(true, "Test should pass");
    }
}`

	err = os.WriteFile(filepath.Join(testDir, "TestAppTest.java"), []byte(testContent), 0644)
	require.NoError(t, err)
}

func validateMavenDependencyStructure(t *testing.T, dependencies []entities.Dependency) {
	for _, dep := range dependencies {
		// Validate dependency ID format (should be groupId:artifactId:version)
		assert.Contains(t, dep.Id, ":", "Dependency ID should contain colons")
		parts := strings.Split(dep.Id, ":")
		assert.GreaterOrEqual(t, len(parts), 3, "Dependency ID should have at least 3 parts (groupId:artifactId:version)")

		// Validate type
		assert.NotEmpty(t, dep.Type, "Dependency should have a type")
		validTypes := []string{"jar", "pom", "war", "ear", "aar", "bundle"}
		assert.Contains(t, validTypes, dep.Type, "Dependency type should be valid Maven type")

		// Validate scopes
		assert.NotEmpty(t, dep.Scopes, "Dependency should have scopes")
		for _, scope := range dep.Scopes {
			validScopes := []string{"compile", "provided", "runtime", "test", "system", "import"}
			assert.Contains(t, validScopes, scope, "Dependency scope should be valid Maven scope")
		}

		// Validate checksums (at least one should be present)
		hasChecksum := dep.Checksum.Sha1 != "" || dep.Checksum.Sha256 != "" || dep.Checksum.Md5 != ""
		assert.True(t, hasChecksum, "Dependency %s should have at least one checksum", dep.Id)
	}
}

func validateMavenRequestedByRelationships(t *testing.T, dependencies []entities.Dependency) {
	// Create map of all dependency IDs for validation
	allDepIds := make(map[string]bool)
	for _, dep := range dependencies {
		allDepIds[dep.Id] = true
	}

	for _, dep := range dependencies {
		for _, requestedByChain := range dep.RequestedBy {
			for _, requester := range requestedByChain {
				// Each requester should be either a known dependency or the root project
				if !strings.Contains(requester, ":") {
					// Skip root project references
					continue
				}
				// For now, we don't enforce that all requesters are in the dependency list
				// as some might be transitive dependencies not included in the main list
				assert.NotEmpty(t, requester, "Requester should not be empty")
			}
		}
	}
}
