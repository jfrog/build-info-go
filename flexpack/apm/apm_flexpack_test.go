package apm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testManifest = `
name: my-agent-project
version: 1.2.3
`

const testLockfile = `
lockfile_version: "1"
dependencies:
  - repo_url: acme/skills-pack
    name: skills-pack
    version: 2.0.0
    package_type: skill
    source: registry
    content_hash: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    resolved_url: https://acme.jfrog.io/artifactory/api/agentpackages/agent-resources-local/acme/skills-pack/skills-pack-2.0.0.zip
    resolved_hash: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  - repo_url: acme/prompt-pack
    name: prompt-pack
    version: 3.1.0
    package_type: skill
    source: registry
    content_hash: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    resolved_url: https://acme.jfrog.io/artifactory/api/agentpackages/agent-resources-local/acme/prompt-pack/prompt-pack-3.1.0.zip
    resolved_hash: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
  - repo_url: someone/github-direct-pack
    name: github-direct-pack
    version: 0.1.0
    package_type: skill
    source: github
    content_hash: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
`

const malformedManifest = `
name: [this is not valid yaml
`

func writeProjectFiles(t *testing.T, dir, manifest, lockfile string) {
	t.Helper()
	if manifest != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, apmManifestName), []byte(manifest), 0644))
	}
	if lockfile != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, apmLockfileName), []byte(lockfile), 0644))
	}
}

func TestCollectBuildInfo_RegistryDependenciesOnly(t *testing.T) {
	tempDir := t.TempDir()
	writeProjectFiles(t, tempDir, testManifest, testLockfile)

	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: tempDir})
	require.NoError(t, err)

	buildInfo, err := af.CollectBuildInfo("apm-build", "1")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)

	module := buildInfo.Modules[0]
	assert.Equal(t, "my-agent-project:1.2.3", module.Id)
	assert.Equal(t, entities.Apm, module.Type)

	// Both registry-sourced dependencies are collected; the github-direct one is skipped.
	require.Len(t, module.Dependencies, 2)
	dep := module.Dependencies[0]
	assert.Equal(t, "acme/skills-pack:2.0.0", dep.Id)
	assert.Equal(t, "zip", dep.Type)
	assert.Equal(t, []string{"runtime"}, dep.Scopes)
	assert.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", dep.Sha256)

	dep2 := module.Dependencies[1]
	assert.Equal(t, "acme/prompt-pack:3.1.0", dep2.Id)
	assert.Equal(t, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", dep2.Sha256)
}

func TestLoadManifest_MalformedYAMLFallsBackCleanly(t *testing.T) {
	tempDir := t.TempDir()
	writeProjectFiles(t, tempDir, malformedManifest, testLockfile)

	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: tempDir})
	require.NoError(t, err)

	// A malformed apm.yml must not fail collection - it should log a warning (verified
	// manually; loadManifest distinguishes IsNotExist from real parse errors) and fall
	// back to the working directory's basename, same as a missing manifest.
	buildInfo, err := af.CollectBuildInfo("apm-build", "1")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	assert.Equal(t, filepath.Base(tempDir), buildInfo.Modules[0].Id)
}

func TestResolveScopeAndRequestedBy_RejectsFlagShapedRepoURL(t *testing.T) {
	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: t.TempDir()})
	require.NoError(t, err)

	scopes, requestedBy := af.resolveScopeAndRequestedBy("--global")
	assert.Equal(t, []string{"runtime"}, scopes)
	assert.Empty(t, requestedBy)
}

func TestParseDependencyToList_LazyInitBeforeCollectBuildInfo(t *testing.T) {
	tempDir := t.TempDir()
	writeProjectFiles(t, tempDir, testManifest, testLockfile)

	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: tempDir})
	require.NoError(t, err)

	// Calling a FlexPackManager method before CollectBuildInfo/GetProjectDependencies must
	// still trigger lazy initialization instead of silently returning an empty result.
	assert.Equal(t, []string{"acme/skills-pack:2.0.0", "acme/prompt-pack:3.1.0"}, af.ParseDependencyToList())
	assert.Len(t, af.CalculateChecksum(), 2)
	assert.Contains(t, af.GetDependency(), "acme/skills-pack:2.0.0")
}

func TestCollectBuildInfo_MissingManifestFallsBackToDirName(t *testing.T) {
	tempDir := t.TempDir()
	writeProjectFiles(t, tempDir, "", testLockfile)

	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: tempDir})
	require.NoError(t, err)

	buildInfo, err := af.CollectBuildInfo("apm-build", "1")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	assert.Equal(t, filepath.Base(tempDir), buildInfo.Modules[0].Id)
}

func TestCollectBuildInfo_MissingLockfileIsZeroDependenciesNotAnError(t *testing.T) {
	tempDir := t.TempDir()
	writeProjectFiles(t, tempDir, testManifest, "")

	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: tempDir})
	require.NoError(t, err)

	// apm doesn't write apm.lock.yaml at all for a zero-dependency project - this must
	// produce a valid module with no dependencies, not fail collection.
	buildInfo, err := af.CollectBuildInfo("apm-build", "1")
	require.NoError(t, err)
	require.Len(t, buildInfo.Modules, 1)
	assert.Empty(t, buildInfo.Modules[0].Dependencies)
}

func TestSha256Hex(t *testing.T) {
	assert.Equal(t, "abc123", sha256Hex("sha256:abc123"))
	assert.Empty(t, sha256Hex("md5:abc123"))
	assert.Empty(t, sha256Hex(""))
}

func TestParseDepsWhyOutput_DirectDependency(t *testing.T) {
	out := []byte(`{
		"package": {"is_direct": true, "repo_url": "uday/pkg-consumer", "source": "registry", "version": "1.0.0"},
		"paths": [{"chain": [{"is_direct": true, "repo_url": "uday/pkg-consumer"}]}]
	}`)
	scopes, requestedBy := parseDepsWhyOutput(out, "uday/pkg-consumer")
	assert.Equal(t, []string{"runtime"}, scopes)
	assert.Empty(t, requestedBy)
}

func TestParseDepsWhyOutput_TransitiveDependency(t *testing.T) {
	out := []byte(`{
		"package": {"is_direct": false, "repo_url": "uday/pkg-base", "source": "registry", "version": "1.0.0"},
		"paths": [{"chain": [
			{"is_direct": true, "repo_url": "uday/pkg-consumer"},
			{"is_direct": false, "repo_url": "uday/pkg-base"}
		]}]
	}`)
	scopes, requestedBy := parseDepsWhyOutput(out, "uday/pkg-base")
	assert.Equal(t, []string{"transitive"}, scopes)
	assert.Equal(t, [][]string{{"uday/pkg-consumer"}}, requestedBy)
}

func TestParseDepsWhyOutput_MultipleParentPaths(t *testing.T) {
	out := []byte(`{
		"package": {"is_direct": false, "repo_url": "shared/lib", "source": "registry", "version": "1.0.0"},
		"paths": [
			{"chain": [{"is_direct": true, "repo_url": "a/pkg"}, {"is_direct": false, "repo_url": "shared/lib"}]},
			{"chain": [{"is_direct": true, "repo_url": "b/pkg"}, {"is_direct": false, "repo_url": "shared/lib"}]}
		]
	}`)
	scopes, requestedBy := parseDepsWhyOutput(out, "shared/lib")
	assert.Equal(t, []string{"transitive"}, scopes)
	assert.Equal(t, [][]string{{"a/pkg"}, {"b/pkg"}}, requestedBy)
}

func TestParseDepsWhyOutput_MalformedJSONFallsBackToRuntime(t *testing.T) {
	scopes, requestedBy := parseDepsWhyOutput([]byte("not json"), "uday/pkg-base")
	assert.Equal(t, []string{"runtime"}, scopes)
	assert.Empty(t, requestedBy)
}

// TestParseDepsWhyOutput_PathCountCappedAtMax verifies the fan-in cap: a widely-shared
// dependency (e.g. a diamond dependency's base, reachable through many parents) reports at
// most requestedByMaxPaths distinct paths, matching the same cap golang.go/yarn.go/
// uv_flexpack.go apply to len(dependency.RequestedBy) elsewhere in build-info-go.
func TestParseDepsWhyOutput_PathCountCappedAtMax(t *testing.T) {
	var pathsJSON strings.Builder
	for i := range requestedByMaxPaths + 5 {
		if i > 0 {
			pathsJSON.WriteByte(',')
		}
		parent := "parent" + string(rune('a'+i%26))
		pathsJSON.WriteString(`{"chain": [{"is_direct": true, "repo_url": "` + parent + `"}, {"is_direct": false, "repo_url": "target"}]}`)
	}
	out := []byte(`{
		"package": {"is_direct": false, "repo_url": "target", "source": "registry", "version": "1.0.0"},
		"paths": [` + pathsJSON.String() + `]
	}`)
	_, requestedBy := parseDepsWhyOutput(out, "target")
	assert.Len(t, requestedBy, requestedByMaxPaths)
}

// TestParseDepsWhyOutput_SinglePathNotTruncatedByDepth verifies an individual chain's depth
// is reported in full - only the number of distinct paths is capped, not how deep one path goes.
func TestParseDepsWhyOutput_SinglePathNotTruncatedByDepth(t *testing.T) {
	chain := `{"is_direct": false, "repo_url": "target"}`
	var parents strings.Builder
	for i := range requestedByMaxPaths + 5 {
		parents.WriteString(`{"is_direct": false, "repo_url": "p` + string(rune('a'+i%26)) + `"},`)
	}
	out := []byte(`{
		"package": {"is_direct": false, "repo_url": "target", "source": "registry", "version": "1.0.0"},
		"paths": [{"chain": [` + parents.String() + chain + `]}]
	}`)
	_, requestedBy := parseDepsWhyOutput(out, "target")
	require.Len(t, requestedBy, 1)
	require.Len(t, requestedBy[0], requestedByMaxPaths+5)
}

// TestCalculateScopesAndRequestedBy_AggregatesPerDependencyData seeds dependencies directly
// (bypassing apm deps why, which isn't available in the test sandbox) to verify the real
// aggregation logic: distinct scopes across all dependencies, and each dependency's closest
// parent flattened from its requestedBy chains.
func TestCalculateScopesAndRequestedBy_AggregatesPerDependencyData(t *testing.T) {
	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: t.TempDir()})
	require.NoError(t, err)
	af.initialized = true
	af.dependencies = []entities.Dependency{
		{Id: "a/pkg:1.0.0", Scopes: []string{"runtime"}},
		{
			Id:          "b/pkg:1.0.0",
			Scopes:      []string{"transitive"},
			RequestedBy: [][]string{{"a/pkg"}, {"c/pkg", "a/pkg"}},
		},
	}

	assert.ElementsMatch(t, []string{"runtime", "transitive"}, af.CalculateScopes())
	assert.Equal(t, map[string][]string{"b/pkg:1.0.0": {"a/pkg", "c/pkg"}}, af.CalculateRequestedBy())
}

func TestFlexPackManagerMethods(t *testing.T) {
	tempDir := t.TempDir()
	writeProjectFiles(t, tempDir, testManifest, testLockfile)

	af, err := NewApmFlexPack(ApmConfig{WorkingDirectory: tempDir})
	require.NoError(t, err)
	_, err = af.CollectBuildInfo("apm-build", "1")
	require.NoError(t, err)

	assert.Equal(t, []string{"acme/skills-pack:2.0.0", "acme/prompt-pack:3.1.0"}, af.ParseDependencyToList())
	assert.Equal(t, []string{"runtime"}, af.CalculateScopes())
	assert.Empty(t, af.CalculateRequestedBy())
	assert.Len(t, af.CalculateChecksum(), 2)

	deps, err := af.GetProjectDependencies()
	require.NoError(t, err)
	assert.Len(t, deps, 2)

	graph, err := af.GetDependencyGraph()
	require.NoError(t, err)
	assert.Empty(t, graph)
}
