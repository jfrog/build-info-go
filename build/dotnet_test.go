package build

import (
	"github.com/jfrog/build-info-go/build/utils/dotnet"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/io"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGetFlagValueExists(t *testing.T) {
	tests := []struct {
		name              string
		currentConfigPath string
		createConfig      bool
		expectErr         bool
		cmdFlags          []string
		expectedCmdFlags  []string
	}{
		{"simple", "file.config", true, false,
			[]string{"-configFile", "file.config"}, []string{"-configFile", "file.config"}},

		{"simple2", "file.config", true, false,
			[]string{"-before", "-configFile", "file.config", "after"}, []string{"-before", "-configFile", "file.config", "after"}},

		{"err", "file.config", false, true,
			[]string{"-before", "-configFile"}, []string{"-before", "-configFile"}},

		{"err2", "file.config", false, true,
			[]string{"-configFile"}, []string{"-configFile"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.createConfig {
				_, err := io.CreateRandFile(test.currentConfigPath, 0)
				if err != nil {
					t.Error(err)
				}
				defer utils.RemoveAndAssert(t, test.currentConfigPath)
			}
			c := &dotnet.Cmd{CommandFlags: test.cmdFlags}
			_, err := getFlagValueIfExists("-configfile", c)
			if err != nil && !test.expectErr {
				t.Error(err)
			}
			if err == nil && test.expectErr {
				t.Errorf("Expecting: error, Got: nil")
			}
			if !reflect.DeepEqual(c.CommandFlags, test.expectedCmdFlags) {
				t.Errorf("Expecting: %s, Got: %s", test.expectedCmdFlags, c.CommandFlags)
			}
		})
	}
}

func TestUpdateSolutionPathAndGetFileName(t *testing.T) {
	workingDir, err := os.Getwd()
	assert.NoError(t, err)
	tests := []struct {
		name                 string
		flags                []string
		solutionPath         string
		expectedSlnFile      string
		expectedSolutionPath string
	}{
		{"emptyFlags", []string{}, workingDir, "", workingDir},
		{"justFlags", []string{"-flag1", "value1", "-flag2", "value2"}, workingDir, "", workingDir},
		{"relFileArgRelPath1", []string{filepath.Join("testdata", "slnDir", "sol.sln")}, filepath.Join("rel", "path"), "sol.sln", filepath.Join("rel", "path", "testdata", "slnDir")},
		{"relDirArgRelPath2", []string{filepath.Join("testdata", "slnDir")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "slnDir")},
		{"absFileArgRelPath1", []string{filepath.Join(workingDir, "testdata", "slnDir", "sol.sln")}, filepath.Join(".", "rel", "path"), "sol.sln", filepath.Join(workingDir, "testdata", "slnDir")},
		{"absDirArgRelPath2", []string{filepath.Join(workingDir, "testdata", "slnDir"), "-flag", "value"}, filepath.Join(".", "rel", "path"), "", filepath.Join(workingDir, "testdata", "slnDir")},
		{"nonExistingFile", []string{filepath.Join(".", "dir1", "sol.sln")}, workingDir, "", workingDir},
		{"nonExistingPath", []string{filepath.Join(workingDir, "non", "existing", "path")}, workingDir, "", workingDir},
		{"relCsprojFile", []string{filepath.Join("testdata", "slnDir", "proj.csproj")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "slnDir")},
		{"relVbprojFile", []string{filepath.Join("testdata", "slnDir", "projTwo.vbproj")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "slnDir")},
		{"absCsprojFile", []string{filepath.Join(workingDir, "testdata", "slnDir", "proj.csproj")}, filepath.Join("rel", "path"), "", filepath.Join(workingDir, "testdata", "slnDir")},
		{"absVbprojFile", []string{filepath.Join(workingDir, "testdata", "slnDir", "projTwo.vbproj")}, filepath.Join("rel", "path"), "", filepath.Join(workingDir, "testdata", "slnDir")},
		{"relPackagesConfigFile", []string{filepath.Join("testdata", "slnDir", "packages.config")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "slnDir")},
		{"absPackagesConfigFile", []string{filepath.Join(workingDir, "testdata", "slnDir", "packages.config")}, filepath.Join("rel", "path"), "", filepath.Join(workingDir, "testdata", "slnDir")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dc := DotnetModule{solutionPath: test.solutionPath, argAndFlags: test.flags}
			slnFile, err := dc.updateSolutionPathAndGetFileName()
			assert.NoError(t, err)
			assert.Equal(t, test.expectedSlnFile, slnFile)
			assert.Equal(t, test.expectedSolutionPath, dc.solutionPath)
		})
	}
}
