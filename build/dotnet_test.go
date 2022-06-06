package build

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

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
		{"relFileArgRelPath1", []string{filepath.Join("testdata", "dotnet", "slnDir", "sol.sln")}, filepath.Join("rel", "path"), "sol.sln", filepath.Join("rel", "path", "testdata", "dotnet", "slnDir")},
		{"relDirArgRelPath2", []string{filepath.Join("testdata", "dotnet", "slnDir")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "dotnet", "slnDir")},
		{"absFileArgRelPath1", []string{filepath.Join(workingDir, "testdata", "dotnet", "slnDir", "sol.sln")}, filepath.Join(".", "rel", "path"), "sol.sln", filepath.Join(workingDir, "testdata", "dotnet", "slnDir")},
		{"absDirArgRelPath2", []string{filepath.Join(workingDir, "testdata", "dotnet", "slnDir"), "-flag", "value"}, filepath.Join(".", "rel", "path"), "", filepath.Join(workingDir, "testdata", "dotnet", "slnDir")},
		{"nonExistingFile", []string{filepath.Join(".", "dir1", "sol.sln")}, workingDir, "", workingDir},
		{"nonExistingPath", []string{filepath.Join(workingDir, "non", "existing", "path")}, workingDir, "", workingDir},
		{"relCsprojFile", []string{filepath.Join("testdata", "dotnet", "slnDir", "proj.csproj")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "dotnet", "slnDir")},
		{"relVbprojFile", []string{filepath.Join("testdata", "dotnet", "slnDir", "projTwo.vbproj")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "dotnet", "slnDir")},
		{"absCsprojFile", []string{filepath.Join(workingDir, "testdata", "dotnet", "slnDir", "proj.csproj")}, filepath.Join("rel", "path"), "", filepath.Join(workingDir, "testdata", "dotnet", "slnDir")},
		{"absVbprojFile", []string{filepath.Join(workingDir, "testdata", "dotnet", "slnDir", "projTwo.vbproj")}, filepath.Join("rel", "path"), "", filepath.Join(workingDir, "testdata", "dotnet", "slnDir")},
		{"relPackagesConfigFile", []string{filepath.Join("testdata", "dotnet", "slnDir", "packages.config")}, filepath.Join("rel", "path"), "", filepath.Join("rel", "path", "testdata", "dotnet", "slnDir")},
		{"absPackagesConfigFile", []string{filepath.Join(workingDir, "testdata", "dotnet", "slnDir", "packages.config")}, filepath.Join("rel", "path"), "", filepath.Join(workingDir, "testdata", "dotnet", "slnDir")},
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
