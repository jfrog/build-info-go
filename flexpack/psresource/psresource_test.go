package psresource

import (
	"testing"

	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPSResourceFlexPackValidation(t *testing.T) {
	t.Run("empty WorkingDirectory returns error", func(t *testing.T) {
		_, err := NewPSResourceFlexPack(buildinfoflex.PSResourceConfig{WorkingDirectory: ""}, nil)
		require.Error(t, err)
	})

	t.Run("valid WorkingDirectory succeeds", func(t *testing.T) {
		fp, err := NewPSResourceFlexPack(buildinfoflex.PSResourceConfig{WorkingDirectory: t.TempDir()}, nil)
		require.NoError(t, err)
		require.NotNil(t, fp)
	})
}

func TestDerivePublishedPath(t *testing.T) {
	tests := []struct {
		name         string
		packageName  string
		version      string
		expectedPath string
	}{
		{
			name:         "standard package",
			packageName:  "PackageName",
			version:      "1.0.0",
			expectedPath: "packagename/1.0.0/PackageName.1.0.0.nupkg",
		},
		{
			name:         "dotted package name",
			packageName:  "Microsoft.PowerShell.PSResourceGet",
			version:      "2.5.0",
			expectedPath: "microsoft.powershell.psresourceget/2.5.0/Microsoft.PowerShell.PSResourceGet.2.5.0.nupkg",
		},
		{
			name:         "prerelease version",
			packageName:  "TestPackage",
			version:      "1.0.0-preview1",
			expectedPath: "testpackage/1.0.0-preview1/TestPackage.1.0.0-preview1.nupkg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedPath, derivePublishedPath(tc.packageName, tc.version))
		})
	}
}

// TestBuildDependencies verifies that already-resolved packages (name/version/checksum, as the
// caller would obtain them from Get-InstalledPSResource and a HEAD request to Artifactory) are
// turned into entities.Dependency records via pure data assembly, with no I/O involved.
func TestBuildDependencies(t *testing.T) {
	tests := []struct {
		name     string
		resolved []ResolvedPackage
		want     []entities.Dependency
		wantErr  bool
	}{
		{
			name:     "no packages returns nil",
			resolved: nil,
			want:     nil,
		},
		{
			name: "single resolved package",
			resolved: []ResolvedPackage{
				{
					Name:    "Pester",
					Version: "5.5.0",
					Checksum: entities.Checksum{
						Sha1:   "sha1hash",
						Sha256: "sha256hash",
						Md5:    "md5hash",
					},
				},
			},
			want: []entities.Dependency{
				{
					Id:     "Pester.5.5.0.nupkg",
					Type:   nupkgType,
					Scopes: []string{"main"},
					Checksum: entities.Checksum{
						Sha1:   "sha1hash",
						Sha256: "sha256hash",
						Md5:    "md5hash",
					},
				},
			},
		},
		{
			name: "multiple resolved packages preserve order",
			resolved: []ResolvedPackage{
				{Name: "PkgA", Version: "1.0.0", Checksum: entities.Checksum{Sha1: "a1"}},
				{Name: "PkgB", Version: "2.0.0", Checksum: entities.Checksum{Sha1: "b1"}},
			},
			want: []entities.Dependency{
				{Id: "PkgA.1.0.0.nupkg", Type: nupkgType, Scopes: []string{"main"}, Checksum: entities.Checksum{Sha1: "a1"}},
				{Id: "PkgB.2.0.0.nupkg", Type: nupkgType, Scopes: []string{"main"}, Checksum: entities.Checksum{Sha1: "b1"}},
			},
		},
		{
			name:     "missing name is an error",
			resolved: []ResolvedPackage{{Version: "1.0.0"}},
			wantErr:  true,
		},
		{
			name:     "missing version is an error",
			resolved: []ResolvedPackage{{Name: "PkgA"}},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildDependencies(tc.resolved)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestBuildPublishedArtifact verifies that an already-resolved published package (as the caller
// would obtain it from a HEAD request to Artifactory after Publish-PSResource) is turned into an
// entities.Artifact via pure data assembly, with no I/O involved.
func TestBuildPublishedArtifact(t *testing.T) {
	tests := []struct {
		name      string
		published PublishedArtifact
		want      entities.Artifact
		wantErr   bool
	}{
		{
			name: "standard package",
			published: PublishedArtifact{
				Name:    "PackageName",
				Version: "1.0.0",
				Repo:    "psresource-local",
				Checksum: entities.Checksum{
					Sha1:   "sha1hash",
					Sha256: "sha256hash",
					Md5:    "md5hash",
				},
			},
			want: entities.Artifact{
				Name:                   "PackageName.1.0.0.nupkg",
				Type:                   nupkgType,
				Path:                   "packagename/1.0.0/PackageName.1.0.0.nupkg",
				OriginalDeploymentRepo: "psresource-local",
				Checksum: entities.Checksum{
					Sha1:   "sha1hash",
					Sha256: "sha256hash",
					Md5:    "md5hash",
				},
			},
		},
		{
			name:      "missing name is an error",
			published: PublishedArtifact{Version: "1.0.0"},
			wantErr:   true,
		},
		{
			name:      "missing version is an error",
			published: PublishedArtifact{Name: "PackageName"},
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildPublishedArtifact(tc.published)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetDependencyGraph(t *testing.T) {
	fp, err := NewPSResourceFlexPack(buildinfoflex.PSResourceConfig{
		WorkingDirectory: t.TempDir(),
	}, nil)
	require.NoError(t, err)

	graph, err := fp.GetDependencyGraph()
	require.NoError(t, err)
	assert.NotNil(t, graph)
	assert.Empty(t, graph, "PSResource has no lock file, so no dependency graph is available")
}

func TestGetProjectDependencies(t *testing.T) {
	fp, err := NewPSResourceFlexPack(buildinfoflex.PSResourceConfig{
		WorkingDirectory: t.TempDir(),
	}, nil)
	require.NoError(t, err)

	deps, err := fp.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestCollectBuildInfo(t *testing.T) {
	fp, err := NewPSResourceFlexPack(buildinfoflex.PSResourceConfig{
		WorkingDirectory: t.TempDir(),
		Module:           "custom-module",
	}, nil)
	require.NoError(t, err)

	resolved := []ResolvedPackage{
		{
			Name:    "Pester",
			Version: "5.5.0",
			Checksum: entities.Checksum{
				Sha1:   "sha1hash",
				Sha256: "sha256hash",
				Md5:    "md5hash",
			},
		},
	}

	bi, err := fp.CollectBuildInfo("test-build", "123", resolved)
	require.NoError(t, err)
	assert.Equal(t, "test-build", bi.Name)
	assert.Equal(t, "123", bi.Number)

	require.Len(t, bi.Modules, 1)
	module := bi.Modules[0]
	assert.Equal(t, "custom-module", module.Id)
	assert.Equal(t, entities.Nuget, module.Type)

	require.Len(t, module.Dependencies, 1)
	dependency := module.Dependencies[0]
	assert.Equal(t, "Pester.5.5.0.nupkg", dependency.Id)
	assert.Equal(t, nupkgType, dependency.Type)
	assert.Equal(t, []string{"main"}, dependency.Scopes)
	assert.Equal(t, "sha256hash", dependency.Sha256)
}

func TestCollectBuildInfoDefaultModule(t *testing.T) {
	fp, err := NewPSResourceFlexPack(buildinfoflex.PSResourceConfig{
		WorkingDirectory: t.TempDir(),
	}, nil)
	require.NoError(t, err)

	bi, err := fp.CollectBuildInfo("test-build", "123", nil)
	require.NoError(t, err)
	require.Len(t, bi.Modules, 1)
	assert.Equal(t, "psresource-project", bi.Modules[0].Id)
	assert.Empty(t, bi.Modules[0].Dependencies)
}
