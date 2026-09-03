package apt

import (
	"os"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── CollectBuildInfo scope assignment ──────────────────────────────────────

// Every reported dependency must carry at least one scope. Essential/base-system
// packages (base-files, dpkg, libc6, …) can enter the closure via --recurse with
// no scope-carrying incoming edge — their only relations are Breaks/Conflicts/
// Replaces, which are not dependencies. They must still default to "required"
// since they are part of the installed closure. Regression for Debian trixie/
// bookworm where such packages appeared with empty Scopes.
func TestCollectBuildInfo_EssentialPkgDefaultsToRequiredScope(t *testing.T) {
	c := NewAptFlexPack(AptConfig{})
	c.rootPkgs = []string{"jq"}
	// jq → libjq1 is a real Depends edge; base-files is a header-only node with
	// no scope-carrying incoming edge.
	c.edgeGraph = map[string][]aptEdge{
		"jq":         {{child: "libjq1", scope: scopeRequired}},
		"libjq1":     nil,
		"base-files": nil,
	}
	c.allMembers = map[string]*dpkgInfo{
		"jq":         {version: "1.7.1", arch: "amd64"},
		"libjq1":     {version: "1.7.1", arch: "amd64"},
		"base-files": {version: "13.8", arch: "amd64"},
	}
	// All three carry a checksum so none is dropped by the checksum filter.
	for name, info := range c.allMembers {
		c.checksums[pkgID(name, info.version, info.arch)] = entities.Checksum{Sha256: "deadbeef-" + name}
	}

	bi, err := c.CollectBuildInfo("build", "1", "mod")
	require.NoError(t, err)
	require.Len(t, bi.Modules, 1)

	for _, dep := range bi.Modules[0].Dependencies {
		assert.NotEmpty(t, dep.Scopes, "dep %s must have at least one scope", dep.Id)
	}

	// base-files specifically must have defaulted to "required".
	var found bool
	for _, dep := range bi.Modules[0].Dependencies {
		if strings.HasPrefix(dep.Id, "base-files:") {
			found = true
			assert.Contains(t, dep.Scopes, scopeRequired,
				"base-files must default to required scope")
		}
	}
	assert.True(t, found, "base-files must be reported in build info")
}

// ── pkgID / packageNameOnly ────────────────────────────────────────────────

// Scenario #42: dependency ID must be name:version:arch.
func TestPkgID_Format(t *testing.T) {
	assert.Equal(t, "jq:1.6-2.1ubuntu3:arm64", pkgID("jq", "1.6-2.1ubuntu3", "arm64"))
	assert.Equal(t, "libc6:2.39-0ubuntu8.4:amd64", pkgID("libc6", "2.39-0ubuntu8.4", "amd64"))
	assert.Equal(t, "libgcc-s1:14.2.0-4ubuntu2:arm64", pkgID("libgcc-s1", "14.2.0-4ubuntu2", "arm64"))
}

func TestPackageNameOnly_StripsVersionAndSuite(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{"curl", "curl"},
		{"curl=8.5.0-2ubuntu10", "curl"},
		{"curl/noble", "curl"},
		{"libcurl4t64=8.5.0", "libcurl4t64"},
		{"apt=2.7.14build2", "apt"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, packageNameOnly(tc.spec), tc.spec)
	}
}

// ── aptRelationToScope ────────────────────────────────────────────────────

// Scenario #43: scope mapping must match design doc §6 table.
func TestAptRelationToScope_KnownRelations(t *testing.T) {
	tests := []struct {
		relation string
		scope    string
		ok       bool
	}{
		{"Depends", scopeRequired, true},
		{"Pre-Depends", scopeRequired, true},
		{"Recommends", scopeRecommended, true},
		{"Suggests", scopeOptional, true},
		{"Conflicts", "", false},
		{"Breaks", "", false},
		{"Enhances", "", false},
		{"Replaces", "", false},
	}
	for _, tc := range tests {
		scope, ok := aptRelationToScope(tc.relation)
		assert.Equal(t, tc.ok, ok, "relation=%q: ok mismatch", tc.relation)
		assert.Equal(t, tc.scope, scope, "relation=%q: scope mismatch", tc.relation)
	}
}

// ── aptCacheClosureFlags ─────────────────────────────────────────────────

// Scenario #71: closure must be bounded to installed packages.
// Without --installed the walk follows every alternative across the whole
// archive (~23,000 packages for curl). Without --no-suggests, Suggests
// entries are included even though apt does not install them.
func TestAptCacheClosureFlags_BoundedToInstalled(t *testing.T) {
	assert.Contains(t, aptCacheClosureFlags, "--installed",
		"--installed must be present; without it the whole archive is walked")
	assert.Contains(t, aptCacheClosureFlags, "--no-suggests",
		"--no-suggests must be present; Suggests packages are not installed by apt")
}

// ── changedFromBaseline ───────────────────────────────────────────────────

func TestChangedFromBaseline_NilBaselineAlwaysTrue(t *testing.T) {
	// A nil PackageState (nil map) must behave like "every package changed"
	// — map reads return "" which never equals a real version:arch.
	c := &AptFlexPack{}
	info := &dpkgInfo{version: "1.0", arch: "amd64"}
	assert.True(t, c.changedFromBaseline("curl", info),
		"nil baseline must report every package as changed")
}

func TestChangedFromBaseline_MatchingBaselineReturnsFalse(t *testing.T) {
	c := &AptFlexPack{baseline: PackageState{"curl": "1.0:amd64"}}
	info := &dpkgInfo{version: "1.0", arch: "amd64"}
	assert.False(t, c.changedFromBaseline("curl", info))
}

func TestChangedFromBaseline_VersionChangedReturnsTrue(t *testing.T) {
	c := &AptFlexPack{baseline: PackageState{"curl": "1.0:amd64"}}
	info := &dpkgInfo{version: "1.1", arch: "amd64"}
	assert.True(t, c.changedFromBaseline("curl", info))
}

// ── ReadPackagesFile ───────────────────────────────────────────────────────

func TestReadPackagesFile_ParsesCorrectly(t *testing.T) {
	content := "# comment\ncurl\nwget=1.21.2\n\n# another comment\njq\n"
	f := writeTempFile(t, content)
	pkgs, err := ReadPackagesFile(f)
	require.NoError(t, err)
	assert.Equal(t, []string{"curl", "wget=1.21.2", "jq"}, pkgs)
}

func TestReadPackagesFile_EmptyAfterFiltering(t *testing.T) {
	f := writeTempFile(t, "# only comments\n\n")
	_, err := ReadPackagesFile(f)
	assert.Error(t, err, "file with only comments must return error")
}

func TestReadPackagesFile_FileNotFound(t *testing.T) {
	_, err := ReadPackagesFile("/nonexistent/no-such-file.txt")
	assert.Error(t, err)
}

func TestReadPackagesFile_VersionPinned(t *testing.T) {
	f := writeTempFile(t, "curl=8.5.0\njq=1.6\n")
	pkgs, err := ReadPackagesFile(f)
	require.NoError(t, err)
	assert.Equal(t, []string{"curl=8.5.0", "jq=1.6"}, pkgs)
}

// ── parseAptCacheDependsOutput ────────────────────────────────────────────

func TestParseAptCacheDependsOutput_BasicEdges(t *testing.T) {
	output := `curl
  Depends: libc6
  Depends: libcurl4t64
  Recommends: ca-certificates
libc6
libcurl4t64
  Depends: libc6
ca-certificates
`
	graph := parseAptCacheDependsOutput(output)

	// All four packages must appear as graph keys.
	for _, pkg := range []string{"curl", "libc6", "libcurl4t64", "ca-certificates"} {
		_, exists := graph[pkg]
		assert.True(t, exists, "%s must be a graph key", pkg)
	}

	// curl has three edges.
	assert.Len(t, graph["curl"], 3)

	// Scope on curl → libc6 edge must be required (Depends).
	found := false
	for _, e := range graph["curl"] {
		if e.child == "libc6" {
			assert.Equal(t, scopeRequired, e.scope)
			found = true
		}
	}
	assert.True(t, found, "curl → libc6 edge must exist")

	// Scope on curl → ca-certificates edge must be recommended (Recommends).
	found = false
	for _, e := range graph["curl"] {
		if e.child == "ca-certificates" {
			assert.Equal(t, scopeRecommended, e.scope)
			found = true
		}
	}
	assert.True(t, found, "curl → ca-certificates edge must exist")
}

func TestParseAptCacheDependsOutput_AlternativeDependency(t *testing.T) {
	output := `curl
  Depends: libcurl4t64
  |libcurl3-gnutls
`
	graph := parseAptCacheDependsOutput(output)

	// Both the primary and the alternative must be recorded as edges.
	var children []string
	for _, e := range graph["curl"] {
		children = append(children, e.child)
	}
	assert.Contains(t, children, "libcurl4t64")
	assert.Contains(t, children, "libcurl3-gnutls")
}

func TestParseAptCacheDependsOutput_AngleBracketVirtual(t *testing.T) {
	// Virtual packages appear as <name>; angle brackets must be stripped.
	output := `curl
  Depends: <libssl-dev>
`
	graph := parseAptCacheDependsOutput(output)

	for _, e := range graph["curl"] {
		assert.NotContains(t, e.child, "<", "angle brackets must be stripped from child names")
		assert.NotContains(t, e.child, ">", "angle brackets must be stripped from child names")
	}
}

// ── populateAptRequestedBy ────────────────────────────────────────────────

// Scenario #70: cyclic Debian dependencies (libc6 ↔ libgcc-s1) must not
// cause a stack overflow, and must not produce packages in their own ancestry.
//
// Scenario #45: requestedBy chains must be acyclic for all packages.
func TestPopulateAptRequestedBy_CyclicGraphTerminates(t *testing.T) {
	const (
		mod     = "mymod"
		libcID  = "libc6:2.39:amd64"
		libgcID = "libgcc-s1:14:amd64"
	)

	depsMap := map[string]entities.Dependency{
		libcID:  {Id: libcID},
		libgcID: {Id: libgcID},
	}
	// Both packages depend on each other — the classic Debian cycle.
	graph := map[string][]string{
		mod:     {libcID},
		libcID:  {libgcID},
		libgcID: {libcID}, // closes the cycle
	}

	// Must return without stack overflow.
	populateAptRequestedBy(mod, [][]string{{}}, depsMap, graph, map[string]bool{mod: true})

	libc := depsMap[libcID]
	libgc := depsMap[libgcID]

	// Both packages must have requestedBy populated.
	assert.NotEmpty(t, libc.RequestedBy, "libc6 must have requestedBy")
	assert.NotEmpty(t, libgc.RequestedBy, "libgcc-s1 must have requestedBy")

	// Neither package must appear in its own ancestry.
	for _, path := range libc.RequestedBy {
		assert.NotContains(t, path, libcID,
			"libc6 must not appear in its own requestedBy path: %v", path)
	}
	for _, path := range libgc.RequestedBy {
		assert.NotContains(t, path, libgcID,
			"libgcc-s1 must not appear in its own requestedBy path: %v", path)
	}
}

func TestPopulateAptRequestedBy_LinearChain(t *testing.T) {
	// A → B → C: verify chain is correctly recorded.
	depsMap := map[string]entities.Dependency{
		"A": {Id: "A"},
		"B": {Id: "B"},
		"C": {Id: "C"},
	}
	graph := map[string][]string{
		"mod": {"A"},
		"A":   {"B"},
		"B":   {"C"},
	}
	populateAptRequestedBy("mod", [][]string{{}}, depsMap, graph, map[string]bool{"mod": true})

	assert.NotEmpty(t, depsMap["A"].RequestedBy)
	assert.NotEmpty(t, depsMap["B"].RequestedBy)
	assert.NotEmpty(t, depsMap["C"].RequestedBy)

	// C's first path must include B and A in that order.
	cPaths := depsMap["C"].RequestedBy
	require.NotEmpty(t, cPaths)
	assert.Contains(t, cPaths[0], "B")
}

// ── parseDeb822 ────────────────────────────────────────────────────────────

func TestParseDeb822_BasicStanza(t *testing.T) {
	input := "Package: curl\nVersion: 8.5.0\nArchitecture: amd64\nSHA256: abc123\n"
	stanzas := parseDeb822(strings.NewReader(input))
	require.Len(t, stanzas, 1)
	assert.Equal(t, "curl", stanzas[0]["Package"])
	assert.Equal(t, "8.5.0", stanzas[0]["Version"])
	assert.Equal(t, "abc123", stanzas[0]["SHA256"])
}

func TestParseDeb822_MultipleStanzas(t *testing.T) {
	input := "Package: curl\nVersion: 8.5.0\n\nPackage: wget\nVersion: 1.21.2\n"
	stanzas := parseDeb822(strings.NewReader(input))
	assert.Len(t, stanzas, 2)
}

func TestParseDeb822_ContinuationLinesSkipped(t *testing.T) {
	// Description fields span multiple lines (continuation lines start with space).
	// Continuation lines must be skipped; only single-line fields are needed.
	input := "Package: curl\nDescription: A tool\n long multiline\n description here\nVersion: 8.5.0\n"
	stanzas := parseDeb822(strings.NewReader(input))
	require.Len(t, stanzas, 1)
	assert.Equal(t, "curl", stanzas[0]["Package"])
	assert.Equal(t, "8.5.0", stanzas[0]["Version"])
	// Description key exists but continuation lines are not appended.
	assert.Equal(t, "A tool", stanzas[0]["Description"])
}

func TestParseDeb822_TrailingStanzaWithoutBlankLine(t *testing.T) {
	// File may not end with a blank line; last stanza must still be parsed.
	input := "Package: jq\nVersion: 1.6"
	stanzas := parseDeb822(strings.NewReader(input))
	require.Len(t, stanzas, 1)
	assert.Equal(t, "jq", stanzas[0]["Package"])
}

// ── helpers ───────────────────────────────────────────────────────────────

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "apt-test-*.txt")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
