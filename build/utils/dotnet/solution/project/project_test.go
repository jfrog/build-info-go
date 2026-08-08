package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateProjectPackageIDResolution verifies that PackageID() follows MSBuild's own default
// resolution order (PackageId, then AssemblyName, then the project file's base name). This must
// match what 'dotnet pack'/'nuget pack' embed in the produced .nupkg's file name, so that
// build-info modules produced by 'restore' and by 'pack'/'push' for the same project share one
// module ID instead of splitting into two disconnected modules.
func TestCreateProjectPackageIDResolution(t *testing.T) {
	tests := []struct {
		name              string
		csprojContent     string
		expectedPackageID string
	}{
		{
			name: "explicit PackageId wins",
			csprojContent: `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <PackageId>Company.SampleLib</PackageId>
    <AssemblyName>SampleLib</AssemblyName>
  </PropertyGroup>
</Project>`,
			expectedPackageID: "Company.SampleLib",
		},
		{
			name: "falls back to AssemblyName when PackageId is absent",
			csprojContent: `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <AssemblyName>SampleLib.Core</AssemblyName>
  </PropertyGroup>
</Project>`,
			expectedPackageID: "SampleLib.Core",
		},
		{
			name: "falls back to file name when neither is set",
			csprojContent: `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>`,
			expectedPackageID: "SampleLib",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			csprojPath := filepath.Join(dir, "SampleLib.csproj")
			require.NoError(t, os.WriteFile(csprojPath, []byte(test.csprojContent), 0o600))

			proj := CreateProject(csprojPath)
			assert.Equal(t, "SampleLib", proj.Name())
			assert.Equal(t, test.expectedPackageID, proj.PackageID())
			assert.Equal(t, dir, proj.RootPath())
		})
	}
}

// TestCreateProjectPackageIDUnreadableFile verifies the fallback to the file-name-derived
// project name when the project file doesn't exist (e.g. a caller-constructed path), instead of
// erroring out.
func TestCreateProjectPackageIDUnreadableFile(t *testing.T) {
	proj := CreateProject(filepath.Join(t.TempDir(), "Missing.csproj"))
	assert.Equal(t, "Missing", proj.Name())
	assert.Equal(t, "Missing", proj.PackageID())
}
