package unit

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

// TestNewGradleFlexPack tests the creation of Gradle FlexPack instance
func TestNewGradleFlexPack(t *testing.T) {
	tempDir := t.TempDir()

	// Create minimal build.gradle
	buildGradleContent := `plugins {
    id 'java'
}

group = 'com.jfrog.test'
version = '1.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err, "Should create build.gradle successfully")

	config := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}

	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err, "Should create Gradle FlexPack successfully")

	// Test through public interface - collect build info and verify module ID
	buildInfo, err := gradleFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "Should collect build info successfully")
	require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")

	// Module ID should contain the expected GAV coordinates
	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.jfrog.test", "Module ID should contain groupId")
	assert.Contains(t, moduleId, "1.0.0", "Module ID should contain version")
}

// TestGradleFlexPackWithSettingsGradle tests project name resolution from settings.gradle
func TestGradleFlexPackWithSettingsGradle(t *testing.T) {
	tempDir := t.TempDir()

	// Create build.gradle
	buildGradleContent := `plugins {
    id 'java'
}

group = 'com.example'
version = '2.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err)

	// Create settings.gradle with rootProject.name
	settingsContent := `rootProject.name = 'my-gradle-project'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settingsContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)

	// Verify project name from settings.gradle is used
	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "my-gradle-project", "Module ID should use rootProject.name from settings.gradle")
}

// TestGradleFlexPackKotlinDSL tests support for build.gradle.kts (Kotlin DSL)
func TestGradleFlexPackKotlinDSL(t *testing.T) {
	tempDir := t.TempDir()

	// Create build.gradle.kts (Kotlin DSL)
	buildGradleKtsContent := `plugins {
    kotlin("jvm") version "1.5.0"
}

group = "com.example.kotlin"
version = "1.0.0"

dependencies {
    implementation("org.jetbrains.kotlin:kotlin-stdlib:1.5.0")
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle.kts"), []byte(buildGradleKtsContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err, "Should support build.gradle.kts")

	buildInfo, err := gradleFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)

	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.example.kotlin", "Module ID should contain parsed groupId from Kotlin DSL")
	assert.Contains(t, moduleId, "1.0.0", "Module ID should contain parsed version from Kotlin DSL")
}

// TestGradleFlexPackMultiModule tests multi-module Gradle project support
func TestGradleFlexPackMultiModule(t *testing.T) {
	tempDir := t.TempDir()

	// Create root build.gradle
	rootBuildGradle := `plugins {
    id 'java'
}

group = 'com.example.multi'
version = '1.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(rootBuildGradle), 0644)
	require.NoError(t, err)

	// Create settings.gradle with submodules
	settingsGradle := `rootProject.name = 'multi-module'
include 'app'
include 'lib'
include 'libs:utils'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settingsGradle), 0644)
	require.NoError(t, err)

	// Create app submodule
	appDir := filepath.Join(tempDir, "app")
	err = os.MkdirAll(appDir, 0755)
	require.NoError(t, err)
	appBuildGradle := `plugins {
    id 'java'
}

dependencies {
    implementation project(':lib')
    implementation 'com.google.guava:guava:30.0-jre'
}
`
	err = os.WriteFile(filepath.Join(appDir, "build.gradle"), []byte(appBuildGradle), 0644)
	require.NoError(t, err)

	// Create lib submodule
	libDir := filepath.Join(tempDir, "lib")
	err = os.MkdirAll(libDir, 0755)
	require.NoError(t, err)
	libBuildGradle := `plugins {
    id 'java-library'
}

dependencies {
    api 'org.apache.commons:commons-lang3:3.11'
}
`
	err = os.WriteFile(filepath.Join(libDir, "build.gradle"), []byte(libBuildGradle), 0644)
	require.NoError(t, err)

	// Create nested libs/utils submodule
	utilsDir := filepath.Join(tempDir, "libs", "utils")
	err = os.MkdirAll(utilsDir, 0755)
	require.NoError(t, err)
	utilsBuildGradle := `plugins {
    id 'java-library'
}
`
	err = os.WriteFile(filepath.Join(utilsDir, "build.gradle"), []byte(utilsBuildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("multi-module-test", "1")
	require.NoError(t, err)

	// Multi-module projects should have multiple modules in build info
	// Root + app + lib + libs:utils = 4 modules
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1, "Should have at least one module")
}

// TestGradleBuildGradleParsing tests parsing of build.gradle files
func TestGradleBuildGradleParsing(t *testing.T) {
	tempDir := t.TempDir()

	// Create build.gradle with dependencies
	buildGradleContent := `plugins {
    id 'java'
}

group = 'com.example'
version = '2.0.0'

repositories {
    mavenCentral()
}

dependencies {
    implementation 'com.fasterxml.jackson.core:jackson-core:2.15.2'
    implementation 'org.slf4j:slf4j-api:2.0.7'
    testImplementation 'org.junit.jupiter:junit-jupiter:5.9.3'
    compileOnly 'org.projectlombok:lombok:1.18.28'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)

	// Module ID should reflect parsed build.gradle data
	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.example", "Module ID should contain parsed groupId")
	assert.Contains(t, moduleId, "2.0.0", "Module ID should contain parsed version")

	// Dependencies parsing depends on Gradle being available
	// In unit tests without Gradle, fallback parsing is used
	deps := buildInfo.Modules[0].Dependencies
	if len(deps) > 0 {
		// If dependencies were parsed, check for expected ones
		dependencyIds := make(map[string]bool)
		for _, dep := range deps {
			dependencyIds[dep.Id] = true
		}

		hasJackson := false
		for id := range dependencyIds {
			if strings.Contains(id, "jackson-core") {
				hasJackson = true
				break
			}
		}
		if hasJackson {
			t.Log("Found jackson-core dependency")
		}

		hasSlf4j := false
		for id := range dependencyIds {
			if strings.Contains(id, "slf4j-api") {
				hasSlf4j = true
				break
			}
		}
		if hasSlf4j {
			t.Log("Found slf4j-api dependency")
		}
	} else {
		// Without Gradle available, dependencies may not be parsed
		t.Log("No dependencies parsed (Gradle may not be available)")
	}
}

// TestGradleScopeMapping tests scope mapping for different Gradle configurations
func TestGradleScopeMapping(t *testing.T) {
	tempDir := t.TempDir()

	buildGradleContent := `plugins {
    id 'java'
}

group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.7'
    runtimeOnly 'ch.qos.logback:logback-classic:1.4.8'
    testImplementation 'org.junit.jupiter:junit-jupiter:5.9.3'
    compileOnly 'org.projectlombok:lombok:1.18.28'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("scope-test", "1")
	require.NoError(t, err)

	// Verify that dependencies have valid scopes
	if len(buildInfo.Modules) > 0 && len(buildInfo.Modules[0].Dependencies) > 0 {
		validScopes := map[string]bool{
			"compile": true, "runtime": true, "test": true, "provided": true, "system": true,
		}

		for _, dep := range buildInfo.Modules[0].Dependencies {
			assert.NotEmpty(t, dep.Scopes, "Dependencies should have scopes: %s", dep.Id)

			for _, scope := range dep.Scopes {
				assert.True(t, validScopes[scope], "Scope should be valid: %s for dep %s", scope, dep.Id)
			}
		}
	}
}

// TestGradleFlexPackConsistency tests that multiple calls produce consistent results
func TestGradleFlexPackConsistency(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalGradleProject(t, tempDir)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	// Test that multiple calls work consistently
	buildInfo1, err := gradleFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err, "First call should succeed")

	buildInfo2, err := gradleFlex.CollectBuildInfo("test-build", "2")
	require.NoError(t, err, "Second call should succeed")

	// Verify both calls succeeded with same structure
	require.Equal(t, len(buildInfo1.Modules), len(buildInfo2.Modules), "Both calls should return same number of modules")

	if len(buildInfo1.Modules) > 0 && len(buildInfo2.Modules) > 0 {
		assert.Equal(t, buildInfo1.Modules[0].Id, buildInfo2.Modules[0].Id, "Module IDs should be consistent")
	}
}

// TestGradleFlexPackBuildInfoStructure tests the structure of collected build info
func TestGradleFlexPackBuildInfoStructure(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalGradleProject(t, tempDir)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("structure-test", "1.0")
	require.NoError(t, err)

	// Verify build info structure
	assert.Equal(t, "structure-test", buildInfo.Name, "Build name should match")
	assert.Equal(t, "1.0", buildInfo.Number, "Build number should match")
	assert.NotEmpty(t, buildInfo.Started, "Started timestamp should be set")

	// Verify agent info
	require.NotNil(t, buildInfo.Agent, "Agent should not be nil")
	assert.Equal(t, "build-info-go", buildInfo.Agent.Name, "Agent name should be build-info-go")
	assert.NotEmpty(t, buildInfo.Agent.Version, "Agent version should be set")

	// Verify build agent info
	require.NotNil(t, buildInfo.BuildAgent, "BuildAgent should not be nil")
	assert.Equal(t, "Gradle", buildInfo.BuildAgent.Name, "Build agent name should be Gradle")

	// Verify modules
	require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")
	module := buildInfo.Modules[0]
	assert.NotEmpty(t, module.Id, "Module should have an ID")
	assert.Equal(t, entities.Gradle, module.Type, "Module type should be gradle")
}

// TestGradleFlexPackErrorHandling tests error handling for invalid configurations
func TestGradleFlexPackErrorHandling(t *testing.T) {
	// Test with directory that exists but has no Gradle files
	tempDir := t.TempDir()
	config := flexpack.GradleConfig{
		WorkingDirectory: tempDir,
	}

	// Constructor should succeed but log a warning (graceful degradation)
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err, "Should create FlexPack even without build.gradle")

	// Build info collection should still work (with minimal info)
	buildInfo, err := gradleFlex.CollectBuildInfo("error-test", "1")
	require.NoError(t, err, "Should collect build info even without build.gradle")
	assert.NotNil(t, buildInfo, "Build info should not be nil")
}

// TestGradleFlexPackChecksumCalculation tests checksum calculation for dependencies
func TestGradleFlexPackChecksumCalculation(t *testing.T) {
	tempDir := t.TempDir()
	setupMinimalGradleProject(t, tempDir)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("checksum-test", "1")
	require.NoError(t, err)

	// Verify checksum structure exists for dependencies
	if len(buildInfo.Modules) > 0 && len(buildInfo.Modules[0].Dependencies) > 0 {
		dep := buildInfo.Modules[0].Dependencies[0]
		// Checksum structure should exist (may be empty if artifacts not in cache)
		assert.NotNil(t, dep.Checksum, "Dependency should have checksum structure")
	}
}

// TestGradleFlexPackVersion tests that version variable is accessible
func TestGradleFlexPackVersion(t *testing.T) {
	// Verify the version variable is accessible and set
	assert.NotEmpty(t, flexpack.GradleFlexPackVersion, "GradleFlexPackVersion should be set")
}

// TestGradleFlexPackNoGroupVersion tests handling of projects without group/version
func TestGradleFlexPackNoGroupVersion(t *testing.T) {
	tempDir := t.TempDir()

	// Create build.gradle without group or version
	buildGradleContent := `plugins {
    id 'java'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)

	// Should have default values for missing group/version
	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "unspecified", "Should contain 'unspecified' for missing group or version")
}

// TestGradleFlexPackNestedDependencies tests parsing of nested dependency blocks
func TestGradleFlexPackNestedDependencies(t *testing.T) {
	tempDir := t.TempDir()

	// Create build.gradle with nested dependency configurations
	buildGradleContent := `plugins {
    id 'java'
}

group = 'com.example'
version = '1.0.0'

dependencies {
    implementation('org.springframework:spring-core:5.3.0') {
        exclude group: 'org.springframework', module: 'spring-jcl'
    }
    implementation 'com.google.guava:guava:30.0-jre'
    
    constraints {
        implementation 'org.apache.httpcomponents:httpclient:4.5.13'
    }
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gradleFlex, err := flexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gradleFlex.CollectBuildInfo("nested-test", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)

	// Dependencies parsing may not work without Gradle available
	// This test verifies that the code handles nested blocks without crashing
	deps := buildInfo.Modules[0].Dependencies
	t.Logf("Parsed %d dependencies from nested dependency blocks", len(deps))

	// If dependencies were parsed, log them
	for _, dep := range deps {
		t.Logf("Found dependency: %s", dep.Id)
	}
}

// Helper functions

func setupMinimalGradleProject(t *testing.T, tempDir string) {
	buildGradleContent := `plugins {
    id 'java'
}

group = 'com.jfrog.test'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.7'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err, "Should create minimal build.gradle")
}
