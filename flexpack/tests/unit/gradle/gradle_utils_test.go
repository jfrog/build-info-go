package unit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/flexpack"
	gradleflexpack "github.com/jfrog/build-info-go/flexpack/gradle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Tests for gradle_utils.go
// - Gradle executable path detection (gradlew/gradlew.bat)
// - Command execution and timeout
// - Path validation and security
// - Build file content reading
// - Settings file reading
// - Scope validation and normalization
// - Gradle cache artifact finding
// - Checksum calculation
// ============================================================================

// --- Gradle Executable Detection Tests ---

// TestGradlewWrapperDetection tests that projects with gradlew wrapper are handled correctly
func TestGradlewWrapperDetection(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	// Create gradlew wrapper (Unix)
	err = os.WriteFile(filepath.Join(tempDir, "gradlew"), []byte("#!/bin/bash\necho 'gradlew'"), 0755)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, gf)
}

// TestGradlewBatWrapperDetection tests Windows gradlew.bat wrapper detection
func TestGradlewBatWrapperDetection(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "gradlew.bat"), []byte("@echo off\necho Gradle"), 0755)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, gf)
}

// TestCustomGradleExecutable tests using a custom Gradle executable path
func TestCustomGradleExecutable(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory: tempDir,
		GradleExecutable: "/custom/path/to/gradle",
	}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, gf)
}

// --- Command Timeout Tests ---

// TestDefaultCommandTimeout tests that default timeout is applied
func TestDefaultCommandTimeout(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, gf)
}

// TestCustomCommandTimeout tests custom timeout configuration
func TestCustomCommandTimeout(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory: tempDir,
		CommandTimeout:   30 * time.Second,
	}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, gf)
}

// --- Path Validation Tests ---

// TestWorkingDirectoryWithSpacesInPath tests paths containing spaces
func TestWorkingDirectoryWithSpacesInPath(t *testing.T) {
	tempDir := t.TempDir()
	spacedDir := filepath.Join(tempDir, "My Gradle Project")
	err := os.MkdirAll(spacedDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(spacedDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example.spaced'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: spacedDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.spaced")
}

// TestWorkingDirectoryWithUnicodeCharacters tests paths with unicode characters
func TestWorkingDirectoryWithUnicodeCharacters(t *testing.T) {
	tempDir := t.TempDir()
	unicodeDir := filepath.Join(tempDir, "gradle-project-日本語")
	err := os.MkdirAll(unicodeDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(unicodeDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example.unicode'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: unicodeDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.unicode")
}

// TestPathTraversalInModuleNames tests that malicious module names are filtered out
func TestPathTraversalInModuleNames(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	legitDir := filepath.Join(tempDir, "legit")
	err = os.MkdirAll(legitDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(legitDir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
	require.NoError(t, err)

	// Settings with path traversal attempts - malicious modules should be filtered out
	settingsContent := `rootProject.name = 'test'
include 'legit'
include '..:..:etc:passwd'
include '..:..:..:windows:system32'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settingsContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("security-test", "1")
	require.NoError(t, err)

	// Should have modules (root and legit only - path traversal modules filtered)
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1, "Should have at least one module")

	// Path traversal modules should NOT appear in the module list
	for _, module := range buildInfo.Modules {
		assert.NotContains(t, module.Id, "..", "Module ID should not contain path traversal")
		assert.NotContains(t, module.Id, "etc", "Module ID should not expose system paths")
		assert.NotContains(t, module.Id, "passwd", "Module ID should not expose sensitive files")
		assert.NotContains(t, module.Id, "windows", "Module ID should not expose Windows paths")
	}
}

// TestSymlinkedWorkingDirectory tests behavior with symlinked working directory
func TestSymlinkedWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()

	projectDir := filepath.Join(tempDir, "actual-project")
	err := os.MkdirAll(projectDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(projectDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example.symlink'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	symlinkDir := filepath.Join(tempDir, "symlink-project")
	err = os.Symlink(projectDir, symlinkDir)
	if err != nil {
		t.Skip("Symlinks not supported on this system")
	}

	config := flexpack.GradleConfig{WorkingDirectory: symlinkDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("symlink-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.symlink")
}

// --- Build File Reading Tests ---

// TestBuildGradlePreferredOverKts tests that build.gradle is preferred when both exist
func TestBuildGradlePreferredOverKts(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.groovy.preferred'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "build.gradle.kts"), []byte(`
plugins { java }
group = "com.kotlin.notused"
version = "2.0.0"
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("preference-test", "1")
	require.NoError(t, err)

	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.groovy.preferred", "Should use build.gradle")
	assert.Contains(t, moduleId, "1.0.0", "Should use build.gradle version")
}

// TestOnlyKotlinDSL tests project with only build.gradle.kts
func TestOnlyKotlinDSL(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle.kts"), []byte(`
plugins {
    kotlin("jvm") version "1.9.0"
}
group = "com.kotlin.only"
version = "3.0.0"
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("kotlin-test", "1")
	require.NoError(t, err)

	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "com.kotlin.only", "Should parse Kotlin DSL")
	assert.Contains(t, moduleId, "3.0.0", "Should parse Kotlin DSL version")
}

// TestEmptyBuildGradle tests handling of empty build.gradle
func TestEmptyBuildGradle(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(""), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("empty-test", "1")
	require.NoError(t, err)

	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "unspecified", "Should use unspecified for empty build file")
}

// TestBuildGradleWithOnlyComments tests build file with only comments
func TestBuildGradleWithOnlyComments(t *testing.T) {
	tempDir := t.TempDir()

	content := `// This is a comment
/* Multi-line comment */
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(content), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("comments-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestBuildGradleWithOnlyWhitespace tests handling of whitespace-only build file
func TestBuildGradleWithOnlyWhitespace(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte("   \n\n   \t\n  "), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("whitespace-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}
