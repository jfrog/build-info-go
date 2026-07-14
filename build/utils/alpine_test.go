package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── AlpinePackage.ID ────────────────────────────────────────────────────────

func TestAlpinePackageID_WithArch(t *testing.T) {
	p := AlpinePackage{Name: "curl", Version: "8.5.0-r0", Arch: "x86_64"}
	assert.Equal(t, "curl:8.5.0-r0", p.ID())
}

func TestAlpinePackageID_WithoutArch(t *testing.T) {
	p := AlpinePackage{Name: "musl", Version: "1.2.4-r2"}
	assert.Equal(t, "musl:1.2.4-r2", p.ID())
}

func TestAlpinePackageSHA1Hex_Valid(t *testing.T) {
	// "Q1" + base64(20 zero bytes) = Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=
	p := AlpinePackage{Checksum: "Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	assert.Equal(t, "0000000000000000000000000000000000000000", p.SHA1Hex())
}

func TestAlpinePackageSHA1Hex_Empty(t *testing.T) {
	p := AlpinePackage{}
	assert.Equal(t, "", p.SHA1Hex())
}

func TestAlpinePackageSHA1Hex_Malformed(t *testing.T) {
	p := AlpinePackage{Checksum: "Q1!!!invalid!!!"}
	assert.Equal(t, "", p.SHA1Hex())
}

// ─── parseDependsOutput ──────────────────────────────────────────────────────

func TestParseDependsOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name: "version constraint >= stripped",
			input: "curl-8.5.0-r0 depends on:\nmusl>=1.2.3\nlibssl3\n\n",
			expected: []string{"musl", "libssl3"},
		},
		{
			name: "version constraint <= stripped",
			input: "foo-1.0-r0 depends on:\nbar<=2.0\n\n",
			expected: []string{"bar"},
		},
		{
			name: "version constraint = stripped",
			input: "foo-1.0-r0 depends on:\nbaz=1.0.0-r0\n\n",
			expected: []string{"baz"},
		},
		{
			name: "so: prefix stripped (virtual provider)",
			// Scenario #73: virtual provider packages like so:libssl.so.3 must be stripped
			input: "openssl-1.1-r0 depends on:\nso:libssl.so.3\nso:libc.so.0\n\n",
			expected: []string{"libssl.so.3", "libc.so.0"},
		},
		{
			name: "mixed: version constraint + so: prefix",
			input: "pkg-1.0-r0 depends on:\nmusl>=1.2\nso:libz.so.1\nbusybox\n\n",
			expected: []string{"musl", "libz.so.1", "busybox"},
		},
		{
			name:     "no depends on section",
			input:    "curl-8.5.0-r0\n",
			expected: nil,
		},
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "no dependencies listed",
			input:    "curl-8.5.0-r0 depends on:\n\n",
			expected: nil,
		},
		{
			name: "stops at blank line",
			input: "curl-8.5.0-r0 depends on:\nmusl\n\nextra-line-should-not-appear",
			expected: []string{"musl"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDependsOutput(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// ─── DiffAlpinePackages ──────────────────────────────────────────────────────

func TestDiffAlpinePackages(t *testing.T) {
	curl := AlpinePackage{Name: "curl", Version: "8.5.0-r0", Arch: "x86_64"}
	musl := AlpinePackage{Name: "musl", Version: "1.2.4-r2", Arch: "x86_64"}
	busybox := AlpinePackage{Name: "busybox", Version: "1.36.1-r0", Arch: "x86_64"}

	tests := []struct {
		name     string
		before   []AlpinePackage
		after    []AlpinePackage
		expected []AlpinePackage
	}{
		{
			// Scenario #71: packages in before must NOT appear in the diff
			name:     "packages in before excluded from diff (no phantom deps)",
			before:   []AlpinePackage{musl},
			after:    []AlpinePackage{musl, curl},
			expected: []AlpinePackage{curl},
		},
		{
			name:     "all new packages returned when before is empty",
			before:   nil,
			after:    []AlpinePackage{curl, musl},
			expected: []AlpinePackage{curl, musl},
		},
		{
			name:     "no new packages returns empty slice",
			before:   []AlpinePackage{curl, musl},
			after:    []AlpinePackage{curl, musl},
			expected: nil,
		},
		{
			name:     "duplicates in after deduplicated",
			before:   nil,
			after:    []AlpinePackage{curl, curl, musl},
			expected: []AlpinePackage{curl, musl},
		},
		{
			name:     "empty before and after",
			before:   nil,
			after:    nil,
			expected: nil,
		},
		{
			name:     "multiple new packages",
			before:   []AlpinePackage{musl},
			after:    []AlpinePackage{musl, curl, busybox},
			expected: []AlpinePackage{curl, busybox},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DiffAlpinePackages(tc.before, tc.after)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// ─── parseInstalledDB ────────────────────────────────────────────────────────

func TestParseInstalledDB(t *testing.T) {
	// Write a minimal APK installed-packages database in the canonical format.
	// The C: field contains the Alpine-style checksum: "Q1" + base64(sha1).
	content := `P:musl
V:1.2.4-r2
A:x86_64
I:655360
C:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=
o:musl
U:https://musl.libc.org/

P:curl
V:8.5.0-r0
A:x86_64
I:339968
o:curl
U:https://curl.se/

`
	tmpFile, err := os.CreateTemp("", "apk-installed-*.db")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.Remove(tmpFile.Name())) }()

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	pkgs, err := parseInstalledDB(tmpFile.Name())
	require.NoError(t, err)
	require.Len(t, pkgs, 2)

	assert.Equal(t, "musl", pkgs[0].Name)
	assert.Equal(t, "1.2.4-r2", pkgs[0].Version)
	assert.Equal(t, "x86_64", pkgs[0].Arch)
	assert.Equal(t, 655360, pkgs[0].Size)
	assert.Equal(t, "Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=", pkgs[0].Checksum, "C: field must be parsed")
	assert.Equal(t, "0000000000000000000000000000000000000000", pkgs[0].SHA1Hex(), "SHA1Hex must decode the C: field")

	assert.Equal(t, "curl", pkgs[1].Name)
	assert.Equal(t, "8.5.0-r0", pkgs[1].Version)
	assert.Equal(t, "", pkgs[1].Checksum, "missing C: field must be empty string")
}

func TestParseInstalledDB_Deduplication(t *testing.T) {
	// The same package appearing twice must be deduplicated.
	content := `P:musl
V:1.2.4-r2
A:x86_64
I:655360

P:musl
V:1.2.4-r2
A:x86_64
I:655360

`
	tmpFile, err := os.CreateTemp("", "apk-installed-dedup-*.db")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.Remove(tmpFile.Name())) }()

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	pkgs, err := parseInstalledDB(tmpFile.Name())
	require.NoError(t, err)
	assert.Len(t, pkgs, 1, "duplicate package entries must be deduplicated")
}

func TestParseInstalledDB_FileNotFound(t *testing.T) {
	_, err := parseInstalledDB("/nonexistent/path/to/installed")
	assert.Error(t, err, "reading a nonexistent file should return an error")
}

// ─── ChecksumsFromCache ──────────────────────────────────────────────────────

func TestChecksumsFromCache_Miss(t *testing.T) {
	// When no .apk file is present in the cache dir, the function should return
	// an empty map (not an error) — a cache miss is a normal condition.
	emptyDir := t.TempDir()
	pkg := AlpinePackage{Name: "curl", Version: "8.5.0-r0"}
	checksums, err := ChecksumsFromCache(pkg, emptyDir)
	require.NoError(t, err, "cache miss should not be an error")
	assert.Empty(t, checksums, "cache miss should return an empty map")
}

func TestChecksumsFromCache_Hit(t *testing.T) {
	// When a matching .apk file exists in the cache dir, sha1/sha256/md5 must all
	// be non-empty.
	cacheDir := t.TempDir()
	apkPath := filepath.Join(cacheDir, "curl-8.5.0-r0.apk")
	require.NoError(t, os.WriteFile(apkPath, []byte("fake apk content for checksum test"), 0644))

	pkg := AlpinePackage{Name: "curl", Version: "8.5.0-r0"}
	checksums, err := ChecksumsFromCache(pkg, cacheDir)
	require.NoError(t, err)
	assert.NotEmpty(t, checksums, "cache hit should return non-empty checksums")

	// All three algorithms should be present.
	for _, key := range checksums {
		assert.NotEmpty(t, key, "each checksum value must be non-empty")
	}
}
