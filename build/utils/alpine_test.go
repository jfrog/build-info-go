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

func TestBuildDepGraph_FromDepends(t *testing.T) {
	pkgs := []AlpinePackage{
		{Name: "curl", Version: "8.5.0-r0", Depends: []string{"musl", "libssl3"}},
		{Name: "musl", Version: "1.2.4-r2"},
		{Name: "libssl3", Version: "3.1.4-r5"},
	}
	graph := BuildDepGraph(pkgs, BuildProviderIndex(pkgs))
	assert.Equal(t, []string{"musl", "libssl3"}, graph["curl"])
	assert.NotContains(t, graph, "musl", "packages with no Depends must be omitted")
}

func TestBuildDepGraph_ResolvesSharedObjectDeps(t *testing.T) {
	pkgs := []AlpinePackage{
		{
			Name:    "curl",
			Version: "8.5.0-r0",
			Depends: []string{"so:libc.musl-x86_64.so.1", "so:libcurl.so.4"},
		},
		{
			Name:     "libcurl",
			Version:  "8.5.0-r0",
			Provides: []string{"so:libcurl.so.4"},
		},
		{
			Name:     "musl",
			Version:  "1.2.4-r2",
			Provides: []string{"so:libc.musl-x86_64.so.1"},
		},
	}
	graph := BuildDepGraph(pkgs, BuildProviderIndex(pkgs))
	assert.Equal(t, []string{"musl", "libcurl"}, graph["curl"],
		"so: dependencies must resolve to the packages providing them")
}

func TestBuildDepGraph_DropsUnprovidedAndSelfEdges(t *testing.T) {
	pkgs := []AlpinePackage{
		{
			Name:     "curl",
			Version:  "8.5.0-r0",
			Depends:  []string{"so:libnotinstalled.so.9", "cmd:curl", "musl"},
			Provides: []string{"cmd:curl"},
		},
		{Name: "musl", Version: "1.2.4-r2"},
	}
	graph := BuildDepGraph(pkgs, BuildProviderIndex(pkgs))
	assert.Equal(t, []string{"musl"}, graph["curl"],
		"tokens nothing provides and self-provided tokens must be dropped")
}

func TestBuildDepGraph_DeduplicatesResolvedChildren(t *testing.T) {
	pkgs := []AlpinePackage{
		{
			Name:    "app",
			Version: "1.0.0-r0",
			Depends: []string{"so:libz.so.1", "cmd:zlib-tool"},
		},
		{
			Name:     "zlib",
			Version:  "1.3.1-r2",
			Provides: []string{"so:libz.so.1", "cmd:zlib-tool"},
		},
	}
	graph := BuildDepGraph(pkgs, BuildProviderIndex(pkgs))
	assert.Equal(t, []string{"zlib"}, graph["app"])
}

func TestBuildDepGraph_Empty(t *testing.T) {
	assert.Empty(t, BuildDepGraph(nil, nil))
	assert.Empty(t, BuildDepGraph([]AlpinePackage{{Name: "a", Version: "1"}}, nil))
}

func TestPackagesFromArchivesDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"curl-8.5.0-r0.apk",
		"ca-certificates-bundle-20240226-r0.apk", // dashes in the package name
		// apk's real --cache-dir output: <name>-<version>.<8-hex-char-content-hash>.apk.
		"libcurl-8.12.1-r0.915f2596.apk",
		"APKINDEX.a1b2c3.tar.gz", // index, not a package
		"notes.txt",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644))
	}

	pkgs, err := PackagesFromArchivesDir(dir)
	require.NoError(t, err)
	require.Len(t, pkgs, 3, "only .apk archives with a parsable name must be returned")

	byName := make(map[string]string, len(pkgs))
	for _, pkg := range pkgs {
		byName[pkg.Name] = pkg.Version
	}
	assert.Equal(t, "8.5.0-r0", byName["curl"])
	assert.Equal(t, "20240226-r0", byName["ca-certificates-bundle"],
		"the version must be split on the -r<revision> suffix, not the first dash")
	assert.Equal(t, "8.12.1-r0", byName["libcurl"],
		"the real apk cache filename format (with a content-hash segment before .apk) must be recognized")
}

func TestPackagesFromArchivesDir_EmptyAndMissing(t *testing.T) {
	pkgs, err := PackagesFromArchivesDir("")
	require.NoError(t, err)
	assert.Empty(t, pkgs)

	pkgs, err = PackagesFromArchivesDir(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err, "a missing download directory is not an error")
	assert.Empty(t, pkgs)
}

func TestBuildProviderIndex(t *testing.T) {
	pkgs := []AlpinePackage{
		{Name: "musl", Version: "1.2.4-r2", Provides: []string{"so:libc.musl-x86_64.so.1"}},
		{Name: "libcurl", Version: "8.5.0-r0", Provides: []string{"so:libcurl.so.4"}},
	}
	index := BuildProviderIndex(pkgs)

	assert.Equal(t, "musl", index["musl"], "a package name must map to itself")
	assert.Equal(t, "musl", index["so:libc.musl-x86_64.so.1"])
	assert.Equal(t, "libcurl", index["so:libcurl.so.4"])
	assert.Empty(t, index["so:libmissing.so.1"])
}

func TestBuildProviderIndex_PackageNameWinsOverProvides(t *testing.T) {
	pkgs := []AlpinePackage{
		{Name: "busybox", Version: "1.36.1-r5", Provides: []string{"sh"}},
		{Name: "sh", Version: "1.0.0-r0"},
	}
	index := BuildProviderIndex(pkgs)
	assert.Equal(t, "sh", index["sh"])
}

func TestResolveDependencyProvider(t *testing.T) {
	providers := map[string]string{"so:libcurl.so.4": "libcurl", "musl": "musl"}
	assert.Equal(t, "libcurl", ResolveDependencyProvider("so:libcurl.so.4", providers))
	assert.Equal(t, "musl", ResolveDependencyProvider("musl", providers))
	assert.Empty(t, ResolveDependencyProvider("so:libunknown.so.1", providers))
	assert.Empty(t, ResolveDependencyProvider("", providers))
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
	content := `P:musl
V:1.2.4-r2
A:x86_64
I:655360
C:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=
D:so:libc.so.0
p:so:libc.musl-x86_64.so.1=1
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
p:cmd:curl=8.5.0-r0
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

	assert.Equal(t, "musl", pkgs[0].Name)
	assert.Equal(t, "1.2.4-r2", pkgs[0].Version)
	assert.Equal(t, "x86_64", pkgs[0].Arch)
	assert.Equal(t, 655360, pkgs[0].Size)
	assert.Equal(t, []string{"so:libc.so.0"}, pkgs[0].Depends, "D: field: so: prefix must be preserved")
	assert.Equal(t, []string{"so:libc.musl-x86_64.so.1"}, pkgs[0].Provides, "p: field: version must be stripped")
	assert.Equal(t, []string{"/lib/ld-musl-x86_64.so.1", "/lib/libc.musl-x86_64.so.1"}, pkgs[0].Files, "F:+R: must construct absolute paths")

	assert.Equal(t, "curl", pkgs[1].Name)
	assert.Equal(t, "8.5.0-r0", pkgs[1].Version)
	assert.Equal(t, []string{"musl", "libssl3", "so:libcrypto.so.3"}, pkgs[1].Depends,
		"D: field: version constraints stripped, provider prefixes preserved")
	assert.Equal(t, []string{"cmd:curl"}, pkgs[1].Provides)
	assert.Equal(t, []string{"/usr/bin/curl", "/usr/lib/libcurl.so.4"}, pkgs[1].Files,
		"F:+R: path must reset when a new F: line appears")
}

func TestParseInstalledDB_Deduplication(t *testing.T) {
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

func TestParseInstalledDB_IncompleteStanzaSkipped(t *testing.T) {
	content := `V:1.0-r0
A:x86_64

P:curl
V:8.5.0-r0
A:x86_64

`
	tmpFile, err := os.CreateTemp("", "apk-installed-incomplete-*.db")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.Remove(tmpFile.Name())) }()

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	pkgs, err := parseInstalledDB(tmpFile.Name())
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "curl", pkgs[0].Name)
}

// ─── ChecksumsFromCache ──────────────────────────────────────────────────────

func TestChecksumsFromCache_Miss(t *testing.T) {
	emptyDir := t.TempDir()
	pkg := AlpinePackage{Name: "curl", Version: "8.5.0-r0"}
	checksums, err := ChecksumsFromCache(pkg, emptyDir)
	require.NoError(t, err, "cache miss should not be an error")
	assert.Empty(t, checksums, "cache miss should return an empty map")
}

func TestChecksumsFromCache_EmptyCacheDir(t *testing.T) {
	pkg := AlpinePackage{Name: "curl", Version: "8.5.0-r0"}
	checksums, err := ChecksumsFromCache(pkg, "")
	require.NoError(t, err)
	assert.Empty(t, checksums, "empty cacheDir must not fall back to a system path")
}

func TestChecksumsFromCache_Hit(t *testing.T) {
	cacheDir := t.TempDir()
	apkPath := filepath.Join(cacheDir, "curl-8.5.0-r0.apk")
	require.NoError(t, os.WriteFile(apkPath, []byte("fake apk content for checksum test"), 0644))

	pkg := AlpinePackage{Name: "curl", Version: "8.5.0-r0"}
	checksums, err := ChecksumsFromCache(pkg, cacheDir)
	require.NoError(t, err)
	assert.NotEmpty(t, checksums, "cache hit should return non-empty checksums")

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
		{name: "so: prefix preserved", input: "so:libssl.so.3", expected: "so:libssl.so.3"},
		{name: "so: with version constraint", input: "so:libz.so.1>=1.0", expected: "so:libz.so.1"},
		{name: "so: provides with version", input: "so:libcurl.so.4=8.5.0", expected: "so:libcurl.so.4"},
		{name: "cmd: prefix preserved", input: "cmd:curl=8.5.0-r0", expected: "cmd:curl"},
		{name: "pc: prefix preserved", input: "pc:openssl=3.1.4", expected: "pc:openssl"},
		{name: "conflict marker returns empty", input: "!curl", expected: ""},
		{name: "empty string returns empty", input: "", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, parseDependencySpec(tc.input))
		})
	}
}
