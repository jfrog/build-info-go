package build

import (
	"fmt"
	"path/filepath"
	"runtime"
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

func TestQuoteArgsForLog(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"clean", "-Dparam=value", "build", "-Pkey=value"},
			expected: []string{"clean", "-Dparam='value'", "build", "-Pkey='value'"},
		},
		{
			input:    []string{"-Dprop1=value1", "test", "-Pprop2=value2"},
			expected: []string{"-Dprop1='value1'", "test", "-Pprop2='value2'"},
		},
		{
			input:    []string{"-Dparam1=value1 value2", "-Pkey1=value1", "-Dparam2=value2", "-Pkey2=value1 value2"},
			expected: []string{"-Dparam1='value1 value2'", "-Pkey1='value1'", "-Dparam2='value2'", "-Pkey2='value1 value2'"},
		},
		{
			input:    []string{"-Dparam1=value1", "run", "-Psign"},
			expected: []string{"-Dparam1='value1'", "run", "-Psign"},
		},
	}

	for _, test := range tests {
		result := quoteArgsForLog(test.input)
		assert.ElementsMatch(t, test.expected, result)
	}
}

func TestUnquoteProperty(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "-Dparam=value", expected: "-Dparam=value"},
		{input: "-Ddeploy.test.property=test test", expected: "-Ddeploy.test.property=test test"},
		{input: "-Ddeploy.test.property='test test'", expected: "-Ddeploy.test.property=test test"},
		{input: `-Pkey="value with spaces"`, expected: "-Pkey=value with spaces"},
		// Mismatched quote pair: left as-is.
		{input: `-Dparam='value"`, expected: `-Dparam='value"`},
		// No '=' at all: returned unchanged rather than panicking on the missing value part.
		{input: "-Dparam", expected: "-Dparam"},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, unquoteProperty(test.input))
	}
}

func TestStripPropertyQuotes(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"clean", "-Dparam=value", "build", "-Pkey=value"},
			expected: []string{"clean", "-Dparam=value", "build", "-Pkey=value"},
		},
		{
			// Quotes already stripped by the shell, value has no surrounding quotes: left as-is.
			input:    []string{"-Ddeploy.test.property=test test"},
			expected: []string{"-Ddeploy.test.property=test test"},
		},
		{
			// Quotes still present in the raw arg (e.g. on Windows, where cmd.exe/PowerShell don't strip
			// single quotes the way bash/zsh do): stripped only on Windows.
			input:    []string{"-Ddeploy.test.property='test test'"},
			expected: []string{"-Ddeploy.test.property=test test"},
		},
		{
			input:    []string{`-Pkey="value with spaces"`},
			expected: []string{"-Pkey=value with spaces"},
		},
		{
			// Mismatched quote pair: left as-is.
			input:    []string{`-Dparam='value"`},
			expected: []string{`-Dparam='value"`},
		},
	}

	for _, test := range tests {
		result := stripPropertyQuotes(test.input)
		if runtime.GOOS == "windows" {
			assert.Equal(t, test.expected, result)
		} else {
			// On non-Windows platforms the shell already stripped shell-level quoting, so any quotes
			// still present in the argument were typed deliberately and must be left untouched.
			assert.Equal(t, test.input, result)
		}
	}
}

func TestGetCmdDoesNotQuotePropertyValues(t *testing.T) {
	config := &gradleRunConfig{
		gradle: "gradle",
		tasks:  []string{"artifactoryPublish", "-Ddeploy.test.property='test test'"},
		logger: utils.NewDefaultLogger(utils.INFO),
	}

	cmd := config.GetCmd()

	expectedValue := "'test test'"
	if runtime.GOOS == "windows" {
		// Pre-existing quotes are only stripped on Windows, where the shell doesn't strip them itself.
		expectedValue = "test test"
	}
	// exec.Command runs the process directly, without a shell, so quotes around the value would be passed to
	// Gradle literally, instead of being stripped, if we didn't strip them ourselves.
	assert.Equal(t, []string{"gradle", "artifactoryPublish", "-Ddeploy.test.property=" + expectedValue}, cmd.Args)
}
