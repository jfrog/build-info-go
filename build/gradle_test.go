package build

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
	"github.com/stretchr/testify/assert"
)

const gradleVersionPattern = `------------------------------------------------------------
Gradle %s
------------------------------------------------------------

Build time:   2019-11-01 20:42:00 UTC
Revision:     dd870424f9bd8e195d614dc14bb140f43c22da98

Kotlin:       1.3.41
Groovy:       2.5.4
Ant:          Apache Ant(TM) version 1.9.14 compiled on March 12 2019
JVM:          11.0.10 (AdoptOpenJDK 11.0.10+9)
OS:           Mac OS X 10.16 x86_64
`

var downloadExtractorsFromReleasesCases = []struct {
	extractorVersion string
}{
	{extractorVersion: gradleExtractor4DependencyVersion},
	{extractorVersion: gradleExtractor5DependencyVersion},
}

func TestDownloadExtractorsFromReleases(t *testing.T) {
	for _, testCase := range downloadExtractorsFromReleasesCases {
		t.Run(testCase.extractorVersion, func(t *testing.T) {
			tempDirPath, err := utils.CreateTempDir()
			assert.NoError(t, err)
			defer func() {
				assert.NoError(t, utils.RemoveTempDir(tempDirPath))
				assert.NoError(t, utils.CleanOldDirs())
			}()

			// Download JAR
			err = downloadGradleDependencies(tempDirPath, testCase.extractorVersion, nil, &utils.NullLog{})
			assert.NoError(t, err)

			// Make sure the Gradle build-info extractor JAR exist
			expectedJarPath := filepath.Join(tempDirPath, fmt.Sprintf(gradleExtractorFileName, testCase.extractorVersion))
			assert.FileExists(t, expectedJarPath)
		})
	}
}

var getExtractorVersionAndInitScriptCases = []struct {
	projectName               string
	expectedExtractorVersion  string
	expectedInitScriptPattern string
}{
	{projectName: "gradle-6.8", expectedExtractorVersion: gradleExtractor4DependencyVersion, expectedInitScriptPattern: gradleInitScriptExtractor4},
	{projectName: "gradle-6.8.1", expectedExtractorVersion: gradleExtractor5DependencyVersion, expectedInitScriptPattern: gradleInitScriptExtractor5},
	{projectName: "gradle-7.0", expectedExtractorVersion: gradleExtractor5DependencyVersion, expectedInitScriptPattern: gradleInitScriptExtractor5},
}

func TestGetExtractorVersionAndInitScript(t *testing.T) {
	gradleModule := &GradleModule{containingBuild: &Build{logger: &utils.NullLog{}}}
	gradleExe, err := GetGradleExecPath(true)
	assert.NoError(t, err)
	for _, testCase := range getExtractorVersionAndInitScriptCases {
		t.Run(testCase.projectName, func(t *testing.T) {
			projectPath := filepath.Join("testdata", "gradle", testCase.projectName, gradleExe)
			gradleExtractorVersion, initScriptPattern, err := gradleModule.getExtractorVersionAndInitScript(projectPath)
			assert.NoError(t, err)
			assert.Equal(t, testCase.expectedExtractorVersion, gradleExtractorVersion)
			assert.Equal(t, testCase.expectedInitScriptPattern, initScriptPattern)
		})
	}
}

func TestGetGradlePluginVersionError(t *testing.T) {
	gradleModule := &GradleModule{containingBuild: &Build{logger: &utils.NullLog{}}}
	_, _, err := gradleModule.getExtractorVersionAndInitScript("non-exist")
	assert.ErrorContains(t, err, "executable file not found")
}

var parseGradleVersionCases = []struct {
	versionOutput   string
	expectedVersion *version.Version
}{
	{versionOutput: "1.2", expectedVersion: version.NewVersion("1.2")},
	{versionOutput: "1.2.3", expectedVersion: version.NewVersion("1.2.3")},
	{versionOutput: "1.23.4", expectedVersion: version.NewVersion("1.23.4")},
	{versionOutput: "1.2-rc-1", expectedVersion: version.NewVersion("1.2-rc-1")},
}

func TestParseGradleVersion(t *testing.T) {
	for _, testCase := range parseGradleVersionCases {
		t.Run(testCase.expectedVersion.GetVersion(), func(t *testing.T) {
			actualVersion, err := parseGradleVersion(fmt.Sprintf(gradleVersionPattern, testCase.versionOutput))
			assert.NoError(t, err)
			assert.Equal(t, testCase.expectedVersion, actualVersion)
		})
	}
}

func TestFormatCommandProperties(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"clean", "-Dparam=value", "build", "-Pkey=value"},
			expected: []string{"clean", "-Dparam=value", "build", "-Pkey=value"},
		},
		{
			input:    []string{"-Dprop1=value1", "test", "-Pprop2=value2"},
			expected: []string{"-Dprop1=value1", "test", "-Pprop2=value2"},
		},
		{
			input:    []string{"-Dparam1=value1 value2", "-Pkey1=value1", "-Dparam2=value2", "-Pkey2=value1 value2"},
			expected: []string{"-Dparam1='value1 value2'", "-Pkey1=value1", "-Dparam2=value2", "-Pkey2='value1 value2'"},
		},
		{
			input:    []string{"-Dparam1=value1", "run", "-Psign"},
			expected: []string{"-Dparam1=value1", "run", "-Psign"},
		},
	}

	for _, test := range tests {
		result := formatCommandProperties(test.input)
		assert.ElementsMatch(t, test.expected, result)
	}
}
