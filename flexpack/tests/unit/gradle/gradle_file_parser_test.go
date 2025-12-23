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

// TestParseMetadata tests parsing of group, artifact, and version from build.gradle
func TestParseMetadata(t *testing.T) {
	tests := []struct {
		name         string
		buildContent string
		expectedInId string
		description  string
	}{
		{
			name: "single quotes",
			buildContent: `plugins { id 'java' }
group = 'com.example.single'
version = '1.0.0'`,
			expectedInId: "com.example.single",
		},
		{
			name: "double quotes",
			buildContent: `plugins { id 'java' }
group = "com.example.double"
version = "1.0.0"`,
			expectedInId: "com.example.double",
		},
		{
			name: "missing group defaults to unspecified",
			buildContent: `plugins { id 'java' }
version = '1.0.0'`,
			expectedInId: "unspecified",
		},
		{
			name: "SNAPSHOT version",
			buildContent: `plugins { id 'java' }
group = 'com.example'
version = '2.0.0-SNAPSHOT'`,
			expectedInId: "2.0.0-SNAPSHOT",
		},
		{
			name: "RC version",
			buildContent: `plugins { id 'java' }
group = 'com.example'
version = '3.0.0-RC1'`,
			expectedInId: "3.0.0-RC1",
		},
		{
			name: "missing version defaults to unspecified",
			buildContent: `plugins { id 'java' }
group = 'com.example'`,
			expectedInId: "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(tt.buildContent), 0644)
			require.NoError(t, err)

			config := flexpack.GradleConfig{WorkingDirectory: tempDir}
			gf, err := gradleflexpack.NewGradleFlexPack(config)
			require.NoError(t, err)

			buildInfo, err := gf.CollectBuildInfo("metadata-test", "1")
			require.NoError(t, err)
			assert.Contains(t, buildInfo.Modules[0].Id, tt.expectedInId)
		})
	}
}

// TestDependencyNotations tests parsing dependencies with various notations
func TestDependencyNotations(t *testing.T) {
	tests := []struct {
		name         string
		buildContent string
	}{
		{
			name: "string notation",
			buildContent: `plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
dependencies {
    implementation 'com.google.guava:guava:31.1-jre'
    testImplementation 'junit:junit:4.13.2'
}`,
		},
		{
			name: "map notation",
			buildContent: `plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
dependencies {
    implementation group: 'com.google.code.gson', name: 'gson', version: '2.10'
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(tt.buildContent), 0644)
			require.NoError(t, err)

			config := flexpack.GradleConfig{
				WorkingDirectory:        tempDir,
				IncludeTestDependencies: true,
			}
			gf, err := gradleflexpack.NewGradleFlexPack(config)
			require.NoError(t, err)

			buildInfo, err := gf.CollectBuildInfo("dep-notation-test", "1")
			require.NoError(t, err)
			assert.NotNil(t, buildInfo)
		})
	}
}

// TestProjectDependencyParsing tests parsing project(':module') dependencies
func TestProjectDependencyParsing(t *testing.T) {
	tempDir := t.TempDir()

	rootBuild := `
plugins { id 'java' }
group = 'com.example.multi'
version = '1.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(rootBuild), 0644)
	require.NoError(t, err)

	appDir := filepath.Join(tempDir, "app")
	err = os.MkdirAll(appDir, 0755)
	require.NoError(t, err)

	libDir := filepath.Join(tempDir, "lib")
	err = os.MkdirAll(libDir, 0755)
	require.NoError(t, err)

	appBuild := `
plugins { id 'java' }
dependencies {
    implementation project(':lib')
}
`
	err = os.WriteFile(filepath.Join(appDir, "build.gradle"), []byte(appBuild), 0644)
	require.NoError(t, err)

	libBuild := `
plugins { id 'java-library' }
`
	err = os.WriteFile(filepath.Join(libDir, "build.gradle"), []byte(libBuild), 0644)
	require.NoError(t, err)

	settings := `rootProject.name = 'multi-module'
include 'app'
include 'lib'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("project-dep-test", "1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1, "Should have modules")
}

// TestNestedDependencyBlocks tests parsing with nested closures
func TestNestedDependencyBlocks(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation('org.springframework:spring-core:5.3.0') {
        exclude group: 'org.springframework', module: 'spring-jcl'
    }
    implementation 'com.google.guava:guava:31.1-jre'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("nested-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestDependencyBlockWithComments tests parsing dependencies with comment styles
func TestDependencyBlockWithComments(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    // Single line comment
    implementation 'org.slf4j:slf4j-api:2.0.0'
    /* Multi-line comment */
    implementation 'com.google.guava:guava:31.1-jre'
    // implementation 'commented:out:1.0.0'
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("comment-test", "1")
	require.NoError(t, err)

	for _, module := range buildInfo.Modules {
		for _, dep := range module.Dependencies {
			assert.NotContains(t, dep.Id, "commented", "Commented dependency should not be parsed")
		}
	}
}

// TestEmptyDependenciesBlock tests handling of empty dependencies block
func TestEmptyDependenciesBlock(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("empty-deps-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestKotlinDSLDependencies tests parsing Kotlin DSL dependency declarations
func TestKotlinDSLDependencies(t *testing.T) {
	tempDir := t.TempDir()

	buildGradleKts := `
plugins {
    kotlin("jvm") version "1.9.0"
}

group = "com.example.kotlin"
version = "1.0.0"

dependencies {
    implementation("com.google.guava:guava:31.1-jre")
    implementation("org.jetbrains.kotlin:kotlin-stdlib")
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle.kts"), []byte(buildGradleKts), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("kotlin-dep-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.kotlin")
}

// TestSettingsFileParsing tests parsing of settings.gradle and settings.gradle.kts
func TestSettingsFileParsing(t *testing.T) {
	tests := []struct {
		name            string
		settingsFile    string
		settingsContent string
		expectedInId    string
	}{
		{
			name:            "groovy settings file",
			settingsFile:    "settings.gradle",
			settingsContent: `rootProject.name = 'my-project-from-settings'`,
			expectedInId:    "my-project-from-settings",
		},
		{
			name:            "kotlin settings file",
			settingsFile:    "settings.gradle.kts",
			settingsContent: `rootProject.name = "kotlin-settings-project"`,
			expectedInId:    "kotlin-settings-project",
		},
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

			err = os.WriteFile(filepath.Join(tempDir, tt.settingsFile), []byte(tt.settingsContent), 0644)
			require.NoError(t, err)

			config := flexpack.GradleConfig{WorkingDirectory: tempDir}
			gf, err := gradleflexpack.NewGradleFlexPack(config)
			require.NoError(t, err)

			buildInfo, err := gf.CollectBuildInfo("settings-test", "1")
			require.NoError(t, err)
			assert.Contains(t, buildInfo.Modules[0].Id, tt.expectedInId)
		})
	}
}

// TestIncludeStatementParsing tests parsing of include statements
func TestIncludeStatementParsing(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
	require.NoError(t, err)

	modules := []string{"app", "lib", "core"}
	for _, m := range modules {
		dir := filepath.Join(tempDir, m)
		err = os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
		require.NoError(t, err)
	}

	settings := `rootProject.name = 'include-test'
include 'app'
include "lib"
include 'core'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("include-test", "1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1, "Should have modules")
}

// TestCommentHandling tests that comments are properly ignored in settings.gradle
func TestCommentHandling(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	appDir := filepath.Join(tempDir, "app")
	err = os.MkdirAll(appDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(appDir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
	require.NoError(t, err)

	// Settings file with single-line and multi-line comments
	settings := `// Root project name
rootProject.name = 'comment-test'

/* This is a multi-line comment */
include 'app'

/* Block commented module:
include 'block-commented-module'
*/

// include 'line-commented-module'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("comment-test", "1")
	require.NoError(t, err)

	// Verify commented modules are NOT included
	for _, module := range buildInfo.Modules {
		assert.NotContains(t, module.Id, "block-commented-module")
		assert.NotContains(t, module.Id, "line-commented-module")
	}
}

// TestIncludeBuildDirective tests that includeBuild is NOT parsed as regular include
func TestIncludeBuildDirective(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	settings := `rootProject.name = 'include-build-test'
includeBuild '../other-project'
includeBuild("../another-project")
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("includebuild-test", "1")
	require.NoError(t, err)

	for _, module := range buildInfo.Modules {
		assert.NotContains(t, module.Id, "other-project")
		assert.NotContains(t, module.Id, "another-project")
	}
}

// TestMalformedContent tests graceful handling of syntactically incorrect files
func TestMalformedContent(t *testing.T) {
	tests := []struct {
		name            string
		buildContent    string
		settingsContent string
		expectedInId    string
	}{
		{
			name: "unclosed dependencies block",
			buildContent: `plugins { id 'java' }
group = 'com.example.malformed'
version = '1.0.0'
dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'`,
			expectedInId: "com.example.malformed",
		},
		{
			name: "unclosed block comment in settings",
			buildContent: `plugins { id 'java' }
group = 'com.example'
version = '1.0.0'`,
			settingsContent: `rootProject.name = 'unclosed-comment'
/* This block comment is never closed
include 'app'`,
			expectedInId: "unclosed-comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(tt.buildContent), 0644)
			require.NoError(t, err)

			if tt.settingsContent != "" {
				err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(tt.settingsContent), 0644)
				require.NoError(t, err)
			}

			config := flexpack.GradleConfig{WorkingDirectory: tempDir}
			gf, err := gradleflexpack.NewGradleFlexPack(config)
			require.NoError(t, err)

			buildInfo, err := gf.CollectBuildInfo("malformed-test", "1")
			require.NoError(t, err)
			assert.Contains(t, buildInfo.Modules[0].Id, tt.expectedInId)
		})
	}
}

// TestNameRegexFalsePositive tests that 'name' in non-project contexts doesn't confuse parsing
func TestNameRegexFalsePositive(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example.falsename'
version = '1.0.0'

tasks.register('myTask') {
    doLast {
        def name = 'task-internal-name'
        println name
    }
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	settingsContent := `rootProject.name = 'correct-project-name'`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settingsContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("false-positive-test", "1")
	require.NoError(t, err)

	assert.Contains(t, buildInfo.Modules[0].Id, "correct-project-name")
}

// TestSpringBootGradleSyntax tests parsing Spring Boot specific configurations
func TestSpringBootGradleSyntax(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins {
    id 'java'
    id 'org.springframework.boot' version '3.2.0'
}

group = 'com.example.spring'
version = '1.0.0'

dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web'
    testImplementation 'org.springframework.boot:spring-boot-starter-test'
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

	buildInfo, err := gf.CollectBuildInfo("spring-boot-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.spring")
	assert.Contains(t, buildInfo.Modules[0].Id, "1.0.0")
}

// TestAndroidPublishingConfiguration tests parsing of Android project with publishing configuration
func TestAndroidPublishingConfiguration(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins {
    id 'com.android.application'
    id 'maven-publish'
}

android {
    compileSdkVersion 30
    buildToolsVersion "30.0.3"

    defaultConfig {
        applicationId "ch.datatrans.android.sample"
        minSdkVersion 26
        targetSdkVersion 30
        versionCode 6
        versionName "0.0.6"
    }
}

afterEvaluate {
    publishing {
        publications {
            debug(MavenPublication) {
                groupId = 'ch.datatrans'
                artifactId = 'android-sample-app'
                version = android.defaultConfig.versionName
            }
        }
        repositories {
            maven {
                // name = 'localRepo'
                url = "https://ecosysjfrog.jfrog.io/artifactory/gradle"
            }
        }
    }
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	// settings.gradle is empty or default
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(""), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("android-test", "1")
	require.NoError(t, err)

	// Expect ch.datatrans:android-sample-app:0.0.6
	assert.Contains(t, buildInfo.Modules[0].Id, "ch.datatrans")
	assert.Contains(t, buildInfo.Modules[0].Id, "android-sample-app")
	assert.Contains(t, buildInfo.Modules[0].Id, "0.0.6")
}

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

// TestAndroidScopeMapping verifies scope mapping logic handles Android configurations
func TestAndroidScopeMapping(t *testing.T) {
	tempDir := t.TempDir()
	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	tests := []struct {
		config string
		scope  string
	}{
		{"debugCompileClasspath", "compile"},
		{"debugRuntimeClasspath", "runtime"},
		{"releaseCompileClasspath", "compile"},
		{"releaseRuntimeClasspath", "runtime"},
		{"debugAndroidTestCompileClasspath", "test"},
		{"debugUnitTestCompileClasspath", "test"},
		{"releaseUnitTestRuntimeClasspath", "test"},
		{"testCompileClasspath", "test"},
		{"compileClasspath", "compile"},
	}

	for _, tt := range tests {
		scopes := gf.MapGradleConfigurationToScopes(tt.config)
		found := false
		for _, s := range scopes {
			if s == tt.scope {
				found = true
				break
			}
		}
		assert.True(t, found, "Configuration %s should map to scope %s", tt.config, tt.scope)
	}
}
