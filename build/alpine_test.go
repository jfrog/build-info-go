//go:build linux

package build

import (
	"testing"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers ─────────────────────────────────────────────────────────────────────

func pkg(name, version string) buildutils.AlpinePackage {
	return buildutils.AlpinePackage{Name: name, Version: version, Arch: "x86_64"}
}

// ─── buildRequestedBy ────────────────────────────────────────────────────────

// Scenario #34: explicitly requested packages must get nil requestedBy so the
// field is omitted from JSON (omitempty).
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

// Scenario #35: transitive deps must carry the parent's full ID in requestedBy.
func TestBuildRequestedBy_TransitiveDeps(t *testing.T) {
	curl := pkg("curl", "8.5.0-r0")
	musl := pkg("musl", "1.2.4-r2")
	added := []buildutils.AlpinePackage{curl, musl}

	// curl depends on musl — so musl is a transitive dep of curl.
	depGraph := map[string][]string{
		"curl": {"musl"},
	}
	requested := map[string]bool{"curl": true}

	result := buildRequestedBy(added, depGraph, requested)

	// curl is directly requested → root level (nil, omitted from JSON)
	require.Contains(t, result, "curl")
	assert.Nil(t, result["curl"])

	// musl is a transitive dep → requestedBy should contain curl's full ID
	require.Contains(t, result, "musl")
	assert.Equal(t, [][]string{{curl.ID()}}, result["musl"],
		"transitive dep must carry parent's full ID in requestedBy")
}

func TestBuildRequestedBy_MultipleParents(t *testing.T) {
	pkgA := pkg("a", "1.0-r0")
	pkgB := pkg("b", "1.0-r0")
	pkgC := pkg("c", "1.0-r0")
	added := []buildutils.AlpinePackage{pkgA, pkgB, pkgC}

	// Both a and b depend on c.
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
	// If a dep listed in the graph was NOT in the newly-installed set,
	// it must not appear in the requestedBy map.
	curl := pkg("curl", "8.5.0-r0")
	added := []buildutils.AlpinePackage{curl}

	// curl "depends on" zlib, but zlib was already installed (not in added).
	depGraph := map[string][]string{"curl": {"zlib"}}
	requested := map[string]bool{"curl": true}

	result := buildRequestedBy(added, depGraph, requested)

	assert.NotContains(t, result, "zlib",
		"dep not in the newly-installed set must not appear in requestedBy")
}

func TestBuildRequestedBy_MaxLength(t *testing.T) {
	// requestedBy is capped at 15 entries per package.
	// Create 20 packages that all depend on a single target.
	const maxParents = 15
	target := pkg("target", "1.0-r0")
	added := []buildutils.AlpinePackage{target}
	depGraph := map[string][]string{}

	for i := 0; i < 20; i++ {
		p := pkg("parent", string(rune('a'+i)))
		added = append(added, p)
		depGraph[p.Name] = []string{"target"}
	}

	requested := map[string]bool{}
	result := buildRequestedBy(added, depGraph, requested)

	assert.LessOrEqual(t, len(result["target"]), maxParents,
		"requestedBy must be capped at %d entries", maxParents)
}

func TestBuildRequestedBy_EmptyInputs(t *testing.T) {
	result := buildRequestedBy(nil, nil, nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
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

// Scenario #20: the build-info module type must be entities.Alpine.
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
	assert.Equal(t, entities.Alpine, buildInfo.Modules[0].Type,
		"Alpine build-info module type must be entities.Alpine")
	assert.Equal(t, "alpine-module", buildInfo.Modules[0].Id)
	assert.Len(t, buildInfo.Modules[0].Dependencies, 1)
}

// ─── Scope classification via buildRequestedBy + SaveBuildInfo ───────────────

// Scenario #34: requested pkgs get scope "prod", transitives get "transitive".
// This tests the scope assignment logic end-to-end through the module.
func TestAlpineModule_ScopeClassification(t *testing.T) {
	service := NewBuildInfoService()
	build, err := service.GetOrCreateBuild("test-scope-classification", "1")
	require.NoError(t, err)
	defer func() { _ = build.Clean() }()

	m := build.AddAlpineModule("scope-test", "test-repo", "3.18")

	// Simulate: curl was requested, musl is a transitive dep pulled in by curl.
	curlDep := entities.Dependency{
		Id:     "curl:8.5.0-r0",
		Scopes: []string{"prod"},
		// RequestedBy is nil for root-level packages — omitted from JSON via omitempty.
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

	assert.Equal(t, []string{"prod"}, depByID["curl:8.5.0-r0"].Scopes,
		"explicitly requested package must have scope 'prod'")
	assert.Nil(t, depByID["curl:8.5.0-r0"].RequestedBy,
		"explicitly requested package must have nil requestedBy so it is omitted from JSON")

	assert.Equal(t, []string{"transitive"}, depByID["musl:1.2.4-r2"].Scopes,
		"transitive dependency must have scope 'transitive'")
	assert.Equal(t, [][]string{{"curl:8.5.0-r0"}}, depByID["musl:1.2.4-r2"].RequestedBy,
		"transitive dep requestedBy must contain the parent's ID")
}
