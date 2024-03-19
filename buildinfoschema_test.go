package main

import (
	"os"
	"path/filepath"
	"testing"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/tests"
	"github.com/jfrog/build-info-go/utils"

	"github.com/stretchr/testify/assert"
	"github.com/xeipuuv/gojsonschema"
)

func TestGoSchema(t *testing.T) {
	validateBuildInfoSchema(t, "go", filepath.Join("golang", "project"), func() {})
}

func TestMavenSchema(t *testing.T) {
	validateBuildInfoSchema(t, "mvn", filepath.Join("maven", "project"), func() {})
}

func TestNpmSchema(t *testing.T) {
	validateBuildInfoSchema(t, "npm", filepath.Join("npm", "project1", "npmv8"), func() {
		_, _, err := buildutils.RunNpmCmd("npm", "", []string{"install"}, &utils.NullLog{})
		assert.NoError(t, err)
	})
}

func TestYarnSchema(t *testing.T) {
	validateBuildInfoSchema(t, "yarn", filepath.Join("yarn", "v2", "project"), func() {})
}

// Validate a build info schema for the input project.
// t              - The testing object
// commandName    - Command to run such as npm, yarn, mvn, and go
// pathInTestData - The path of the test project in testdata dir
// install        - Install the project, if needed
func validateBuildInfoSchema(t *testing.T, commandName, pathInTestData string, install func()) {
	// Load build-info schema
	schema, err := os.ReadFile("buildinfo-schema.json")
	assert.NoError(t, err)
	schemaLoader := gojsonschema.NewBytesLoader(schema)

	// Prepare test project
	cleanUp := prepareProject(t, pathInTestData, install)

	// Generate build-info
	buildInfoContent := generateBuildInfo(t, commandName)
	documentLoader := gojsonschema.NewBytesLoader(buildInfoContent)

	// Validate build-info schema
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	assert.NoError(t, err)
	assert.True(t, result.Valid(), result.Errors())

	// Clean up
	cleanUp()
}

// Copy project from testdata to a temp directory and change dir to the temp dir.
// t              - The testing object
// pathInTestdata - The path of the test project in to the testdata dir
// install -      - Installs the project
// Returns a cleanup callback
func prepareProject(t *testing.T, pathInTestdata string, install func()) func() {
	wd, err := os.Getwd()
	assert.NoError(t, err)
	tempDir, cleanup := tests.CreateTestProject(t, filepath.Join("build", "testdata", pathInTestdata))
	assert.NoError(t, os.Chdir(tempDir))
	install()

	return func() {
		assert.NoError(t, os.Chdir(wd))
		cleanup()
	}
}

// Generate build-info by running "bi <commandName>" in the current working directory
// t              - The testing object
// commandName    - Command to run such as npm, yarn, mvn, and go
// Returns the buildinfo content
func generateBuildInfo(t *testing.T, commandName string) []byte {
	// Save old state
	oldArgs := os.Args
	oldStdout := os.Stdout

	// Create a commandOutput file
	commandOutput, err := os.CreateTemp("", "output")
	assert.NoError(t, err)

	defer func() {
		os.Stdout = oldStdout
		os.Args = oldArgs
		assert.NoError(t, commandOutput.Close())
		assert.NoError(t, os.Remove(commandOutput.Name()))
	}()

	// Execute command with output redirection to a temp file
	os.Stdout = commandOutput
	os.Args = []string{"bi", commandName}
	main()

	// Read output
	content, err := os.ReadFile(commandOutput.Name())
	assert.NoError(t, err)
	assert.NotEmpty(t, content)
	return content
}
