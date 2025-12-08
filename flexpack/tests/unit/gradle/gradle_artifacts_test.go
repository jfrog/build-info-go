package unit

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

		if dep.Checksum.Sha1 != "" {
			assert.Len(t, dep.Checksum.Sha1, 40, "SHA1 should be 40 characters")
		}
		if dep.Checksum.Sha256 != "" {
			assert.Len(t, dep.Checksum.Sha256, 64, "SHA256 should be 64 characters")
		}
		if dep.Checksum.Md5 != "" {
			assert.Len(t, dep.Checksum.Md5, 32, "MD5 should be 32 characters")
		}
	}
}

// TestBuildInfoDependencyStructure tests dependency structure when present
func TestBuildInfoDependencyStructure(t *testing.T) {
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
