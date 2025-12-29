package flexpack_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/flexpack"
	gradleflexpack "github.com/jfrog/build-info-go/flexpack/gradle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildInfoDependencyChecksums tests checksum fields on dependencies
func TestBuildInfoDependencyChecksums(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()
	setupMinimalGradleProjectForArtifacts(t, tempDir)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("checksum-test", "1")
	require.NoError(t, err)

	if len(buildInfo.Modules) > 0 && len(buildInfo.Modules[0].Dependencies) > 0 {
		dep := buildInfo.Modules[0].Dependencies[0]
		assert.NotNil(t, dep.Checksum, "Dependency should have checksum structure")

		if dep.Sha1 != "" {
			assert.Len(t, dep.Sha1, 40, "SHA1 should be 40 characters")
		}
		if dep.Sha256 != "" {
			assert.Len(t, dep.Sha256, 64, "SHA256 should be 64 characters")
		}
		if dep.Md5 != "" {
			assert.Len(t, dep.Md5, 32, "MD5 should be 32 characters")
		}
	}
}

// TestBuildInfoDependencyStructure tests dependency structure when present
func TestBuildInfoDependencyStructure(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.7'
    implementation 'com.google.guava:guava:31.1-jre'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("dependency-structure-test", "1")
	require.NoError(t, err)

	if len(buildInfo.Modules) > 0 && len(buildInfo.Modules[0].Dependencies) > 0 {
		dep := buildInfo.Modules[0].Dependencies[0]
		assert.NotEmpty(t, dep.Id, "Dependency should have an ID")
		assert.NotEmpty(t, dep.Type, "Dependency should have a type")
		assert.NotEmpty(t, dep.Scopes, "Dependency should have scopes")
		assert.NotNil(t, dep.Checksum, "Dependency should have checksum structure")
	}
}

// TestDependencyWithClassifier tests dependencies with classifier
func TestDependencyWithClassifier(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'
    implementation 'com.google.guava:guava:31.1-jre:sources'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("classifier-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestIvyAndMavenPublishToLocal ensures the init script wiring publishes Maven and Ivy artifacts to local/file repos
// and the manifest is generated without errors.
func TestIvyAndMavenPublishToLocal(t *testing.T) {
	skipIfGradleInvalid(t)

	tempDir := t.TempDir()
	setupGradleProjectWithPublishing(t, tempDir)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	// Indicate this is a publish command so artifacts are collected
	gf.SetWasPublishCommand(true)

	buildInfo, err := gf.CollectBuildInfo("local-publish-test", "1")
	require.NoError(t, err)
	require.NotNil(t, buildInfo)

	// Manifest-driven artifacts should be present for the root module
	if len(buildInfo.Modules) > 0 {
		assert.NotEmpty(t, buildInfo.Modules[0].Artifacts, "expected artifacts from local Maven/Ivy publish")
	}
}

// Helper function for artifact tests
func setupMinimalGradleProjectForArtifacts(t *testing.T, tempDir string) {
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

func setupGradleProjectWithPublishing(t *testing.T, tempDir string) {
	buildGradleContent := `
plugins {
    id 'java'
    id 'maven-publish'
    id 'ivy-publish'
}

group = 'com.jfrog.test'
version = '1.0.0'

publishing {
    repositories {
        maven {
            url = layout.buildDirectory.dir("repo-maven")
        }
        ivy {
            url = layout.buildDirectory.dir("repo-ivy")
        }
    }
    publications {
        mavenJava(MavenPublication) {
            from components.java
        }
        ivyJava(IvyPublication) {
            from components.java
        }
    }
}
`

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradleContent), 0644)
	require.NoError(t, err, "Should create publishing build.gradle")

	settings := `rootProject.name = "pub-project"`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err, "Should create settings.gradle")

	// Minimal Java source so components.java exists
	srcDir := filepath.Join(tempDir, "src", "main", "java", "com", "jfrog", "test")
	err = os.MkdirAll(srcDir, 0755)
	require.NoError(t, err, "Should create source directory")

	javaFile := `
package com.jfrog.test;
public class App {
    public static void main(String[] args) { System.out.println("ok"); }
}`
	err = os.WriteFile(filepath.Join(srcDir, "App.java"), []byte(javaFile), 0644)
	require.NoError(t, err, "Should create Java source")
}
