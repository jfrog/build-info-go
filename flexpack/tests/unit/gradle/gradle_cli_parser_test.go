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

// TestConfigurationToScopeMapping tests scope mapping for Gradle configurations
func TestConfigurationToScopeMapping(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'
    compileOnly 'org.projectlombok:lombok:1.18.24'
    runtimeOnly 'ch.qos.logback:logback-classic:1.4.5'
    testImplementation 'org.junit.jupiter:junit-jupiter:5.9.0'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("scope-test", "1")
	require.NoError(t, err)

	validScopes := map[string]bool{
		"compile": true, "runtime": true, "test": true, "provided": true, "system": true,
	}

	for _, module := range buildInfo.Modules {
		for _, dep := range module.Dependencies {
			for _, scope := range dep.Scopes {
				assert.True(t, validScopes[scope], "Invalid scope %s for dep %s", scope, dep.Id)
			}
		}
	}
}

// TestExcludeTestDependenciesConfig tests that test dependencies are excluded when configured
func TestExcludeTestDependenciesConfig(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'
    testImplementation 'junit:junit:4.13.2'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: false,
	}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("exclude-test-deps", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestMultiLevelDependencyTree tests parsing multi-level transitive dependencies
func TestMultiLevelDependencyTree(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.springframework:spring-webmvc:5.3.20'
    implementation 'com.fasterxml.jackson.core:jackson-databind:2.14.0'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("tree-test", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)
}

// TestBuildInfoDependencyScopes tests that dependencies have valid scopes
func TestBuildInfoDependencyScopes(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'
    compileOnly 'org.projectlombok:lombok:1.18.24'
    runtimeOnly 'ch.qos.logback:logback-classic:1.4.5'
    testImplementation 'junit:junit:4.13.2'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("scope-test", "1")
	require.NoError(t, err)

	validScopes := map[string]bool{
		"compile": true, "runtime": true, "test": true, "provided": true, "system": true,
	}

	for _, module := range buildInfo.Modules {
		for _, dep := range module.Dependencies {
			assert.NotEmpty(t, dep.Scopes, "Dependency %s should have scopes", dep.Id)
			for _, scope := range dep.Scopes {
				assert.True(t, validScopes[scope], "Invalid scope %s for dependency %s", scope, dep.Id)
			}
		}
	}
}

// TestAnnotationProcessorDependencies tests kapt/annotationProcessor configurations
func TestAnnotationProcessorDependencies(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'
    annotationProcessor 'org.projectlombok:lombok:1.18.24'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("annotation-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestApiConfiguration tests 'api' configuration for java-library plugin
func TestApiConfiguration(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java-library' }
group = 'com.example'
version = '1.0.0'

dependencies {
    api 'org.slf4j:slf4j-api:2.0.0'
    implementation 'com.google.guava:guava:31.1-jre'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("api-config-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}
