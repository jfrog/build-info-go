package build

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers ─────────────────────────────────────────────────────────────────────

func pkg(name, version string) buildutils.AlpinePackage {
	return buildutils.AlpinePackage{Name: name, Version: version, Arch: "x86_64"}
}

func pkgWithDepends(name, version string, depends []string) buildutils.AlpinePackage {
	p := pkg(name, version)
	p.Depends = depends
	return p
}

// ─── buildRequestedBy ────────────────────────────────────────────────────────

func TestBuildRequestedBy_DirectPackages(t *testing.T) {
	curl := pkg("curl", "8.5.0-r0")
	added := []buildutils.AlpinePackage{curl}
	depGraph := map[string][]string{}
	requested := map[string]bool{"curl": true}

	result := buildRequestedBy(added, depGraph, requested)

	require.Contains(t, result, "curl")
	assert.Nil(t, result["curl"],
		"explicitly requested package must have nil requestedBy so it is omitted from JSON")
}

func TestBuildRequestedBy_TransitiveDeps(t *testing.T) {
	curl := pkg("curl", "8.5.0-r0")
	musl := pkg("musl", "1.2.4-r2")
	added := []buildutils.AlpinePackage{curl, musl}

	depGraph := map[string][]string{
		"curl": {"musl"},
	}
	requested := map[string]bool{"curl": true}

	result := buildRequestedBy(added, depGraph, requested)

	require.Contains(t, result, "curl")
	assert.Nil(t, result["curl"])

	require.Contains(t, result, "musl")
	assert.Equal(t, [][]string{{curl.ID()}}, result["musl"],
		"transitive dep must carry parent's full ID in requestedBy")
}

func TestBuildRequestedBy_FullAncestorChain(t *testing.T) {
	app := pkg("app", "1.0-r0")
	mid := pkg("mid", "1.0-r0")
	leaf := pkg("leaf", "1.0-r0")
	added := []buildutils.AlpinePackage{app, mid, leaf}
	depGraph := map[string][]string{
		"app": {"mid"},
		"mid": {"leaf"},
	}
	requested := map[string]bool{"app": true}

	result := buildRequestedBy(added, depGraph, requested)

	assert.Nil(t, result["app"])
	assert.Equal(t, [][]string{{app.ID()}}, result["mid"])
	assert.Equal(t, [][]string{{mid.ID(), app.ID()}}, result["leaf"],
		"deeper transitive dep must carry full ancestor chain")
}

func TestBuildRequestedBy_MultipleParents(t *testing.T) {
	pkgA := pkg("a", "1.0-r0")
	pkgB := pkg("b", "1.0-r0")
	pkgC := pkg("c", "1.0-r0")
	added := []buildutils.AlpinePackage{pkgA, pkgB, pkgC}

	depGraph := map[string][]string{
		"a": {"c"},
		"b": {"c"},
	}
	requested := map[string]bool{"a": true, "b": true}

	result := buildRequestedBy(added, depGraph, requested)

	require.Contains(t, result, "c")
	assert.Len(t, result["c"], 2,
		"c should have two requestedBy entries (one from a, one from b)")
}

func TestBuildRequestedBy_TransitiveDep_NotInAddedSet(t *testing.T) {
	curl := pkg("curl", "8.5.0-r0")
	added := []buildutils.AlpinePackage{curl}

	depGraph := map[string][]string{"curl": {"zlib"}}
	requested := map[string]bool{"curl": true}

	result := buildRequestedBy(added, depGraph, requested)

	assert.NotContains(t, result, "zlib",
		"dep not in the newly-installed set must not appear in requestedBy")
}

func TestBuildRequestedBy_MaxLength(t *testing.T) {
	target := pkg("target", "1.0-r0")
	added := []buildutils.AlpinePackage{target}
	depGraph := map[string][]string{}
	requested := map[string]bool{}

	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("parent%d", i)
		p := pkg(name, "1.0-r0")
		added = append(added, p)
		depGraph[name] = []string{"target"}
		requested[name] = true
	}

	result := buildRequestedBy(added, depGraph, requested)

	assert.LessOrEqual(t, len(result["target"]), entities.RequestedByMaxLength,
		"requestedBy must be capped at %d entries", entities.RequestedByMaxLength)
}

func TestBuildRequestedBy_EmptyInputs(t *testing.T) {
	result := buildRequestedBy(nil, nil, nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestFlattenRequestedBy(t *testing.T) {
	chains := [][]string{
		{"bash:5.2.37-r0"},
		{"jq:1.7.1-r0"},
		{"libncursesw:6.5-r3", "readline:8.2.13-r0", "bash:5.2.37-r0"},
		{"oniguruma:6.9.9-r0", "jq:1.7.1-r0"},
		{"readline:8.2.13-r0", "bash:5.2.37-r0"},
	}

	got := FlattenRequestedBy(chains)

	require.Len(t, got, 1, "chains must collapse into a single collection")
	assert.Equal(t, []string{
		"bash:5.2.37-r0",
		"jq:1.7.1-r0",
		"libncursesw:6.5-r3",
		"readline:8.2.13-r0",
		"oniguruma:6.9.9-r0",
	}, got[0], "every ancestor must appear exactly once, in first-seen order")
}

func TestFlattenRequestedBy_Empty(t *testing.T) {
	assert.Nil(t, FlattenRequestedBy(nil))
	assert.Nil(t, FlattenRequestedBy([][]string{}))
	assert.Nil(t, FlattenRequestedBy([][]string{{}}))
}

func TestFlattenRequestedBy_CapsAtMaxLength(t *testing.T) {
	var chains [][]string
	for i := 0; i < entities.RequestedByMaxLength*2; i++ {
		chains = append(chains, []string{fmt.Sprintf("parent%d:1.0-r0", i)})
	}

	got := FlattenRequestedBy(chains)

	require.Len(t, got, 1)
	assert.Len(t, got[0], entities.RequestedByMaxLength)
}

func TestResolveDep_CacheHit(t *testing.T) {
	cacheDir := t.TempDir()
	content := []byte("fake apk content for resolveDep")
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "curl-8.5.0-r0.apk"), content, 0644))

	expected, err := crypto.GetFileChecksums(filepath.Join(cacheDir, "curl-8.5.0-r0.apk"))
	require.NoError(t, err)

	m := &AlpineModule{}
	dep := m.resolveDep(pkg("curl", "8.5.0-r0"), cacheDir, nil)

	assert.Equal(t, "curl:8.5.0-r0", dep.Id)
	assert.Equal(t, expected[crypto.SHA1], dep.Sha1)
	assert.Equal(t, expected[crypto.SHA256], dep.Sha256)
	assert.Equal(t, expected[crypto.MD5], dep.Md5)
}

func TestResolveDep_CacheMiss(t *testing.T) {
	m := &AlpineModule{}
	dep := m.resolveDep(pkg("curl", "8.5.0-r0"), t.TempDir(), nil)

	assert.Equal(t, "curl:8.5.0-r0", dep.Id)
	assert.Empty(t, dep.Sha1, "cache miss must leave SHA1 empty")
	assert.Empty(t, dep.Sha256, "cache miss must leave SHA256 empty")
	assert.Empty(t, dep.Md5, "cache miss must leave MD5 empty")
}

func TestResolveDep_EmptyCacheDir(t *testing.T) {
	m := &AlpineModule{}
	dep := m.resolveDep(pkg("curl", "8.5.0-r0"), "", nil)

	assert.Equal(t, "curl:8.5.0-r0", dep.Id)
	assert.Empty(t, dep.Sha1)
	assert.Empty(t, dep.Sha256)
	assert.Empty(t, dep.Md5)
}

func TestCollectDependencies_ScopeChecksumAndRequestedBy(t *testing.T) {
	cacheDir := t.TempDir()
	curlContent := []byte("curl-apk-bytes")
	muslContent := []byte("musl-apk-bytes")
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "curl-8.5.0-r0.apk"), curlContent, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "musl-1.2.4-r2.apk"), muslContent, 0644))

	curlExpected, err := crypto.GetFileChecksums(filepath.Join(cacheDir, "curl-8.5.0-r0.apk"))
	require.NoError(t, err)
	muslExpected, err := crypto.GetFileChecksums(filepath.Join(cacheDir, "musl-1.2.4-r2.apk"))
	require.NoError(t, err)

	preExisting := pkg("zlib", "1.3-r0")
	curl := pkgWithDepends("curl", "8.5.0-r0", []string{"musl", "zlib"})
	musl := pkg("musl", "1.2.4-r2")

	m := &AlpineModule{requestedPkgs: map[string]bool{"curl": true}}
	m.SetPreSnapshot([]buildutils.AlpinePackage{preExisting})
	m.SetCacheDir(cacheDir)

	after := []buildutils.AlpinePackage{preExisting, curl, musl}
	deps := m.collectDependenciesFromSnapshots(after)

	byID := make(map[string]entities.Dependency, len(deps))
	for _, d := range deps {
		byID[d.Id] = d
	}

	require.Contains(t, byID, curl.ID())
	require.Contains(t, byID, musl.ID())
	require.Contains(t, byID, preExisting.ID())

	assert.Equal(t, []string{"prod"}, byID[curl.ID()].Scopes)
	assert.Nil(t, byID[curl.ID()].RequestedBy)
	assert.Equal(t, curlExpected[crypto.SHA1], byID[curl.ID()].Sha1)
	assert.Equal(t, curlExpected[crypto.SHA256], byID[curl.ID()].Sha256)

	assert.Equal(t, []string{"transitive"}, byID[musl.ID()].Scopes)
	assert.Equal(t, [][]string{{curl.ID()}}, byID[musl.ID()].RequestedBy)
	assert.Equal(t, muslExpected[crypto.SHA1], byID[musl.ID()].Sha1)

	assert.Equal(t, []string{"transitive"}, byID[preExisting.ID()].Scopes)
	assert.Equal(t, [][]string{{curl.ID()}}, byID[preExisting.ID()].RequestedBy)
	assert.Empty(t, byID[preExisting.ID()].Sha1, "pre-existing dep without cached apk has empty checksums")
}

func TestCollectDependencies_RequestedByForSharedObjectDeps(t *testing.T) {
	curl := pkgWithDepends("curl", "8.5.0-r0", []string{"so:libcurl.so.4", "so:libc.musl-x86_64.so.1"})
	libcurl := pkgWithDepends("libcurl", "8.5.0-r0", []string{"so:libc.musl-x86_64.so.1"})
	libcurl.Provides = []string{"so:libcurl.so.4"}
	musl := pkg("musl", "1.2.4-r2")
	musl.Provides = []string{"so:libc.musl-x86_64.so.1"}

	m := &AlpineModule{requestedPkgs: map[string]bool{"curl": true}}
	m.SetPreSnapshot([]buildutils.AlpinePackage{musl})

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{musl, curl, libcurl})

	byID := make(map[string]entities.Dependency, len(deps))
	for _, d := range deps {
		byID[d.Id] = d
	}

	require.Contains(t, byID, curl.ID())
	require.Contains(t, byID, libcurl.ID(), "so:-provided dependency must be recorded")
	require.Contains(t, byID, musl.ID(), "pre-existing so:-provided dependency must be recorded")

	assert.Equal(t, []string{AlpineScopeProd}, byID[curl.ID()].Scopes)
	assert.Nil(t, byID[curl.ID()].RequestedBy, "the requested package is the root of the chain")

	assert.Equal(t, []string{AlpineScopeTransitive}, byID[libcurl.ID()].Scopes)
	assert.Equal(t, [][]string{{curl.ID()}}, byID[libcurl.ID()].RequestedBy,
		"libcurl is reached through so:libcurl.so.4 and must point back at curl")

	assert.Equal(t, []string{AlpineScopeTransitive}, byID[musl.ID()].Scopes)
	require.Len(t, byID[musl.ID()].RequestedBy, 1,
		"musl is reached through so:libc.musl-x86_64.so.1 and must carry a single flat requestedBy list")
	assert.Contains(t, byID[musl.ID()].RequestedBy[0], curl.ID(),
		"the requested package must appear among musl's ancestors")
}

func TestCollectDependencies_AllTransitiveDepsHaveRequestedBy(t *testing.T) {
	app := pkgWithDepends("app", "1.0.0-r0", []string{"so:libmid.so.1", "cmd:helper"})
	mid := pkgWithDepends("mid", "2.0.0-r0", []string{"so:libleaf.so.2"})
	mid.Provides = []string{"so:libmid.so.1"}
	leaf := pkg("leaf", "3.0.0-r0")
	leaf.Provides = []string{"so:libleaf.so.2"}
	helper := pkg("helper", "4.0.0-r0")
	helper.Provides = []string{"cmd:helper"}

	m := &AlpineModule{requestedPkgs: map[string]bool{"app": true}}
	m.SetPreSnapshot(nil)

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{app, mid, leaf, helper})
	require.Len(t, deps, 4)

	for _, dep := range deps {
		if dep.Id == app.ID() {
			continue
		}
		assert.NotEmpty(t, dep.RequestedBy, "transitive dependency %s must have a requestedBy chain", dep.Id)
	}

	byID := make(map[string]entities.Dependency, len(deps))
	for _, d := range deps {
		byID[d.Id] = d
	}
	assert.Equal(t, [][]string{{mid.ID(), app.ID()}}, byID[leaf.ID()].RequestedBy,
		"a dependency two levels deep must carry the full ancestor chain")
	assert.Equal(t, [][]string{{app.ID()}}, byID[helper.ID()].RequestedBy,
		"cmd: dependencies must resolve to their provider too")
}

func TestCollectDependencies_RequestedByForSideEffectInstalls(t *testing.T) {
	oldZlib := pkg("zlib", "1.2.13-r0")
	newZlib := pkgWithDepends("zlib", "1.3.1-r2", []string{"so:libzhelper.so.1"})
	helper := pkg("libz-helper", "1.0.0-r0")
	helper.Provides = []string{"so:libzhelper.so.1"}
	curl := pkg("curl", "8.5.0-r0")

	m := &AlpineModule{requestedPkgs: map[string]bool{"curl": true}}
	m.SetPreSnapshot([]buildutils.AlpinePackage{oldZlib})

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{newZlib, helper, curl})

	byID := make(map[string]entities.Dependency, len(deps))
	for _, d := range deps {
		byID[d.Id] = d
	}

	require.Contains(t, byID, helper.ID())
	assert.Equal(t, [][]string{{newZlib.ID()}}, byID[helper.ID()].RequestedBy,
		"a dependency of an upgraded package must point back at that package")
}

func TestCollectDependencies_RecordsDownloadsMissedByDiff(t *testing.T) {
	downloads := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(downloads, "curl-8.5.0-r0.apk"), []byte("curl-bytes"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downloads, "zlib-1.3-r0.apk"), []byte("zlib-bytes"), 0644))
	zlibExpected, err := crypto.GetFileChecksums(filepath.Join(downloads, "zlib-1.3-r0.apk"))
	require.NoError(t, err)

	zlib := pkg("zlib", "1.3-r0")
	curl := pkg("curl", "8.5.0-r0")

	m := &AlpineModule{requestedPkgs: map[string]bool{"curl": true}}
	m.SetPreSnapshot([]buildutils.AlpinePackage{zlib})
	m.SetCacheDir(downloads)
	m.SetDownloadsDir(downloads)

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{zlib, curl})

	byID := make(map[string]entities.Dependency, len(deps))
	for _, d := range deps {
		byID[d.Id] = d
	}
	require.Contains(t, byID, curl.ID())
	require.Contains(t, byID, zlib.ID(), "a re-downloaded archive must be recorded even when the diff misses it")
	assert.Len(t, deps, 2, "each downloaded archive must be recorded exactly once")

	assert.Equal(t, []string{AlpineScopeTransitive}, byID[zlib.ID()].Scopes)
	assert.Equal(t, zlibExpected[crypto.SHA256], byID[zlib.ID()].Sha256,
		"checksums must come from the downloaded archive")
}

func TestCollectDependencies_DownloadsDirDoesNotDuplicateDeps(t *testing.T) {
	downloads := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(downloads, "curl-8.5.0-r0.apk"), []byte("curl-bytes"), 0644))

	curl := pkg("curl", "8.5.0-r0")
	m := &AlpineModule{requestedPkgs: map[string]bool{"curl": true}}
	m.SetPreSnapshot(nil)
	m.SetCacheDir(downloads)
	m.SetDownloadsDir(downloads)

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{curl})

	require.Len(t, deps, 1, "a package recorded from the diff must not be recorded again from the download dir")
	assert.Equal(t, curl.ID(), deps[0].Id)
	assert.Equal(t, []string{AlpineScopeProd}, deps[0].Scopes, "the requested package keeps its prod scope")
}

func TestCollectDependencies_NoDownloadsDirIsANoOp(t *testing.T) {
	curl := pkg("curl", "8.5.0-r0")
	m := &AlpineModule{requestedPkgs: map[string]bool{"curl": true}}
	m.SetPreSnapshot(nil)

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{curl})
	require.Len(t, deps, 1)
	assert.Empty(t, deps[0].Sha1, "without a download dir checksums are left for AQL enrichment")
}

func TestCollectDependencies_CyclicProviderDeps(t *testing.T) {
	app := pkgWithDepends("app", "1.0.0-r0", []string{"so:liba.so.1"})
	a := pkgWithDepends("a", "2.0.0-r0", []string{"so:libb.so.1"})
	a.Provides = []string{"so:liba.so.1"}
	b := pkgWithDepends("b", "3.0.0-r0", []string{"so:liba.so.1"})
	b.Provides = []string{"so:libb.so.1"}

	m := &AlpineModule{requestedPkgs: map[string]bool{"app": true}}
	m.SetPreSnapshot(nil)

	deps := m.collectDependenciesFromSnapshots([]buildutils.AlpinePackage{app, a, b})
	require.Len(t, deps, 3)

	for _, dep := range deps {
		assert.LessOrEqual(t, len(dep.RequestedBy), entities.RequestedByMaxLength,
			"requestedBy for %s must stay capped even with cyclic dependencies", dep.Id)
		if dep.Id != app.ID() {
			assert.NotEmpty(t, dep.RequestedBy, "%s must still have a requestedBy chain", dep.Id)
		}
	}
}

// ─── AlpineModule setters ─────────────────────────────────────────────────────

func TestAlpineModule_SetRequestedPackages(t *testing.T) {
	service := NewBuildInfoService()
	build, err := service.GetOrCreateBuild("test-set-pkgs", "1")
	require.NoError(t, err)
	defer func() { _ = build.Clean() }()

	m := build.AddAlpineModule("test-module", "test-repo", "3.18")
	m.SetRequestedPackages([]string{"curl", "wget"})

	assert.True(t, m.requestedPkgs["curl"])
	assert.True(t, m.requestedPkgs["wget"])
	assert.False(t, m.requestedPkgs["bash"])
}

func TestAlpineModule_SetPreSnapshot(t *testing.T) {
	service := NewBuildInfoService()
	build, err := service.GetOrCreateBuild("test-set-snapshot", "1")
	require.NoError(t, err)
	defer func() { _ = build.Clean() }()

	m := build.AddAlpineModule("test-module", "test-repo", "3.18")
	snapshot := []buildutils.AlpinePackage{pkg("musl", "1.2.4-r2")}
	m.SetPreSnapshot(snapshot)

	assert.Equal(t, snapshot, m.preSnapshot)
}

// ─── SaveBuildInfo — module type ─────────────────────────────────────────────

func TestAlpineModule_SaveBuildInfo_ModuleType(t *testing.T) {
	service := NewBuildInfoService()
	build, err := service.GetOrCreateBuild("test-alpine-module-type", "1")
	require.NoError(t, err)
	defer func() { _ = build.Clean() }()

	m := build.AddAlpineModule("alpine-module", "test-repo", "3.18")
	deps := []entities.Dependency{
		{
			Id:     "curl:8.5.0-r0",
			Scopes: []string{"prod"},
		},
	}
	require.NoError(t, m.SaveBuildInfo(deps))

	buildInfo, err := build.ToBuildInfo()
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	assert.Equal(t, entities.Apk, buildInfo.Modules[0].Type,
		"Alpine build-info module type must be entities.Apk")
	assert.Equal(t, "alpine-module", buildInfo.Modules[0].Id)
	assert.Len(t, buildInfo.Modules[0].Dependencies, 1)
}

func TestAlpineModule_ScopeClassification(t *testing.T) {
	service := NewBuildInfoService()
	build, err := service.GetOrCreateBuild("test-scope-classification", "1")
	require.NoError(t, err)
	defer func() { _ = build.Clean() }()

	m := build.AddAlpineModule("scope-test", "test-repo", "3.18")

	curlDep := entities.Dependency{
		Id:     "curl:8.5.0-r0",
		Scopes: []string{"prod"},
	}
	muslDep := entities.Dependency{
		Id:          "musl:1.2.4-r2",
		Scopes:      []string{"transitive"},
		RequestedBy: [][]string{{"curl:8.5.0-r0"}},
	}

	require.NoError(t, m.SaveBuildInfo([]entities.Dependency{curlDep, muslDep}))

	buildInfo, err := build.ToBuildInfo()
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)

	deps := buildInfo.Modules[0].Dependencies
	require.Len(t, deps, 2)

	depByID := make(map[string]entities.Dependency, len(deps))
	for _, d := range deps {
		depByID[d.Id] = d
	}

	assert.Equal(t, []string{"prod"}, depByID["curl:8.5.0-r0"].Scopes)
	assert.Nil(t, depByID["curl:8.5.0-r0"].RequestedBy)
	assert.Equal(t, []string{"transitive"}, depByID["musl:1.2.4-r2"].Scopes)
	assert.Equal(t, [][]string{{"curl:8.5.0-r0"}}, depByID["musl:1.2.4-r2"].RequestedBy)
}
