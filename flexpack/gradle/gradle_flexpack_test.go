package flexpack_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	gradleflexpack "github.com/jfrog/build-info-go/flexpack/gradle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyWorkingDirectory tests error handling for empty working directory
func TestEmptyWorkingDirectory(t *testing.T) {
	config := flexpack.GradleConfig{WorkingDirectory: ""}
	_, err := gradleflexpack.NewGradleFlexPack(config)
	assert.Error(t, err, "Should fail with empty working directory")
	assert.Contains(t, err.Error(), "empty")
}

// TestNonExistentWorkingDirectory tests error handling for non-existent directory
func TestNonExistentWorkingDirectory(t *testing.T) {
	config := flexpack.GradleConfig{WorkingDirectory: "/nonexistent/path/xyz123"}
	_, err := gradleflexpack.NewGradleFlexPack(config)
	assert.Error(t, err, "Should fail with non-existent directory")
	assert.Contains(t, err.Error(), "does not exist")
}

// TestNoBuildFileGracefulHandling tests handling when no build file exists
// func TestNoBuildFileGracefulHandling(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo, err := gf.CollectBuildInfo("no-build-test", "1")
// 	require.NoError(t, err)
// 	assert.NotNil(t, buildInfo)
// }

// TestCollectBuildInfoBasic tests basic build info collection
// func TestCollectBuildInfoBasic(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{
// 		WorkingDirectory:        tempDir,
// 		IncludeTestDependencies: true,
// 	}

// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err, "Should create Gradle FlexPack successfully")

// 	buildInfo, err := gf.CollectBuildInfo("test-build", "123")
// 	require.NoError(t, err, "Should collect build info successfully")

// 	assert.Equal(t, "test-build", buildInfo.Name)
// 	assert.Equal(t, "123", buildInfo.Number)
// 	assert.NotEmpty(t, buildInfo.Started, "Started timestamp should be set")

// 	require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")
// 	moduleId := buildInfo.Modules[0].Id
// 	assert.Contains(t, moduleId, "com.jfrog.test", "Module ID should contain groupId")
// 	assert.Contains(t, moduleId, "1.0.0", "Module ID should contain version")
// }

// TestCollectBuildInfoWithDifferentBuildNumbers tests collecting build info with various identifiers
// func TestCollectBuildInfoWithDifferentBuildNumbers(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	testCases := []struct {
// 		buildName   string
// 		buildNumber string
// 	}{
// 		{"simple-build", "1"},
// 		{"build-with-timestamp", "2024.01.15.001"},
// 		{"ci-pipeline", "pipeline-123-job-456"},
// 		{"snapshot-build", "SNAPSHOT-2024.01.15"},
// 		{"release", "v1.0.0-rc1"},
// 	}

// 	for _, tc := range testCases {
// 		t.Run(tc.buildName+"_"+tc.buildNumber, func(t *testing.T) {
// 			buildInfo, err := gf.CollectBuildInfo(tc.buildName, tc.buildNumber)
// 			require.NoError(t, err)
// 			assert.Equal(t, tc.buildName, buildInfo.Name)
// 			assert.Equal(t, tc.buildNumber, buildInfo.Number)
// 		})
// 	}
// }

// TestCollectBuildInfoIdempotent tests that multiple calls produce consistent results
// func TestCollectBuildInfoIdempotent(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo1, err := gf.CollectBuildInfo("idempotent-test", "1")
// 	require.NoError(t, err)

// 	buildInfo2, err := gf.CollectBuildInfo("idempotent-test", "2")
// 	require.NoError(t, err)

// 	buildInfo3, err := gf.CollectBuildInfo("idempotent-test", "3")
// 	require.NoError(t, err)

// 	require.Equal(t, len(buildInfo1.Modules), len(buildInfo2.Modules))
// 	require.Equal(t, len(buildInfo2.Modules), len(buildInfo3.Modules))

// 	if len(buildInfo1.Modules) > 0 {
// 		assert.Equal(t, buildInfo1.Modules[0].Id, buildInfo2.Modules[0].Id)
// 		assert.Equal(t, buildInfo2.Modules[0].Id, buildInfo3.Modules[0].Id)
// 	}

// 	assert.Equal(t, buildInfo1.Agent.Name, buildInfo2.Agent.Name)
// 	assert.Equal(t, buildInfo1.Agent.Version, buildInfo2.Agent.Version)
// }

// // TestCollectBuildInfoWithDifferentInstances tests multiple FlexPack instances
// func TestCollectBuildInfoWithDifferentInstances(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}

// 	gf1, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	gf2, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo1, err := gf1.CollectBuildInfo("instance-test", "1")
// 	require.NoError(t, err)

// 	buildInfo2, err := gf2.CollectBuildInfo("instance-test", "2")
// 	require.NoError(t, err)

// 	require.Equal(t, len(buildInfo1.Modules), len(buildInfo2.Modules))
// 	if len(buildInfo1.Modules) > 0 {
// 		assert.Equal(t, buildInfo1.Modules[0].Id, buildInfo2.Modules[0].Id)
// 	}
// }

// TestBuildInfoAgentInfo tests agent information in build info
// func TestBuildInfoAgentInfo(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo, err := gf.CollectBuildInfo("agent-test", "1")
// 	require.NoError(t, err)

// 	require.NotNil(t, buildInfo.Agent, "Agent should not be nil")
// 	assert.Equal(t, "build-info-go", buildInfo.Agent.Name)
// 	assert.NotEmpty(t, buildInfo.Agent.Version, "Agent version should be set")
// }

// TestBuildInfoModuleStructure tests module structure in build info
// func TestBuildInfoModuleStructure(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo, err := gf.CollectBuildInfo("module-structure-test", "1")
// 	require.NoError(t, err)

// 	require.Greater(t, len(buildInfo.Modules), 0, "Should have at least one module")
// 	module := buildInfo.Modules[0]

// 	assert.NotEmpty(t, module.Id, "Module should have an ID")
// 	assert.Equal(t, entities.Gradle, module.Type, "Module type should be gradle")

// 	parts := splitModuleId(module.Id)
// 	assert.Equal(t, 3, len(parts), "Module ID should have 3 parts (groupId:artifactId:version)")
// }

// TestBuildInfoTimestamp tests that started timestamp is properly formatted
// func TestBuildInfoTimestamp(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()
// 	setupMinimalGradleProject(t, tempDir)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo, err := gf.CollectBuildInfo("timestamp-test", "1")
// 	require.NoError(t, err)

// 	assert.NotEmpty(t, buildInfo.Started, "Started timestamp should be set")

// 	_, err = time.Parse(entities.TimeFormat, buildInfo.Started)
// 	assert.NoError(t, err, "Started timestamp should be in valid format")
// }

// TestModuleIdFormatConsistency tests that module IDs consistently have group:artifact:version format
// func TestModuleIdFormatConsistency(t *testing.T) {
// 	skipIfGradleInvalid(t)
// 	tempDir := t.TempDir()

// 	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
// plugins { id 'java' }
// group = 'com.example.format'
// version = '2.0.0-SNAPSHOT'
// `), 0644)
// 	require.NoError(t, err)

// 	settings := `rootProject.name = 'format-test-project'`
// 	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
// 	require.NoError(t, err)

// 	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
// 	gf, err := gradleflexpack.NewGradleFlexPack(config)
// 	require.NoError(t, err)

// 	buildInfo, err := gf.CollectBuildInfo("format-test", "1")
// 	require.NoError(t, err)

// 	for _, module := range buildInfo.Modules {
// 		parts := splitModuleId(module.Id)
// 		assert.Equal(t, 3, len(parts), "Module ID should have 3 parts: %s", module.Id)
// 		assert.NotEmpty(t, parts[0], "Group should not be empty")
// 		assert.NotEmpty(t, parts[1], "Artifact should not be empty")
// 		assert.NotEmpty(t, parts[2], "Version should not be empty")
// 	}
// }

// TestVersionWithMetadata tests version strings with build metadata
func TestVersionWithMetadata(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	buildGradle := `
plugins { id 'java' }
group = 'com.example'
version = '1.0.0+build.123'
`
	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(buildGradle), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("metadata-version-test", "1")
	require.NoError(t, err)
	assert.Contains(t, buildInfo.Modules[0].Id, "1.0.0+build.123")
}

// TestBuildNameAndNumberSpecialChars tests build info with special characters in name/number
func TestBuildNameAndNumberSpecialChars(t *testing.T) {
	skipIfGradleInvalid(t)
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

	specialBuildNames := []struct {
		name   string
		number string
	}{
		{"build/with/slashes", "1.0.0"},
		{"build-with-dashes", "2024-01-15"},
		{"build_with_underscores", "v1.0.0-rc1"},
		{"build.with.dots", "123.456"},
		{"build with spaces", "build number 1"},
	}

	for _, tc := range specialBuildNames {
		buildInfo, err := gf.CollectBuildInfo(tc.name, tc.number)
		require.NoError(t, err, "Failed for name=%s, number=%s", tc.name, tc.number)
		assert.Equal(t, tc.name, buildInfo.Name)
		assert.Equal(t, tc.number, buildInfo.Number)
	}
}

// TestMultiModuleBuildInfoStructure tests build info structure for multi-module projects
func TestMultiModuleBuildInfoStructure(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example.multi'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(`
rootProject.name = 'multi-module-project'
include 'app'
include 'lib'
include 'core'
`), 0644)
	require.NoError(t, err)

	for _, module := range []string{"app", "lib", "core"} {
		dir := filepath.Join(tempDir, module)
		err = os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
		require.NoError(t, err)
	}

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("multi-module-test", "1")
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1, "Should have at least root module")

	for _, module := range buildInfo.Modules {
		assert.NotEmpty(t, module.Id, "Each module should have an ID")
		assert.Equal(t, entities.Gradle, module.Type, "Each module should be Gradle type")
	}
}

// TestMultiModuleUniqueness tests that module IDs are unique
func TestMultiModuleUniqueness(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(`
rootProject.name = 'unique-test'
include 'moduleA'
include 'moduleB'
`), 0644)
	require.NoError(t, err)

	for _, module := range []string{"moduleA", "moduleB"} {
		dir := filepath.Join(tempDir, module)
		err = os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
		require.NoError(t, err)
	}

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("uniqueness-test", "1")
	require.NoError(t, err)

	moduleIds := make(map[string]bool)
	for _, module := range buildInfo.Modules {
		if moduleIds[module.Id] {
			t.Errorf("Duplicate module ID found: %s", module.Id)
		}
		moduleIds[module.Id] = true
	}
}

// TestModuleWithSameNameAsRoot tests module with same artifact name as root project
func TestModuleWithSameNameAsRoot(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	coreDir := filepath.Join(tempDir, "libs", "core")
	err = os.MkdirAll(coreDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(coreDir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
	require.NoError(t, err)

	settings := `rootProject.name = 'core'
include 'libs:core'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("same-name-test", "1")
	require.NoError(t, err)

	moduleIds := make(map[string]bool)
	for _, module := range buildInfo.Modules {
		if moduleIds[module.Id] {
			t.Errorf("Duplicate module ID found: %s", module.Id)
		}
		moduleIds[module.Id] = true
	}
}

// TestModuleWithGroupOverride tests submodule with its own group ID
func TestModuleWithGroupOverride(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.root.project'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	subDir := filepath.Join(tempDir, "submodule")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(subDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.different.group'
version = '2.0.0'
`), 0644)
	require.NoError(t, err)

	settings := `rootProject.name = 'group-override-test'
include 'submodule'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("group-override-test", "1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1)
}

// TestDeepNestedModulePath tests deeply nested module paths
func TestDeepNestedModulePath(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	deepPath := filepath.Join(tempDir, "level1", "level2", "level3", "level4", "level5")
	err = os.MkdirAll(deepPath, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(deepPath, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
	require.NoError(t, err)

	settings := `rootProject.name = 'deep-path'
include 'level1:level2:level3:level4:level5'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settings), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("deep-path-test", "1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1)
}

// TestModuleNamesWithSpecialCharacters tests module names with various special characters
func TestModuleNamesWithSpecialCharacters(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '1.0.0'
`), 0644)
	require.NoError(t, err)

	// Test various special character patterns in module names
	modules := []string{"my-app", "my_lib", "core-utils", "my.module", "module_v2"}
	for _, m := range modules {
		dir := filepath.Join(tempDir, m)
		err = os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(`plugins { id 'java' }`), 0644)
		require.NoError(t, err)
	}

	settingsContent := `rootProject.name = 'special-chars'
include 'my-app'
include 'my_lib'
include 'core-utils'
include 'my.module'
include 'module_v2'
`
	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(settingsContent), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("special-test", "1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(buildInfo.Modules), 1)
}

// --- Configuration Tests ---

// TestIncludeTestDependenciesConfig tests the IncludeTestDependencies configuration
func TestIncludeTestDependenciesConfig(t *testing.T) {
	skipIfGradleInvalid(t)
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

	configWithTests := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: true,
	}
	gfWithTests, err := gradleflexpack.NewGradleFlexPack(configWithTests)
	require.NoError(t, err)

	buildInfoWithTests, err := gfWithTests.CollectBuildInfo("with-tests", "1")
	require.NoError(t, err)

	configWithoutTests := flexpack.GradleConfig{
		WorkingDirectory:        tempDir,
		IncludeTestDependencies: false,
	}
	gfWithoutTests, err := gradleflexpack.NewGradleFlexPack(configWithoutTests)
	require.NoError(t, err)

	buildInfoWithoutTests, err := gfWithoutTests.CollectBuildInfo("without-tests", "1")
	require.NoError(t, err)

	assert.NotNil(t, buildInfoWithTests)
	assert.NotNil(t, buildInfoWithoutTests)
}

// TestMultiLevelDependencyTree tests parsing multi-level transitive dependencies
func TestMultiLevelDependencyTree(t *testing.T) {
	skipIfGradleInvalid(t)
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

// TestAnnotationProcessorDependencies tests kapt/annotationProcessor configurations
func TestAnnotationProcessorDependencies(t *testing.T) {
	skipIfGradleInvalid(t)
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
	skipIfGradleInvalid(t)
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

// TestGradleFlexPackWithSettingsGradle tests project name resolution from settings.gradle
func TestGradleFlexPackWithSettingsGradle(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte(`
plugins { id 'java' }
group = 'com.example'
version = '2.0.0'
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "settings.gradle"), []byte(`rootProject.name = 'my-gradle-project'
`), 0644)
	require.NoError(t, err)

	config := flexpack.GradleConfig{WorkingDirectory: tempDir}
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)

	moduleId := buildInfo.Modules[0].Id
	assert.Contains(t, moduleId, "my-gradle-project", "Module ID should use rootProject.name from settings.gradle")
}

// TestGradleFlexPackNestedDependencies tests parsing of nested dependency blocks
func TestGradleFlexPackNestedDependencies(t *testing.T) {
	skipIfGradleInvalid(t)
	tempDir := t.TempDir()

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
	gf, err := gradleflexpack.NewGradleFlexPack(config)
	require.NoError(t, err)

	buildInfo, err := gf.CollectBuildInfo("nested-test", "1")
	require.NoError(t, err)
	require.Greater(t, len(buildInfo.Modules), 0)
}

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

func splitModuleId(moduleId string) []string {
	result := []string{}
	current := ""
	for _, c := range moduleId {
		if c == ':' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
