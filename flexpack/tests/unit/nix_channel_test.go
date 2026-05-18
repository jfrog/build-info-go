package unit

import (
	"testing"

	nixpkg "github.com/jfrog/build-info-go/flexpack/nix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractStoreHash(t *testing.T) {
	tests := []struct {
		storePath string
		expected  string
	}{
		{"/nix/store/yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3", "yalw1pbrzmzk66phdkhslqh79pvbb67k"},
		{"/nix/store/xvmhkpvfvmy4sfdkqwg9inq3qkpnx81b-libiconv-109.100.2", "xvmhkpvfvmy4sfdkqwg9inq3qkpnx81b"},
		{"yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3", "yalw1pbrzmzk66phdkhslqh79pvbb67k"},
	}
	for _, tt := range tests {
		t.Run(tt.storePath, func(t *testing.T) {
			assert.Equal(t, tt.expected, nixpkg.ExtractStoreHash(tt.storePath))
		})
	}
}

func TestExtractPackageName(t *testing.T) {
	tests := []struct {
		storePath string
		expected  string
	}{
		{"/nix/store/yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3", "hello-2.12.3"},
		{"/nix/store/xvmhkpvfvmy4sfdkqwg9inq3qkpnx81b-libiconv-109.100.2", "libiconv-109.100.2"},
		{"/nix/store/f700nj7wlwg441h39gkq29qbviy99sgq-bash-5.3p9", "bash-5.3p9"},
	}
	for _, tt := range tests {
		t.Run(tt.storePath, func(t *testing.T) {
			assert.Equal(t, tt.expected, nixpkg.ExtractPackageName(tt.storePath))
		})
	}
}

func TestExtractNameAndVersion(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		version string
	}{
		{"hello-2.12.3", "hello", "2.12.3"},
		{"libiconv-109.100.2", "libiconv", "109.100.2"},
		{"glibc-2.40-66", "glibc-2.40", "66"},
		{"bash-5.3p9", "bash", "5.3p9"},
		{"bash", "bash", ""},
		{"version-check-hook", "version-check-hook", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, version := nixpkg.ExtractNameAndVersion(tt.input)
			assert.Equal(t, tt.name, name)
			assert.Equal(t, tt.version, version)
		})
	}
}

func TestStorePathToDepID(t *testing.T) {
	tests := []struct {
		storePath string
		expected  string
	}{
		{"/nix/store/yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3", "hello:2.12.3"},
		{"/nix/store/xvmhkpvfvmy4sfdkqwg9inq3qkpnx81b-libiconv-109.100.2", "libiconv:109.100.2"},
		{"/nix/store/f700nj7wlwg441h39gkq29qbviy99sgq-bash-5.3p9", "bash:5.3p9"},
	}
	for _, tt := range tests {
		t.Run(tt.storePath, func(t *testing.T) {
			assert.Equal(t, tt.expected, nixpkg.StorePathToDepID(tt.storePath))
		})
	}
}

func TestSriToHex(t *testing.T) {
	// Known conversion: sha256-lHk9nuLEIEnCaAvriEAfIbNxpQbopIyNmSU+YZcqZl0=
	hex, err := nixpkg.SriToHex("sha256-lHk9nuLEIEnCaAvriEAfIbNxpQbopIyNmSU+YZcqZl0=")
	require.NoError(t, err)
	assert.Equal(t, "94793d9ee2c42049c2680beb88401f21b371a506e8a48c8d99253e61972a665d", hex)
	assert.Len(t, hex, 64) // sha256 hex is 64 chars

	// Invalid format
	_, err = nixpkg.SriToHex("md5-abc")
	assert.Error(t, err)
}

func TestParseNarInfo(t *testing.T) {
	content := `StorePath: /nix/store/yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3
URL: nar/0wzchzrgwsiwww56kqxdlcny85rav7x3knsbfa0ig5sp1zpkj4c7.nar.xz
Compression: xz
FileHash: sha256:0wzchzrgwsiwww56kqxdlcny85rav7x3knsbfa0ig5sp1zpkj4c7
FileSize: 24856
NarHash: sha256:0y8xkifj3i4a5fnyjl7cic2ykjjjx1pj0q09ghin26s9bs5vs7rh
NarSize: 113096
References: xvmhkpvfvmy4sfdkqwg9inq3qkpnx81b-libiconv-109.100.2
Deriver: k32h9g0pzvvihwwdq4yj719lwf5np9p5-hello-2.12.3.drv
Sig: cache.nixos.org-1:X7kHz7Q/fxM0/sEt2329cKGG5xrLQ97sUauarc1yBpDXVcI3tYyeSu/TQq37as9QqHYEs93ROoqGHzzrI8IQCg==`

	info, err := nixpkg.ParseNarInfo(content)
	require.NoError(t, err)

	assert.Equal(t, "/nix/store/yalw1pbrzmzk66phdkhslqh79pvbb67k-hello-2.12.3", info.StorePath)
	assert.Equal(t, "nar/0wzchzrgwsiwww56kqxdlcny85rav7x3knsbfa0ig5sp1zpkj4c7.nar.xz", info.URL)
	assert.Equal(t, "xz", info.Compression)
	assert.Equal(t, "sha256:0wzchzrgwsiwww56kqxdlcny85rav7x3knsbfa0ig5sp1zpkj4c7", info.FileHash)
	assert.Equal(t, int64(24856), info.FileSize)
	assert.Equal(t, "sha256:0y8xkifj3i4a5fnyjl7cic2ykjjjx1pj0q09ghin26s9bs5vs7rh", info.NarHash)
	assert.Equal(t, int64(113096), info.NarSize)
	assert.Equal(t, "xvmhkpvfvmy4sfdkqwg9inq3qkpnx81b-libiconv-109.100.2", info.References)
	assert.Equal(t, "k32h9g0pzvvihwwdq4yj719lwf5np9p5-hello-2.12.3.drv", info.Deriver)
	assert.Contains(t, info.Sig, "cache.nixos.org-1:")
}

func TestNewNixChannelCollector(t *testing.T) {
	config := nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
		ChannelName:      "nixos-25.11",
	}

	collector, err := nixpkg.NewNixChannelCollector(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)

	// With no store paths collected, should have empty deps
	deps, err := collector.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps)

	// Build info should still work with 0 deps
	bi, err := collector.CollectBuildInfo("test", "1")
	require.NoError(t, err)
	require.Len(t, bi.Modules, 1)
	assert.Equal(t, "nix", string(bi.Modules[0].Type))
	assert.Empty(t, bi.Modules[0].Dependencies)
}

func TestNixChannelCollectorBuildInfoFields(t *testing.T) {
	config := nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	}

	collector, err := nixpkg.NewNixChannelCollector(config)
	require.NoError(t, err)

	bi, err := collector.CollectBuildInfo("my-build", "42")
	require.NoError(t, err)

	assert.Equal(t, "my-build", bi.Name)
	assert.Equal(t, "42", bi.Number)
	assert.NotNil(t, bi.Agent)
	assert.Equal(t, "nix", bi.Agent.Name)
	assert.NotNil(t, bi.BuildAgent)
	assert.Equal(t, "Generic", bi.BuildAgent.Name)
	assert.NotEmpty(t, bi.Started)
}

func TestNixChannelCollectorScopes(t *testing.T) {
	config := nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	}

	collector, err := nixpkg.NewNixChannelCollector(config)
	require.NoError(t, err)

	scopes := collector.CalculateScopes()
	assert.Equal(t, []string{"runtime"}, scopes)
}

// ==================== Additional narinfo_parser Tests ====================

func TestParseNarInfo_EmptyContent(t *testing.T) {
	info, err := nixpkg.ParseNarInfo("")
	require.NoError(t, err)
	assert.Empty(t, info.StorePath)
	assert.Empty(t, info.URL)
	assert.Equal(t, int64(0), info.FileSize)
}

func TestParseNarInfo_MalformedLines(t *testing.T) {
	// Lines without ":" should be skipped; valid lines still parsed
	content := `this line has no colon
StorePath: /nix/store/abc-hello-1.0
another bad line
Compression: xz`

	info, err := nixpkg.ParseNarInfo(content)
	require.NoError(t, err)
	assert.Equal(t, "/nix/store/abc-hello-1.0", info.StorePath)
	assert.Equal(t, "xz", info.Compression)
}

func TestParseNarInfo_WithSystem(t *testing.T) {
	content := `StorePath: /nix/store/abc-hello-1.0
System: x86_64-linux
NarSize: 42`

	info, err := nixpkg.ParseNarInfo(content)
	require.NoError(t, err)
	assert.Equal(t, "x86_64-linux", info.System)
	assert.Equal(t, int64(42), info.NarSize)
}

func TestExtractStoreHash_NoDash(t *testing.T) {
	// A basename with no dash returns whole basename
	result := nixpkg.ExtractStoreHash("nodashhere")
	assert.Equal(t, "nodashhere", result)
}

func TestStorePathToDepID_NoVersion(t *testing.T) {
	// Store path for a package with no version component
	result := nixpkg.StorePathToDepID("/nix/store/abc123def456ghij789klmnopqrstuv-version-check-hook")
	// "version-check-hook" has no trailing "-digit", so version is ""
	assert.Equal(t, "version-check-hook", result)
}

func TestSriToHex_InvalidBase64(t *testing.T) {
	_, err := nixpkg.SriToHex("sha256-!!!invalidbase64!!!")
	assert.Error(t, err)
}

func TestSriToHex_DifferentAlgorithm(t *testing.T) {
	_, err := nixpkg.SriToHex("sha512-abc")
	assert.Error(t, err, "only sha256 should be supported")
}

// ==================== Additional NixChannelCollector Tests ====================

func TestNewNixChannelCollector_DefaultDir(t *testing.T) {
	// Empty WorkingDirectory should default to current directory
	config := nixpkg.NixChannelConfig{}
	collector, err := nixpkg.NewNixChannelCollector(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)
}

func TestNixChannelCollector_GetDependencyGraph_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	graph, err := collector.GetDependencyGraph()
	require.NoError(t, err)
	assert.Empty(t, graph)
}

func TestNixChannelCollector_ParseDependencyToList_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	depList := collector.ParseDependencyToList()
	assert.Empty(t, depList)
}

func TestNixChannelCollector_GetDependency_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	result := collector.GetDependency()
	assert.Contains(t, result, "Project:")
	assert.Contains(t, result, "Dependencies:")
}

func TestNixChannelCollector_CalculateChecksum_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	checksums := collector.CalculateChecksum()
	assert.Empty(t, checksums)
}

func TestNixChannelCollector_CalculateRequestedBy_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	reqBy := collector.CalculateRequestedBy()
	assert.Empty(t, reqBy)
}

func TestNixChannelCollector_ProjectNameFromDir(t *testing.T) {
	tmpDir := t.TempDir()
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: tmpDir,
	})
	require.NoError(t, err)

	bi, err := collector.CollectBuildInfo("test", "1")
	require.NoError(t, err)
	require.Len(t, bi.Modules, 1)
	// Module ID should be the directory's basename
	assert.NotEmpty(t, bi.Modules[0].Id)
}

func TestNixChannelCollector_CollectStorePathDependencies_EmptyInput(t *testing.T) {
	collector, err := nixpkg.NewNixChannelCollector(nixpkg.NixChannelConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	// Empty store paths → no-op, no error
	err = collector.CollectStorePathDependencies()
	require.NoError(t, err)

	deps, err := collector.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps)
}
