//go:build linux

package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/gofrog/crypto"
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
	// D: lists runtime dependencies; F: and R: together form installed file paths.
	content := `P:musl
V:1.2.4-r2
A:x86_64
I:655360
C:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=
D:so:libc.so.0
F:lib
R:ld-musl-x86_64.so.1
R:libc.musl-x86_64.so.1
o:musl
U:https://musl.libc.org/

P:curl
V:8.5.0-r0
A:x86_64
I:339968
D:musl>=1.2.3 libssl3 so:libcrypto.so.3
F:usr/bin
R:curl
F:usr/lib
R:libcurl.so.4
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

	// musl assertions
	assert.Equal(t, "musl", pkgs[0].Name)
	assert.Equal(t, "1.2.4-r2", pkgs[0].Version)
	assert.Equal(t, "x86_64", pkgs[0].Arch)
	assert.Equal(t, 655360, pkgs[0].Size)
	assert.Equal(t, "Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=", pkgs[0].Checksum, "C: field must be parsed")
	assert.Equal(t, "0000000000000000000000000000000000000000", pkgs[0].SHA1Hex(), "SHA1Hex must decode the C: field")
	assert.Equal(t, []string{"libc.so.0"}, pkgs[0].Depends, "D: field: so: prefix must be stripped")
	assert.Equal(t, []string{"/lib/ld-musl-x86_64.so.1", "/lib/libc.musl-x86_64.so.1"}, pkgs[0].Files, "F:+R: must construct absolute paths")

	// curl assertions
	assert.Equal(t, "curl", pkgs[1].Name)
	assert.Equal(t, "8.5.0-r0", pkgs[1].Version)
	assert.Equal(t, "", pkgs[1].Checksum, "missing C: field must be empty string")
	assert.Equal(t, []string{"musl", "libssl3", "libcrypto.so.3"}, pkgs[1].Depends,
		"D: field: version constraints and so: prefix must be stripped")
	assert.Equal(t, []string{"/usr/bin/curl", "/usr/lib/libcurl.so.4"}, pkgs[1].Files,
		"F:+R: path must reset when a new F: line appears")
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

// ─── parseDependencySpec ─────────────────────────────────────────────────────

func TestParseDependencySpec(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "plain name unchanged", input: "musl", expected: "musl"},
		{name: ">= stripped", input: "musl>=1.2.3", expected: "musl"},
		{name: "<= stripped", input: "openssl<=3.0", expected: "openssl"},
		{name: "= stripped", input: "bash=5.2.0-r0", expected: "bash"},
		{name: "> stripped", input: "zlib>1.3", expected: "zlib"},
		{name: "< stripped", input: "glibc<2.40", expected: "glibc"},
		{name: "so: prefix stripped", input: "so:libssl.so.3", expected: "libssl.so.3"},
		{name: "so: with version constraint", input: "so:libz.so.1>=1.0", expected: "libz.so.1"},
		{name: "conflict marker returns empty", input: "!curl", expected: ""},
		{name: "empty string returns empty", input: "", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, parseDependencySpec(tc.input))
		})
	}
}

// ─── ChecksumsFromInstalledFiles ─────────────────────────────────────────────

func TestChecksumsFromInstalledFiles_EmptyFiles(t *testing.T) {
	pkg := AlpinePackage{Name: "curl", Version: "8.5.0-r0"}
	checksums, err := ChecksumsFromInstalledFiles(pkg)
	require.NoError(t, err)
	assert.Empty(t, checksums, "empty Files slice must return empty map, not an error")
}

func TestChecksumsFromInstalledFiles_NonexistentFiles(t *testing.T) {
	pkg := AlpinePackage{
		Name:    "ghost",
		Version: "1.0-r0",
		Files:   []string{"/nonexistent/path/foo.so", "/another/missing.so"},
	}
	checksums, err := ChecksumsFromInstalledFiles(pkg)
	require.NoError(t, err, "all files missing must return empty map, not an error")
	assert.Empty(t, checksums)
}

func TestChecksumsFromInstalledFiles_WithFiles(t *testing.T) {
	// Create two temporary files that act as "installed" package files.
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "libfoo.so.1")
	file2 := filepath.Join(tmpDir, "foo")
	require.NoError(t, os.WriteFile(file1, []byte("libfoo content"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("foo binary content"), 0644))

	pkg := AlpinePackage{
		Name:    "foo",
		Version: "1.0-r0",
		Files:   []string{file1, file2},
	}
	checksums, err := ChecksumsFromInstalledFiles(pkg)
	require.NoError(t, err)
	require.NotEmpty(t, checksums, "checksums must be non-empty when files exist")

	// All three algorithms must be present and non-empty.
	assert.NotEmpty(t, checksums[crypto.SHA1], "SHA1 must be present")
	assert.NotEmpty(t, checksums[crypto.SHA256], "SHA256 must be present")
	assert.NotEmpty(t, checksums[crypto.MD5], "MD5 must be present")
}

func TestChecksumsFromInstalledFiles_Deterministic(t *testing.T) {
	// The same set of files in different order must produce identical checksums.
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "a.so")
	file2 := filepath.Join(tmpDir, "b.so")
	require.NoError(t, os.WriteFile(file1, []byte("aaa"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("bbb"), 0644))

	pkgForward := AlpinePackage{Name: "x", Version: "1.0-r0", Files: []string{file1, file2}}
	pkgReverse := AlpinePackage{Name: "x", Version: "1.0-r0", Files: []string{file2, file1}}

	c1, err1 := ChecksumsFromInstalledFiles(pkgForward)
	c2, err2 := ChecksumsFromInstalledFiles(pkgReverse)
	require.NoError(t, err1)
	require.NoError(t, err2)

	assert.Equal(t, c1, c2, "checksum must be independent of Files slice order")
}

func TestChecksumsFromInstalledFiles_DirectoriesSkipped(t *testing.T) {
	// Directories within Files must be silently skipped.
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))
	realFile := filepath.Join(tmpDir, "real.so")
	require.NoError(t, os.WriteFile(realFile, []byte("data"), 0644))

	pkg := AlpinePackage{
		Name:    "test",
		Version: "1.0-r0",
		Files:   []string{subDir, realFile},
	}
	checksums, err := ChecksumsFromInstalledFiles(pkg)
	require.NoError(t, err)
	assert.NotEmpty(t, checksums, "directory entries must be skipped; real file must still be hashed")
}
