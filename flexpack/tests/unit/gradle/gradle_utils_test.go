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

// TestGradleWrapperDetection tests that projects with gradlew wrappers are handled correctly
func TestGradleWrapperDetection(t *testing.T) {
	tests := []struct {
		name       string
		wrapperFile string
		content    string
	}{
		{"unix wrapper", "gradlew", "#!/bin/bash\necho 'gradlew'"},
		{"windows wrapper", "gradlew.bat", "@echo off\necho Gradle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
			require.NoError(t, err)

			err = os.WriteFile(filepath.Join(tempDir, tt.wrapperFile), []byte(tt.content), 0755)
			require.NoError(t, err)

			config := flexpack.GradleConfig{WorkingDirectory: tempDir}
			gf, err := gradleflexpack.NewGradleFlexPack(config)
			require.NoError(t, err)
			assert.NotNil(t, gf)
		})
	}
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

// TestCommandTimeout tests default and custom timeout configurations
func TestCommandTimeout(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	t.Run("default timeout", func(t *testing.T) {
		config := flexpack.GradleConfig{WorkingDirectory: tempDir}
		gf, err := gradleflexpack.NewGradleFlexPack(config)
		require.NoError(t, err)
		assert.NotNil(t, gf)
	})

	t.Run("custom timeout", func(t *testing.T) {
		config := flexpack.GradleConfig{
			WorkingDirectory: tempDir,
			CommandTimeout:   30 * time.Second,
		}
		gf, err := gradleflexpack.NewGradleFlexPack(config)
		require.NoError(t, err)
		assert.NotNil(t, gf)
	})
}

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

// TestMinimalOrEmptyBuildGradle tests handling of minimal content build files
func TestMinimalOrEmptyBuildGradle(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty file", ""},
		{"only comments", "// This is a comment\n/* Multi-line comment */\n"},
		{"only whitespace", "   \n\n   \t\n  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(tt.content), 0644)
			require.NoError(t, err)

			config := flexpack.GradleConfig{WorkingDirectory: tempDir}
			gf, err := gradleflexpack.NewGradleFlexPack(config)
			require.NoError(t, err)

			buildInfo, err := gf.CollectBuildInfo("minimal-test", "1")
			require.NoError(t, err)
			assert.NotNil(t, buildInfo)
		})
	}
}
