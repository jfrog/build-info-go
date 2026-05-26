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
	hex, err := nixpkg.SriToHex("sha256-lHk9nuLEIEnCaAvriEAfIbNxpQbopIyNmSU+YZcqZl0=")
	require.NoError(t, err)
	assert.Equal(t, "94793d9ee2c42049c2680beb88401f21b371a506e8a48c8d99253e61972a665d", hex)
	assert.Len(t, hex, 64)

	_, err = nixpkg.SriToHex("md5-abc")
	assert.Error(t, err)
}

func TestNewNixFlexPack(t *testing.T) {
	config := nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	}

	collector, err := nixpkg.NewNixFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)

	deps, err := collector.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps)

	bi, err := collector.CollectBuildInfo("test", "1")
	require.NoError(t, err)
	require.Len(t, bi.Modules, 1)
	assert.Equal(t, "nix", string(bi.Modules[0].Type))
	assert.Empty(t, bi.Modules[0].Dependencies)
}

func TestNixFlexPackBuildInfoFields(t *testing.T) {
	config := nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	}

	collector, err := nixpkg.NewNixFlexPack(config)
	require.NoError(t, err)

	bi, err := collector.CollectBuildInfo("my-build", "42")
	require.NoError(t, err)

	assert.Equal(t, "my-build", bi.Name)
	assert.Equal(t, "42", bi.Number)
	assert.NotNil(t, bi.Agent)
	assert.Equal(t, "build-info-go", bi.Agent.Name)
	assert.NotNil(t, bi.BuildAgent)
	assert.Equal(t, "Nix", bi.BuildAgent.Name)
	assert.NotEmpty(t, bi.Started)
}

func TestNixFlexPackScopes(t *testing.T) {
	config := nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	}

	collector, err := nixpkg.NewNixFlexPack(config)
	require.NoError(t, err)

	scopes := collector.CalculateScopes()
	assert.Equal(t, []string{"runtime"}, scopes)
}

func TestExtractStoreHash_NoDash(t *testing.T) {
	result := nixpkg.ExtractStoreHash("nodashhere")
	assert.Equal(t, "nodashhere", result)
}

func TestStorePathToDepID_NoVersion(t *testing.T) {
	result := nixpkg.StorePathToDepID("/nix/store/abc123def456ghij789klmnopqrstuv-version-check-hook")
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

func TestNewNixFlexPack_DefaultDir(t *testing.T) {
	config := nixpkg.NixConfig{}
	collector, err := nixpkg.NewNixFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)
}

func TestNixFlexPack_GetDependencyGraph_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	graph, err := collector.GetDependencyGraph()
	require.NoError(t, err)
	assert.Empty(t, graph)
}

func TestNixFlexPack_ParseDependencyToList_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	depList := collector.ParseDependencyToList()
	assert.Empty(t, depList)
}

func TestNixFlexPack_GetDependency_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	result := collector.GetDependency()
	assert.Contains(t, result, "Project:")
	assert.Contains(t, result, "Dependencies:")
}

func TestNixFlexPack_CalculateChecksum_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	checksums := collector.CalculateChecksum()
	assert.Empty(t, checksums)
}

func TestNixFlexPack_CalculateRequestedBy_Empty(t *testing.T) {
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	reqBy := collector.CalculateRequestedBy()
	assert.Empty(t, reqBy)
}

func TestNixFlexPack_ProjectNameFromDir(t *testing.T) {
	tmpDir := t.TempDir()
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: tmpDir,
	})
	require.NoError(t, err)

	bi, err := collector.CollectBuildInfo("test", "1")
	require.NoError(t, err)
	require.Len(t, bi.Modules, 1)
	assert.NotEmpty(t, bi.Modules[0].Id)
}

func TestNixFlexPack_CollectStorePathDependencies_EmptyInput(t *testing.T) {
	collector, err := nixpkg.NewNixFlexPack(nixpkg.NixConfig{
		WorkingDirectory: t.TempDir(),
	})
	require.NoError(t, err)

	err = collector.CollectStorePathDependencies()
	require.NoError(t, err)

	deps, err := collector.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps)
}
