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

// TestParseGroupIdWithSingleQuotes tests parsing group with single quotes
func TestParseGroupIdWithSingleQuotes(t *testing.T) {
	tempDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example.single'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("group-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.single")
}

// TestParseGroupIdWithDoubleQuotes tests parsing group with double quotes
func TestParseGroupIdWithDoubleQuotes(t *testing.T) {
	tempDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = "com.example.double"
version = "1.0.0"
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("group-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "com.example.double")
}

// TestParseGroupIdMissing tests parsing when no group is specified
func TestParseGroupIdMissing(t *testing.T) {
	tempDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("group-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "unspecified")
}

// TestParseVersionSnapshot tests parsing SNAPSHOT version
func TestParseVersionSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '2.0.0-SNAPSHOT'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("version-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "2.0.0-SNAPSHOT")
}

// TestParseVersionReleaseCandidate tests parsing RC version
func TestParseVersionReleaseCandidate(t *testing.T) {
	tempDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '3.0.0-RC1'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("version-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "3.0.0-RC1")
}

// TestParseVersionMissing tests parsing when no version is specified
func TestParseVersionMissing(t *testing.T) {
	tempDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("version-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "unspecified")
}

// TestDependencyStringNotation tests parsing dependencies with string notation
func TestDependencyStringNotation(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'com.google.guava:guava:31.1-jre'
    implementation 'org.apache.commons:commons-lang3:3.12.0'
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

	buildInfo, err := gf.CollectBuildInfo("string-dep-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestDependencyMapNotation tests parsing dependencies with map notation
func TestDependencyMapNotation(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation group: 'com.google.code.gson', name: 'gson', version: '2.10'
    implementation(group: 'org.apache.httpcomponents', name: 'httpclient', version: '4.5.14')
}
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("map-dep-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
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

// TestSettingsGradleParsing tests parsing of settings.gradle for project name
func TestSettingsGradleParsing(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	settings := `rootProject.name = 'my-project-from-settings'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("settings-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "my-project-from-settings")
}

// TestSettingsGradleKtsParsing tests parsing of settings.gradle.kts for rootProject.name
func TestSettingsGradleKtsParsing(t *testing.T) {
	tempDir := t.TempDir()

	// Build file without explicit name
	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	// settings.gradle.kts with rootProject.name - should be used for artifact ID
	settingsKts := `rootProject.name = "kotlin-settings-project"
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle.kts"), []byte(settingsKts), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("kts-settings-test", "1")
	require.NoError(t, err)

	// rootProject.name from settings.gradle.kts should be used as artifact ID
	assert.Contains(t, buildInfo.Modules[0].Id, "kotlin-settings-project")
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

// TestCommentedIncludeStatement tests that commented include statements are NOT parsed
func TestCommentedIncludeStatement(t *testing.T) {
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

	settings := `rootProject.name = 'comment-test'
include 'app'
// include 'commented-module'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("comment-test", "1")
	require.NoError(t, err)

	for _, module := range buildInfo.Modules {
		assert.NotContains(t, module.Id, "commented-module")
	}
}

// TestSettingsGradleWithComments tests settings.gradle with various comment styles
func TestSettingsGradleWithComments(t *testing.T) {
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

	settings := `// Root project name
rootProject.name = 'comment-styles'

/* This is a multi-line
   comment */
include 'app'

/*
include 'shouldnt-be-included'
*/

// include 'also-shouldnt-be-included'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("comment-styles-test", "1")
	require.NoError(t, err)

	for _, module := range buildInfo.Modules {
		assert.NotContains(t, module.Id, "shouldnt-be-included")
		assert.NotContains(t, module.Id, "also-shouldnt-be-included")
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

// TestMalformedBuildGradle tests handling of syntactically incorrect build files
func TestMalformedBuildGradle(t *testing.T) {
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'

dependencies {
    implementation 'org.slf4j:slf4j-api:2.0.0'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("malformed-test", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
}

// TestUnclosedBlockComment tests handling of malformed content with unclosed block comment
func TestUnclosedBlockComment(t *testing.T) {
	tempDir := t.TempDir()

	settings := `
rootProject.name = 'unclosed-comment'
/* This block comment is never closed
include 'app'
`
	err := os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("unclosed-comment-test", "1")
	require.NoError(t, err)

	assert.Contains(t, buildInfo.Modules[0].Id, "unclosed-comment")
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
