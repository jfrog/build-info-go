package flexpack

import (
	"os"
	"path/filepath"
	"testing"

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

	config := MavenConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}

	mavenFlex, err := NewMavenFlexPack(config)
	require.NoError(t, err, "Should create Maven FlexPack successfully")

	assert.Equal(t, "com.jfrog.test", mavenFlex.groupId, "GroupId should match")
	assert.Equal(t, "test-maven-project", mavenFlex.artifactId, "ArtifactId should match")
	assert.Equal(t, "1.0.0", mavenFlex.projectVersion, "Version should match")
	assert.Equal(t, "com.jfrog.test:test-maven-project", mavenFlex.projectName, "Project name should match")
}

// TestMavenFlexPackInterface tests that Maven FlexPack implements required interfaces
func TestMavenFlexPackInterface(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test FlexPackManager interface methods
	var _ FlexPackManager = mavenFlex

	// Test BuildInfoCollector interface methods
	var _ BuildInfoCollector = mavenFlex

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

	config := MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := NewMavenFlexPack(config)
	require.NoError(t, err)

	// Validate parsed POM data
	assert.Equal(t, "com.example", mavenFlex.groupId)
	assert.Equal(t, "complex-project", mavenFlex.artifactId)
	assert.Equal(t, "2.0.0", mavenFlex.projectVersion)
	assert.NotNil(t, mavenFlex.pomData)
	assert.Equal(t, "Complex Test Project", mavenFlex.pomData.Name)
	assert.Equal(t, "Test project with dependencies", mavenFlex.pomData.Description)
	assert.Len(t, mavenFlex.pomData.Dependencies.Dependency, 3, "Should parse all 3 dependencies")

	// Validate specific dependencies
	deps := mavenFlex.pomData.Dependencies.Dependency
	jacksonDep := findDependency(deps, "jackson-core")
	require.NotNil(t, jacksonDep, "Should find jackson-core dependency")
	assert.Equal(t, "com.fasterxml.jackson.core", jacksonDep.GroupId)
	assert.Equal(t, "2.15.2", jacksonDep.Version)

	junitDep := findDependency(deps, "junit-jupiter")
	require.NotNil(t, junitDep, "Should find junit-jupiter dependency")
	assert.Equal(t, "test", junitDep.Scope)
}

// TestMavenDependencyParsing tests dependency parsing functionality
func TestMavenDependencyParsing(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := NewMavenFlexPack(config)
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

	config := MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := NewMavenFlexPack(config)
	require.NoError(t, err)

	// Create a sample dependency for checksum calculation
	testDep := DependencyInfo{
		ID:      "com.example:test-dep:1.0.0",
		Name:    "com.example:test-dep",
		Version: "1.0.0",
		Type:    "jar",
		Scopes:  []string{"compile"},
	}

	// Test manifest checksum calculation (fallback when file not found)
	sha1, sha256, md5, err := mavenFlex.calculateManifestChecksum(testDep)
	require.NoError(t, err, "Should calculate manifest checksum successfully")

	assert.NotEmpty(t, sha1, "SHA1 should not be empty")
	assert.NotEmpty(t, sha256, "SHA256 should not be empty")
	assert.NotEmpty(t, md5, "MD5 should not be empty")

	// Verify checksums are consistent
	sha1_2, sha256_2, md5_2, err := mavenFlex.calculateManifestChecksum(testDep)
	require.NoError(t, err)
	assert.Equal(t, sha1, sha1_2, "SHA1 should be deterministic")
	assert.Equal(t, sha256, sha256_2, "SHA256 should be deterministic")
	assert.Equal(t, md5, md5_2, "MD5 should be deterministic")
}

// TestMavenScopeValidation tests scope validation and normalization
func TestMavenScopeValidation(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalMavenProject(t, tempDir)

	config := MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test valid scopes
	validScopes := []string{"compile", "test", "runtime", "provided"}
	normalized := mavenFlex.validateAndNormalizeScopes(validScopes)
	assert.Equal(t, validScopes, normalized, "Valid scopes should remain unchanged")

	// Test invalid scopes (should be filtered out)
	mixedScopes := []string{"compile", "invalid-scope", "test", "", "runtime"}
	normalized = mavenFlex.validateAndNormalizeScopes(mixedScopes)
	expected := []string{"compile", "test", "runtime"}
	assert.Equal(t, expected, normalized, "Invalid scopes should be filtered out")

	// Test empty scopes
	emptyScopes := []string{}
	normalized = mavenFlex.validateAndNormalizeScopes(emptyScopes)
	assert.Equal(t, []string{"compile"}, normalized, "Empty scopes should default to compile")
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

	config := MavenConfig{WorkingDirectory: tempDir}
	mavenFlex, err := NewMavenFlexPack(config)
	require.NoError(t, err)

	// Test repository detection
	repo := mavenFlex.getDeploymentRepository()
	assert.Equal(t, "maven-release-local", repo, "Should detect release repository for non-SNAPSHOT version")

	// Test with SNAPSHOT version
	mavenFlex.projectVersion = "1.0.0-SNAPSHOT"
	repo = mavenFlex.getDeploymentRepository()
	assert.Equal(t, "maven-snapshot-local", repo, "Should detect snapshot repository for SNAPSHOT version")
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

func findDependency(deps []MavenDependency, artifactId string) *MavenDependency {
	for _, dep := range deps {
		if dep.ArtifactId == artifactId {
			return &dep
		}
	}
	return nil
}
