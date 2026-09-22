package flexpack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvertDependencyGraph(t *testing.T) {
	graph := map[string][]string{
		"root": {"a", "b"},
		"a":    {"c"},
	}
	got := invertDependencyGraph(graph)
	assert.Equal(t, []string{"root"}, got["a"])
	assert.Equal(t, []string{"root"}, got["b"])
	assert.Equal(t, []string{"a"}, got["c"])
	assert.Empty(t, got["root"])
}

func TestCollectModuleDependencies(t *testing.T) {
	mf := &MavenFlexPack{config: MavenConfig{IncludeTestDependencies: true}}
	root := MavenDependencyJSON{
		GroupID: "com.example", ArtifactID: "app", Version: "1.0",
		Children: []MavenDependencyJSON{
			{GroupID: "com.google.guava", ArtifactID: "guava", Version: "32.1", Scope: "compile", Children: []MavenDependencyJSON{
				{GroupID: "com.google.guava", ArtifactID: "failureaccess", Version: "1.0", Scope: "compile"},
			}},
			// failureaccess is a diamond: pulled in transitively via guava AND declared directly on the
			// module. The node is collapsed to one entry, but BOTH parent edges must be recorded.
			{GroupID: "com.google.guava", ArtifactID: "failureaccess", Version: "1.0", Scope: "compile"},
		},
	}
	deps, graph := mf.collectModuleDependencies("com.example:app:1.0", root)

	ids := depIDs(deps)
	assert.ElementsMatch(t, []string{"com.google.guava:guava:32.1", "com.google.guava:failureaccess:1.0"}, ids,
		"duplicate within module collapsed to a single node")
	// requested-by edges: guava requested by the module; failureaccess by BOTH guava (transitive) and
	// the module (direct) - the diamond's two routes are preserved, not just the first discovered.
	requestedBy := invertDependencyGraph(graph)
	assert.Equal(t, []string{"com.example:app:1.0"}, requestedBy["com.google.guava:guava:32.1"])
	assert.ElementsMatch(t, []string{"com.google.guava:guava:32.1", "com.example:app:1.0"},
		requestedBy["com.google.guava:failureaccess:1.0"])
}

func TestBuildRequestedByPaths(t *testing.T) {
	t.Run("full chain to root", func(t *testing.T) {
		// module -> guava -> failureaccess. failureaccess's path is [guava, module], guava's is [module].
		parents := map[string][]string{
			"guava":         {"module"},
			"failureaccess": {"guava"},
		}
		assert.Equal(t, [][]string{{"module"}}, buildRequestedByPaths("guava", parents))
		assert.Equal(t, [][]string{{"guava", "module"}}, buildRequestedByPaths("failureaccess", parents))
	})

	t.Run("diamond yields one path per route", func(t *testing.T) {
		// failureaccess reached via guava and directly from the module -> two complete paths.
		parents := map[string][]string{
			"guava":         {"module"},
			"failureaccess": {"guava", "module"},
		}
		assert.ElementsMatch(t,
			[][]string{{"guava", "module"}, {"module"}},
			buildRequestedByPaths("failureaccess", parents))
	})

	t.Run("no parents yields no path", func(t *testing.T) {
		assert.Empty(t, buildRequestedByPaths("orphan", map[string][]string{}))
	})

	t.Run("cycle is broken defensively", func(t *testing.T) {
		// a -> b -> a. Walking from a must terminate rather than recurse forever.
		parents := map[string][]string{"a": {"b"}, "b": {"a"}}
		paths := buildRequestedByPaths("a", parents)
		assert.NotEmpty(t, paths)
	})
}

func TestCollectModuleDependencies_TestScopeFiltered(t *testing.T) {
	root := MavenDependencyJSON{
		GroupID: "com.example", ArtifactID: "app", Version: "1.0",
		Children: []MavenDependencyJSON{
			{GroupID: "junit", ArtifactID: "junit", Version: "4.13", Scope: "test"},
			{GroupID: "com.google.guava", ArtifactID: "guava", Version: "32.1", Scope: "compile"},
		},
	}

	excluded := &MavenFlexPack{config: MavenConfig{IncludeTestDependencies: false}}
	deps, _ := excluded.collectModuleDependencies("com.example:app:1.0", root)
	assert.ElementsMatch(t, []string{"com.google.guava:guava:32.1"}, depIDs(deps), "test dep excluded")

	included := &MavenFlexPack{config: MavenConfig{IncludeTestDependencies: true}}
	deps, _ = included.collectModuleDependencies("com.example:app:1.0", root)
	assert.ElementsMatch(t, []string{"junit:junit:4.13", "com.google.guava:guava:32.1"}, depIDs(deps), "test dep included")
}

func TestBuildModuleFromTreeFile(t *testing.T) {
	dir := t.TempDir()
	treePath := filepath.Join(dir, mavenDepsFileName)
	// root node = the module itself (packaging in "type"); children = its dependencies.
	treeJSON := `{
	  "groupId": "com.example", "artifactId": "app", "version": "1.0", "type": "war",
	  "children": [
	    {"groupId": "com.google.guava", "artifactId": "guava", "version": "32.1", "scope": "compile"}
	  ]
	}`
	require.NoError(t, os.WriteFile(treePath, []byte(treeJSON), 0600))

	mf := &MavenFlexPack{config: MavenConfig{IncludeTestDependencies: true}, moduleLocations: make(map[string]ModuleLocation)}
	module, err := mf.buildModuleFromTreeFile(treePath)
	require.NoError(t, err)

	assert.Equal(t, "com.example:app:1.0", module.Id)
	assert.Equal(t, entities.Maven, module.Type)
	require.Len(t, module.Dependencies, 1)
	assert.Equal(t, "com.google.guava:guava:32.1", module.Dependencies[0].Id)

	// Location recorded: dir = the tree file's directory, packaging = root "type".
	loc, ok := mf.GetModuleLocations()["com.example:app:1.0"]
	require.True(t, ok)
	assert.Equal(t, dir, loc.Dir)
	assert.Equal(t, "war", loc.Packaging)
}

func TestBuildModuleFromTreeFile_MissingCoordinates(t *testing.T) {
	dir := t.TempDir()
	treePath := filepath.Join(dir, mavenDepsFileName)
	require.NoError(t, os.WriteFile(treePath, []byte(`{"groupId": "", "artifactId": "", "version": ""}`), 0600))

	mf := &MavenFlexPack{config: MavenConfig{}, moduleLocations: make(map[string]ModuleLocation)}
	_, err := mf.buildModuleFromTreeFile(treePath)
	assert.Error(t, err)
}

func TestLocalRepositoryPath(t *testing.T) {
	t.Run("honors maven.repo.local override", func(t *testing.T) {
		mf := &MavenFlexPack{config: MavenConfig{ExtraArgs: []string{"-Pprod", "-Dmaven.repo.local=/custom/repo"}}}
		assert.Equal(t, "/custom/repo", mf.localRepositoryPath())
	})
	t.Run("last override wins", func(t *testing.T) {
		mf := &MavenFlexPack{config: MavenConfig{ExtraArgs: []string{"-Dmaven.repo.local=/a", "-Dmaven.repo.local=/b"}}}
		assert.Equal(t, "/b", mf.localRepositoryPath())
	})
	t.Run("falls back to default .m2 when no override", func(t *testing.T) {
		mf := &MavenFlexPack{config: MavenConfig{ExtraArgs: []string{"-Pprod"}}}
		got := mf.localRepositoryPath()
		assert.True(t, filepath.IsAbs(got))
		assert.Contains(t, got, filepath.Join(".m2", "repository"))
	})
}

func depIDs(deps []DependencyInfo) []string {
	ids := make([]string, 0, len(deps))
	for _, d := range deps {
		ids = append(ids, d.ID)
	}
	return ids
}

func TestRepoURLFromAltValue(t *testing.T) {
	assert.Equal(t, "https://acme/artifactory/libs-release", repoURLFromAltValue("id::default::https://acme/artifactory/libs-release"))
	assert.Equal(t, "https://acme/artifactory/libs-release", repoURLFromAltValue("id::https://acme/artifactory/libs-release"))
	assert.Equal(t, "https://acme/artifactory/libs-release", repoURLFromAltValue("https://acme/artifactory/libs-release"))
}

func TestAltDeploymentRepoURLFromArgs(t *testing.T) {
	t.Run("general", func(t *testing.T) {
		args := []string{"-DskipTests", "-DaltDeploymentRepository=id::default::https://acme/artifactory/general"}
		assert.Equal(t, "https://acme/artifactory/general", altDeploymentRepoURLFromArgs(args, false))
		assert.Equal(t, "https://acme/artifactory/general", altDeploymentRepoURLFromArgs(args, true))
	})
	t.Run("release-specific wins for release", func(t *testing.T) {
		args := []string{
			"-DaltDeploymentRepository=id::default::https://acme/artifactory/general",
			"-DaltReleaseDeploymentRepository=id::default::https://acme/artifactory/releases",
		}
		assert.Equal(t, "https://acme/artifactory/releases", altDeploymentRepoURLFromArgs(args, false))
	})
	t.Run("snapshot-specific wins for snapshot", func(t *testing.T) {
		args := []string{
			"-DaltDeploymentRepository=id::default::https://acme/artifactory/general",
			"-DaltSnapshotDeploymentRepository=id::default::https://acme/artifactory/snapshots",
		}
		assert.Equal(t, "https://acme/artifactory/snapshots", altDeploymentRepoURLFromArgs(args, true))
	})
	t.Run("none", func(t *testing.T) {
		assert.Equal(t, "", altDeploymentRepoURLFromArgs([]string{"-Pprod", "-DskipTests"}, false))
	})
}

func TestParseModuleDeployURLs(t *testing.T) {
	const dm = `<distributionManagement>
	  <repository><url>https://acme/artifactory/libs-release</url></repository>
	  <snapshotRepository><url>https://acme/artifactory/libs-snapshot</url></snapshotRepository>
	</distributionManagement>`

	t.Run("effective SNAPSHOT version picks snapshotRepository (raw ${revision} would misfire)", func(t *testing.T) {
		content := `<project><groupId>com.acme</groupId><artifactId>app</artifactId><version>1.0.0-SNAPSHOT</version>` + dm + `</project>`
		urls, err := parseModuleDeployURLs(content, false /*raw said release*/)
		assert.NoError(t, err)
		assert.Equal(t, map[string]string{"com.acme:app:1.0.0-SNAPSHOT": "https://acme/artifactory/libs-snapshot"}, urls)
	})
	t.Run("effective release version picks repository", func(t *testing.T) {
		content := `<project><groupId>com.acme</groupId><artifactId>app</artifactId><version>1.0.0</version>` + dm + `</project>`
		urls, err := parseModuleDeployURLs(content, true /*raw said snapshot*/)
		assert.NoError(t, err)
		assert.Equal(t, "https://acme/artifactory/libs-release", urls["com.acme:app:1.0.0"])
	})
	t.Run("reactor: each module maps to its OWN repo", func(t *testing.T) {
		content := `<projects>
		  <project><groupId>com.acme</groupId><artifactId>lib</artifactId><version>1.0.0</version>
		    <distributionManagement><repository><url>https://acme/artifactory/repo-lib</url></repository></distributionManagement></project>
		  <project><groupId>com.acme</groupId><artifactId>app</artifactId><version>1.0.0</version>
		    <distributionManagement><repository><url>https://acme/artifactory/repo-app</url></repository></distributionManagement></project>
		</projects>`
		urls, err := parseModuleDeployURLs(content, false)
		assert.NoError(t, err)
		assert.Equal(t, "https://acme/artifactory/repo-lib", urls["com.acme:lib:1.0.0"])
		assert.Equal(t, "https://acme/artifactory/repo-app", urls["com.acme:app:1.0.0"])
	})
	t.Run("module without distributionManagement is omitted", func(t *testing.T) {
		content := `<project><groupId>com.acme</groupId><artifactId>app</artifactId><version>1.0.0</version></project>`
		urls, err := parseModuleDeployURLs(content, false)
		assert.NoError(t, err)
		assert.Empty(t, urls)
	})
}
