package choco

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectPackedArtifacts(t *testing.T) {
	workingDirectory := t.TempDir()
	writePackage(t, workingDirectory, "stale.1.0.0.nupkg", "stale")
	before, err := SnapshotPackageFiles(workingDirectory)
	require.NoError(t, err)

	writePackage(t, workingDirectory, "tool.2.0.0.nupkg", "tool")
	artifacts, err := CollectPackedArtifacts(workingDirectory, before, "choco-local")
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "tool.2.0.0.nupkg", artifacts[0].Name)
	assert.Equal(t, nupkgType, artifacts[0].Type)
	assert.Equal(t, "tool.2.0.0.nupkg", artifacts[0].Path)
	assert.Equal(t, "choco-local", artifacts[0].OriginalDeploymentRepo)
	assert.NotEmpty(t, artifacts[0].Sha1)
	assert.NotEmpty(t, artifacts[0].Sha256)
	assert.NotEmpty(t, artifacts[0].Md5)
}

func TestCollectPushArtifactsIgnoresFlagValues(t *testing.T) {
	workingDirectory := t.TempDir()
	packagePath := writePackage(t, workingDirectory, "tool.1.2.3.nupkg", "tool")
	writePackage(t, workingDirectory, "not-a-target.9.9.9.nupkg", "other")

	artifacts, err := CollectPushArtifacts(workingDirectory, []string{
		"--source", "https://example.test/api/nuget/choco-local",
		"--api-key", "secret",
		packagePath,
	}, "choco-local")
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "tool.1.2.3.nupkg", artifacts[0].Name)
}

func TestCollectPushArtifactsAcceptsPackageAfterBooleanFlag(t *testing.T) {
	workingDirectory := t.TempDir()
	packagePath := writePackage(t, workingDirectory, "tool.1.2.3.nupkg", "tool")

	artifacts, err := CollectPushArtifacts(workingDirectory, []string{"--yes", packagePath}, "choco-local")
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "tool.1.2.3.nupkg", artifacts[0].Name)
}

func TestCollectDirectDependencies(t *testing.T) {
	chocolateyRoot := t.TempDir()
	packageDirectory := filepath.Join(chocolateyRoot, "lib", "7zip.install")
	writePackage(t, packageDirectory, "7zip.install.23.1.0.nupkg", "old")
	time.Sleep(10 * time.Millisecond)
	writePackage(t, packageDirectory, "7zip.install.24.0.0.nupkg", "new")

	collector, err := NewChocoFlexPack(buildinfoflex.ChocoConfig{
		WorkingDirectory:  filepath.Join(t.TempDir(), "project"),
		ChocolateyInstall: chocolateyRoot,
		Packages:          []string{"--yes", "7zip.install", "missing"},
		RepoResolve:       "choco-virtual",
		Module:            "image-build",
	}, nil)
	require.NoError(t, err)

	buildInfo, err := collector.CollectBuildInfo("my-build", "42")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	module := buildInfo.Modules[0]
	assert.Equal(t, "image-build", module.Id)
	require.Len(t, module.Dependencies, 1)
	dependency := module.Dependencies[0]
	assert.Equal(t, "7zip.install:24.0.0", dependency.Id)
	assert.Equal(t, nupkgType, dependency.Type)
	assert.Equal(t, []string{"compile"}, dependency.Scopes)
	assert.Equal(t, [][]string{{"image-build"}}, dependency.RequestedBy)
	assert.Equal(t, "choco-virtual", dependency.Repository)
	assert.NotEmpty(t, dependency.Sha1)
	assert.NotEmpty(t, dependency.Sha256)
	assert.NotEmpty(t, dependency.Md5)
}

func TestBuildArtifactModules(t *testing.T) {
	modules := BuildArtifactModules([]entities.Artifact{{Name: "git.install.2.43.0.2.nupkg", Type: nupkgType}}, "")
	require.Len(t, modules, 1)
	assert.Equal(t, "git.install:2.43.0.2", modules[0].Id)
}

// Chocolatey installs the fully resolved dependency set under lib, so the transitive graph is
// recovered from the .nuspec of each installed package rather than from a lock file.
func TestCollectTransitiveDependencies(t *testing.T) {
	chocolateyRoot := t.TempDir()
	library := filepath.Join(chocolateyRoot, "lib")

	// tool -> liba, libshared, plus a dependency served by a Chocolatey special source, which never
	// lands under lib. liba -> libshared (shared requester) and back to tool (a cycle).
	writePackage(t, filepath.Join(library, "tool"), "tool.1.0.0.nupkg", "tool")
	writeNuspec(t, filepath.Join(library, "tool"), "tool.nuspec", `
		<dependency id="libA" version="[1.0,)" />
		<dependency id="libShared" version="2.0.0" />
		<dependency id="windowsfeatures-only" version="1.0.0" />`)
	// The manifest name is lower-cased here on purpose: feeds have shipped it that way, and it must
	// still be found.
	writePackage(t, filepath.Join(library, "libA"), "libA.1.1.0.nupkg", "liba")
	writeNuspec(t, filepath.Join(library, "libA"), "liba.nuspec", `
		<dependency id="libshared" version="2.0.0" />
		<dependency id="tool" version="1.0.0" />`)
	writePackage(t, filepath.Join(library, "libShared"), "libShared.2.0.0.nupkg", "shared")
	writeGroupedNuspec(t, filepath.Join(library, "libShared"), "libShared.nuspec")

	collector, err := NewChocoFlexPack(buildinfoflex.ChocoConfig{
		WorkingDirectory:  filepath.Join(t.TempDir(), "project"),
		ChocolateyInstall: chocolateyRoot,
		Packages:          []string{"--yes", "tool"},
		RepoResolve:       "choco-virtual",
		Module:            "image-build",
	}, nil)
	require.NoError(t, err)

	buildInfo, err := collector.CollectBuildInfo("my-build", "42")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	dependencies := buildInfo.Modules[0].Dependencies
	require.Len(t, dependencies, 3, "the requested package and both of its transitives")

	byID := make(map[string]entities.Dependency, len(dependencies))
	for _, dependency := range dependencies {
		byID[dependency.Id] = dependency
		assert.Equal(t, nupkgType, dependency.Type)
		assert.Equal(t, "choco-virtual", dependency.Repository)
		assert.NotEmpty(t, dependency.Sha1, dependency.Id)
		assert.NotEmpty(t, dependency.Sha256, dependency.Id)
		assert.NotEmpty(t, dependency.Md5, dependency.Id)
	}
	require.Contains(t, byID, "libA:1.1.0")
	require.Contains(t, byID, "libShared:2.0.0")

	// The transitive records the path back to the root module, and a shared dependency records one
	// path per requester.
	assert.Equal(t, [][]string{{"tool:1.0.0", "image-build"}}, byID["libA:1.1.0"].RequestedBy)
	assert.Equal(t, [][]string{
		{"tool:1.0.0", "image-build"},
		{"libA:1.1.0", "tool:1.0.0", "image-build"},
	}, byID["libShared:2.0.0"].RequestedBy)

	graph, err := collector.GetDependencyGraph()
	require.NoError(t, err)
	assert.Equal(t, []string{"tool:1.0.0"}, graph["image-build"])
	assert.Equal(t, []string{"libA:1.1.0", "libShared:2.0.0"}, graph["tool:1.0.0"])
	assert.Equal(t, []string{"libShared:2.0.0", "tool:1.0.0"}, graph["libA:1.1.0"],
		"a dependency cycle is reported as an edge, and must not loop forever")
	assert.Empty(t, graph["libShared:2.0.0"])
}

// "-v" is Chocolatey's global --verbose switch, not a short form of --version, so it must not
// swallow the package that follows it.
func TestVerboseFlagDoesNotSwallowThePackage(t *testing.T) {
	chocolateyRoot := t.TempDir()
	writePackage(t, filepath.Join(chocolateyRoot, "lib", "tool"), "tool.1.0.0.nupkg", "tool")

	collector, err := NewChocoFlexPack(buildinfoflex.ChocoConfig{
		WorkingDirectory:  filepath.Join(t.TempDir(), "project"),
		ChocolateyInstall: chocolateyRoot,
		Packages:          []string{"-v", "tool"},
		Module:            "image-build",
	}, nil)
	require.NoError(t, err)

	buildInfo, err := collector.CollectBuildInfo("my-build", "42")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	require.Len(t, buildInfo.Modules[0].Dependencies, 1)
	assert.Equal(t, "tool:1.0.0", buildInfo.Modules[0].Dependencies[0].Id)
}

// A manifest that is not valid XML belongs to a package that is already installed, so it degrades to
// "no declared dependencies" rather than failing the whole collection.
func TestUnparsableNuspecIsTreatedAsALeaf(t *testing.T) {
	chocolateyRoot := t.TempDir()
	packageDirectory := filepath.Join(chocolateyRoot, "lib", "tool")
	writePackage(t, packageDirectory, "tool.1.0.0.nupkg", "tool")
	writePackage(t, packageDirectory, "tool.nuspec", "<package><metadata>truncated")

	collector, err := NewChocoFlexPack(buildinfoflex.ChocoConfig{
		WorkingDirectory:  filepath.Join(t.TempDir(), "project"),
		ChocolateyInstall: chocolateyRoot,
		Packages:          []string{"tool"},
		Module:            "image-build",
	}, nil)
	require.NoError(t, err)

	buildInfo, err := collector.CollectBuildInfo("my-build", "42")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules[0].Dependencies, 1)
	assert.Equal(t, "tool:1.0.0", buildInfo.Modules[0].Dependencies[0].Id)
}

func writeNuspec(t *testing.T, directory, name, dependencies string) {
	t.Helper()
	writePackage(t, directory, name, `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>`+strings.TrimSuffix(name, ".nuspec")+`</id>
    <dependencies>`+dependencies+`
    </dependencies>
  </metadata>
</package>`)
}

// A .nuspec may group its dependencies per target framework instead of listing them directly.
func writeGroupedNuspec(t *testing.T, directory, name string) {
	t.Helper()
	writePackage(t, directory, name, `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>`+strings.TrimSuffix(name, ".nuspec")+`</id>
    <dependencies>
      <group targetFramework="net48" />
    </dependencies>
  </metadata>
</package>`)
}

func writePackage(t *testing.T, directory, name, contents string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o750))
	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

// writeVersionedNuspec writes the manifest Chocolatey extracts beside the .nupkg in an installed
// package directory. The version lives here, not in the .nupkg file name. Distinct from
// writeNuspec above, which builds a dependency-focused manifest with no <version> element.
func writeVersionedNuspec(t *testing.T, directory, packageID, version string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o750))
	manifest := `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>` + packageID + `</id>
    <version>` + version + `</version>
    <authors>test</authors>
    <description>test</description>
  </metadata>
</package>`
	require.NoError(t, os.WriteFile(filepath.Join(directory, packageID+".nuspec"), []byte(manifest), 0o600))
}

// TestResolveInstalledPackageReadsVersionFromNuspec pins the real on-disk layout: Chocolatey stores
// lib\<id>\<id>.nupkg with NO version in the file name, and keeps the version in the sibling
// .nuspec. Verified against chocolatey/choco's own integration suite, which asserts paths of the
// form Path.Combine(..., "lib", "isdependency", "isdependency.nupkg").
//
// The other dependency tests in this file use the versioned "<id>.<version>.nupkg" spelling, which
// a feed serves but an installed package directory never contains, so they passed while the real
// resolution path returned os.ErrNotExist and dropped every dependency.
func TestResolveInstalledPackageReadsVersionFromNuspec(t *testing.T) {
	chocolateyRoot := t.TempDir()
	packageDirectory := filepath.Join(chocolateyRoot, "lib", "7zip.install")
	writePackage(t, packageDirectory, "7zip.install.nupkg", "package")
	writeVersionedNuspec(t, packageDirectory, "7zip.install", "24.0.0")

	path, name, version, err := resolveInstalledPackage(chocolateyRoot, "7zip.install")
	require.NoError(t, err, "an installed package with no version in its file name must still resolve")
	assert.Equal(t, filepath.Join(packageDirectory, "7zip.install.nupkg"), path)
	assert.Equal(t, "7zip.install", name)
	assert.Equal(t, "24.0.0", version, "the version must come from the .nuspec")
}

// TestResolveInstalledPackageWithoutVersionSourceFails covers the honest failure: no version in the
// file name and no readable manifest means the version is genuinely unknown, and recording a
// dependency with an empty version would be worse than saying so.
func TestResolveInstalledPackageWithoutVersionSourceFails(t *testing.T) {
	chocolateyRoot := t.TempDir()
	packageDirectory := filepath.Join(chocolateyRoot, "lib", "orphan")
	writePackage(t, packageDirectory, "orphan.nupkg", "package")

	_, _, _, err := resolveInstalledPackage(chocolateyRoot, "orphan")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orphan", "the error must name the package that could not be resolved")
}

// TestExtractPackOutputDirectory covers every spelling Chocolatey accepts for `choco pack`'s output
// directory, in both the "--flag value" and "--flag=value" forms.
func TestExtractPackOutputDirectory(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		expected string
	}{
		{"absent", []string{"pack", "tool.nuspec"}, ""},
		{"out-space", []string{"pack", "--out", "build"}, "build"},
		{"out-equals", []string{"pack", "--out=build"}, "build"},
		{"outdir", []string{"pack", "--outdir=build"}, "build"},
		{"outputdirectory", []string{"pack", "--outputdirectory=build"}, "build"},
		{"output-directory", []string{"pack", "--output-directory=build"}, "build"},
		{"quoted", []string{"pack", `--output-directory="build dir"`}, "build dir"},
		{"trailing-flag-no-value", []string{"pack", "--out"}, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, ExtractPackOutputDirectory(testCase.args))
		})
	}
}

// TestCollectPackedArtifactsFromOutputDirectory covers the flag's consequence. `choco pack` does
// have an output-directory flag, so a CWD-only snapshot silently yields zero artifacts and an empty
// build-info with no error at all.
func TestCollectPackedArtifactsFromOutputDirectory(t *testing.T) {
	workingDirectory := t.TempDir()
	outputDirectory := filepath.Join(workingDirectory, "build")

	before, err := SnapshotPackageFiles(workingDirectory, outputDirectory)
	require.NoError(t, err, "a not-yet-created output directory must not fail the snapshot")

	writePackage(t, outputDirectory, "tool.2.0.0.nupkg", "tool")

	artifacts, err := CollectPackedArtifacts(workingDirectory, before, "choco-local", outputDirectory)
	require.NoError(t, err)
	require.Len(t, artifacts, 1, "a package written to --output-directory must be collected")
	assert.Equal(t, "tool.2.0.0.nupkg", artifacts[0].Name)
	assert.Equal(t, "choco-local", artifacts[0].OriginalDeploymentRepo)
}

// TestCollectPushArtifactsFallsBackToWorkingDirectory covers `choco push` with no positional path.
// Chocolatey pushes the single .nupkg in the folder in that case, so the artifact must still be
// recorded; otherwise the package uploads but is left unstamped and absent from the build-info.
func TestCollectPushArtifactsFallsBackToWorkingDirectory(t *testing.T) {
	workingDirectory := t.TempDir()
	writePackage(t, workingDirectory, "tool.1.2.3.nupkg", "tool")

	artifacts, err := CollectPushArtifacts(workingDirectory, []string{"--source", "https://example.test"}, "choco-local")
	require.NoError(t, err)
	require.Len(t, artifacts, 1, "a bare `choco push` must record the package Chocolatey found in the CWD")
	assert.Equal(t, "tool.1.2.3.nupkg", artifacts[0].Name)
}

// TestCollectPushArtifactsAmbiguousWorkingDirectory: with several packages present, the fallback
// deliberately does not guess. Chocolatey itself refuses this push and demands an explicit path, so
// in practice the native command fails and collection is never reached; the error here is the
// defensive case, and it must not silently attribute an arbitrary package to the build.
func TestCollectPushArtifactsAmbiguousWorkingDirectory(t *testing.T) {
	workingDirectory := t.TempDir()
	writePackage(t, workingDirectory, "tool.1.2.3.nupkg", "tool")
	writePackage(t, workingDirectory, "other.4.5.6.nupkg", "other")

	_, err := CollectPushArtifacts(workingDirectory, []string{"--source", "https://example.test"}, "choco-local")
	require.Error(t, err, "an ambiguous folder must not resolve to an arbitrarily chosen package")
}

// TestCollectPushArtifactsIgnoresApiKeyValue guards the credential spellings now in the
// value-taking table: an API key must never be mistaken for a package path.
func TestCollectPushArtifactsIgnoresApiKeyValue(t *testing.T) {
	workingDirectory := t.TempDir()
	packagePath := writePackage(t, workingDirectory, "tool.1.2.3.nupkg", "tool")

	for _, keyFlag := range []string{"-k", "--key", "--apikey", "--api-key"} {
		t.Run(keyFlag, func(t *testing.T) {
			artifacts, err := CollectPushArtifacts(workingDirectory,
				[]string{keyFlag, "user:token", packagePath}, "choco-local")
			require.NoError(t, err)
			require.Len(t, artifacts, 1)
			assert.Equal(t, "tool.1.2.3.nupkg", artifacts[0].Name)
		})
	}
}
