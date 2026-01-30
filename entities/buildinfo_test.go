package entities

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEqualModuleSlices(t *testing.T) {
	a := []Module{{
		Type: "docker",
		Id:   "manifest",
		Artifacts: []Artifact{{
			Name: "layer",
			Type: "",
			Path: "path/to/somewhere",
			Checksum: Checksum{
				Sha1: "1",
				Md5:  "2",
			},
		}},
		Dependencies: []Dependency{{
			Id:   "alpine",
			Type: "docker",
			Checksum: Checksum{
				Sha1: "3",
				Md5:  "4",
			},
		}},
	}}
	b := []Module{{
		Type: "docker",
		Id:   "manifest",
		Artifacts: []Artifact{{
			Name: "layer",
			Type: "",
			Path: "path/to/somewhere",
			Checksum: Checksum{
				Sha1: "1",
				Md5:  "2",
			},
		}},
		Dependencies: []Dependency{{
			Id:   "alpine",
			Type: "docker",
			Checksum: Checksum{
				Sha1: "3",
				Md5:  "4",
			},
		}},
	}}
	match, err := IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.True(t, match)

	b[0].Type = "other"
	match, err = IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.False(t, match)

	b[0].Type = "docker"
	b[0].Id = "other"
	match, err = IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.False(t, match)

	b[0].Id = "manifest"
	b[0].Artifacts[0].Name = "other"
	match, err = IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.False(t, match)

	b[0].Artifacts[0].Name = "layer"
	newDependency := Dependency{
		Id:   "alpine",
		Type: "docker",
		Checksum: Checksum{
			Sha1: "3",
			Md5:  "4",
		},
	}
	b[0].Dependencies = append(b[0].Dependencies, newDependency)
	match, err = IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.False(t, match)
	a[0].Dependencies = append(a[0].Dependencies, newDependency)
	match, err = IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.True(t, match)

	newArtifact := Artifact{
		Name:     "a",
		Type:     "s",
		Path:     "s",
		Checksum: Checksum{},
	}
	a[0].Artifacts = append(a[0].Artifacts, newArtifact)
	match, err = IsEqualModuleSlices(a, b)
	assert.NoError(t, err)
	assert.False(t, match)
}

func TestIsEqualModuleSlicesRegex(t *testing.T) {
	actual := []Module{{
		Type: "docker",
		Id:   "manifest",
		Artifacts: []Artifact{{
			Name: "sha256__7d3b33ae048d1",
			Type: "json",
			Path: "image-name-multiarch-image/sha256:a56b64f",
			Checksum: Checksum{
				Sha1: "1",
				Md5:  "2",
			},
		}},
		Dependencies: []Dependency{{
			Id:   "sha256__7d3b33ae048d1",
			Type: "docker",
			Checksum: Checksum{
				Sha1: "3",
				Md5:  "4",
			},
		}},
	}}
	expected := []Module{{
		Type: "docker",
		Id:   "manifest",
		Artifacts: []Artifact{{
			Name: "sha256__*",
			Type: "json",
			Path: "image-name-multiarch-image*",
			Checksum: Checksum{
				Sha1: ".+",
				Md5:  ".+",
			},
		}},
		Dependencies: []Dependency{{
			Id:   "sha256__*",
			Type: "docker",
			Checksum: Checksum{
				Sha1: ".+",
				Md5:  ".+",
			},
		}},
	}}
	match, err := IsEqualModuleSlices(actual, expected)
	assert.NoError(t, err)
	assert.True(t, match)
	// Validate dependencies
	actual[0].Dependencies[0].Sha1 = ""
	match, err = IsEqualModuleSlices(actual, expected)
	assert.NoError(t, err)
	assert.False(t, match)
	actual[0].Dependencies[0].Sha1 = "123"

	actual[0].Dependencies[0].Id = ""
	match, err = IsEqualModuleSlices(actual, expected)
	assert.NoError(t, err)
	assert.False(t, match)
	actual[0].Dependencies[0].Id = "sha256__7d3b33ae048d1"

	// Validate artifact
	actual[0].Artifacts[0].Sha1 = ""
	match, err = IsEqualModuleSlices(actual, expected)
	assert.NoError(t, err)
	assert.False(t, match)
	actual[0].Artifacts[0].Sha1 = "123"

	actual[0].Artifacts[0].Path = "a"
	match, err = IsEqualModuleSlices(actual, expected)
	assert.NoError(t, err)
	assert.False(t, match)
	actual[0].Artifacts[0].Sha1 = "image-name-multiarch-image/sha256:a56b64f"

	actual[0].Artifacts[0].Name = ""
	match, err = IsEqualModuleSlices(actual, expected)
	assert.NoError(t, err)
	assert.False(t, match)
}

func TestMergeDependenciesLists(t *testing.T) {
	dependenciesToAdd := []Dependency{
		{Id: "test-dep1", Type: "tst", Scopes: []string{"a", "b"}, RequestedBy: [][]string{{"a", "b"}, {"b", "a"}}},
		{Id: "test-dep2", Type: "tst", Scopes: []string{"a"}, RequestedBy: [][]string{{"a", "b"}}, Checksum: Checksum{Sha1: "123"}},
		{Id: "test-dep3", Type: "tst"},
		{Id: "test-dep4", Type: "tst"},
	}
	intoDependencies := []Dependency{
		{Id: "test-dep1", Type: "tst", Scopes: []string{"a"}, RequestedBy: [][]string{{"b", "a"}}},
		{Id: "test-dep2", Type: "tst", Scopes: []string{"b"}, RequestedBy: [][]string{{"a", "c"}}, Checksum: Checksum{Sha1: "123"}},
		{Id: "test-dep3", Type: "tst", Scopes: []string{"a"}, RequestedBy: [][]string{{"a", "b"}}},
	}
	expectedMergedDependencies := []Dependency{
		{Id: "test-dep1", Type: "tst", Scopes: []string{"a", "b"}, RequestedBy: [][]string{{"b", "a"}, {"a", "b"}}},
		{Id: "test-dep2", Type: "tst", Scopes: []string{"b"}, RequestedBy: [][]string{{"a", "c"}}, Checksum: Checksum{Sha1: "123"}},
		{Id: "test-dep3", Type: "tst", Scopes: []string{"a"}, RequestedBy: [][]string{{"a", "b"}}},
		{Id: "test-dep4", Type: "tst"},
	}
	mergeDependenciesLists(&dependenciesToAdd, &intoDependencies)
	reflect.DeepEqual(expectedMergedDependencies, intoDependencies)
}

func TestAppend(t *testing.T) {
	artifactA := Artifact{Name: "artifact-a", Checksum: Checksum{Sha1: "artifact-a-sha"}}
	artifactB := Artifact{Name: "artifact-b", Checksum: Checksum{Sha1: "artifact-b-sha"}}
	artifactC := Artifact{Name: "artifact-c", Checksum: Checksum{Sha1: "artifact-c-sha"}}

	dependencyA := Dependency{Id: "dependency-a", Checksum: Checksum{Sha1: "dependency-a-sha"}}
	dependencyB := Dependency{Id: "dependency-b", Checksum: Checksum{Sha1: "dependency-b-sha"}}
	dependencyC := Dependency{Id: "dependency-c", Checksum: Checksum{Sha1: "dependency-c-sha"}}

	buildInfo1 := BuildInfo{
		Modules: []Module{{
			Id:           "module-id",
			Artifacts:    []Artifact{artifactA, artifactB},
			Dependencies: []Dependency{dependencyA, dependencyB},
		}},
	}

	buildInfo2 := BuildInfo{
		Modules: []Module{{
			Id:           "module-id",
			Artifacts:    []Artifact{artifactA, artifactC},
			Dependencies: []Dependency{dependencyA, dependencyC},
		}},
	}

	expected := BuildInfo{
		Modules: []Module{{
			Id:           "module-id",
			Artifacts:    []Artifact{artifactA, artifactB, artifactC},
			Dependencies: []Dependency{dependencyA, dependencyB, dependencyC},
		}},
	}

	buildInfo1.Append(&buildInfo2)
	results, err := IsEqualModuleSlices(expected.Modules, buildInfo1.Modules)
	assert.NoError(t, err)
	assert.True(t, results)
}

func TestToCycloneDxBOM(t *testing.T) {
	dependencyA := Dependency{Id: "dependency-a", Checksum: Checksum{Sha1: "dependency-a-sha"}, RequestedBy: [][]string{{"dependency-c"}}}
	dependencyB := Dependency{Id: "dependency-b", Checksum: Checksum{Sha1: "dependency-b-sha"}, RequestedBy: [][]string{{"dependency-b"}, {"dependency-c"}}}
	dependencyC := Dependency{Id: "dependency-c", Checksum: Checksum{Sha1: "dependency-c-sha"}}

	buildInfo := BuildInfo{
		Modules: []Module{{
			Id:           "module-id1",
			Dependencies: []Dependency{dependencyC, dependencyB, dependencyA},
		}},
	}

	cdxBom, err := buildInfo.ToCycloneDxBom()
	assert.NoError(t, err)

	componentsIsSorted := sort.SliceIsSorted(*cdxBom.Components, func(i, j int) bool {
		return (*cdxBom.Components)[i].BOMRef < (*cdxBom.Components)[j].BOMRef
	})
	assert.True(t, componentsIsSorted)

	dependenciesIsSorted := sort.SliceIsSorted(*cdxBom.Dependencies, func(i, j int) bool {
		return (*cdxBom.Dependencies)[i].Ref < (*cdxBom.Dependencies)[j].Ref
	})
	assert.True(t, dependenciesIsSorted)

	for _, dep := range *cdxBom.Dependencies {
		dependsOnIsSorted := sort.SliceIsSorted(*dep.Dependencies, func(i, j int) bool {
			return (*dep.Dependencies)[i] < (*dep.Dependencies)[j]
		})
		assert.True(t, dependsOnIsSorted)
	}
}

// ==================== extractPathDir Tests ====================

func TestExtractPathDir(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"full path", "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-SNAPSHOT.war", "org/jfrog/test/multi3/3.7-SNAPSHOT"},
		{"simple path", "repo/artifact.jar", "repo"},
		{"deep path", "a/b/c/d/e/file.txt", "a/b/c/d/e"},
		{"no directory", "file.jar", ""},
		{"empty path", "", ""},
		{"trailing slash", "repo/dir/", "repo/dir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPathDir(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ==================== isSameLogicalArtifact Tests ====================

func TestIsSameLogicalArtifact_SameDirectory(t *testing.T) {
	existing := Artifact{
		Name: "artifact.war",
		Path: "org/jfrog/test/1.0-SNAPSHOT/artifact-1.war",
	}
	new := Artifact{
		Name: "artifact.war",
		Path: "org/jfrog/test/1.0-SNAPSHOT/artifact-2.war",
	}

	assert.True(t, isSameLogicalArtifact(existing, new), "Same directory should return true")
}

func TestIsSameLogicalArtifact_DifferentDirectory(t *testing.T) {
	existing := Artifact{
		Name: "artifact.war",
		Path: "org/jfrog/test/1.0-SNAPSHOT/artifact.war",
	}
	new := Artifact{
		Name: "artifact.war",
		Path: "org/jfrog/other/2.0-SNAPSHOT/artifact.war",
	}

	assert.False(t, isSameLogicalArtifact(existing, new), "Different directory should return false")
}

func TestIsSameLogicalArtifact_SameRepo(t *testing.T) {
	existing := Artifact{
		Name:                   "artifact.war",
		Path:                   "org/jfrog/test/artifact.war",
		OriginalDeploymentRepo: "libs-snapshot",
	}
	new := Artifact{
		Name:                   "artifact.war",
		Path:                   "org/jfrog/test/artifact-v2.war",
		OriginalDeploymentRepo: "libs-snapshot",
	}

	assert.True(t, isSameLogicalArtifact(existing, new), "Same repo should return true")
}

func TestIsSameLogicalArtifact_DifferentRepo(t *testing.T) {
	existing := Artifact{
		Name:                   "artifact.war",
		Path:                   "org/jfrog/test/artifact.war",
		OriginalDeploymentRepo: "libs-snapshot",
	}
	new := Artifact{
		Name:                   "artifact.war",
		Path:                   "org/jfrog/test/artifact-v2.war",
		OriginalDeploymentRepo: "libs-release", // Different repo
	}

	assert.False(t, isSameLogicalArtifact(existing, new), "Different repos should return false")
}

func TestIsSameLogicalArtifact_OneRepoEmpty(t *testing.T) {
	existing := Artifact{
		Name:                   "artifact.war",
		Path:                   "org/jfrog/test/artifact.war",
		OriginalDeploymentRepo: "libs-snapshot",
	}
	new := Artifact{
		Name:                   "artifact.war",
		Path:                   "org/jfrog/test/artifact-v2.war",
		OriginalDeploymentRepo: "", // Empty repo - should still match
	}

	assert.True(t, isSameLogicalArtifact(existing, new), "One empty repo should return true")
}

// ==================== mergeArtifacts Tests ====================

// TestMergeArtifacts_SameSHA1Skipped tests Priority 1: same SHA1 → skip
func TestMergeArtifacts_SameSHA1Skipped(t *testing.T) {
	existingArtifacts := []Artifact{
		{
			Name:     "artifact.war",
			Path:     "org/example/artifact.war",
			Checksum: Checksum{Sha1: "same-sha1"},
		},
	}

	newArtifacts := []Artifact{
		{
			Name:     "artifact.war",
			Path:     "different/path/artifact.war", // Different path but same SHA1
			Checksum: Checksum{Sha1: "same-sha1"},   // SAME SHA1
		},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	assert.Len(t, existingArtifacts, 1, "Should have 1 artifact (duplicate skipped)")
	assert.Equal(t, "org/example/artifact.war", existingArtifacts[0].Path, "Should keep existing path")
}

// TestMergeArtifacts_SameNameSameDir tests Priority 2: same name + same dir → replace
func TestMergeArtifacts_SameNameSameDir(t *testing.T) {
	existingArtifacts := []Artifact{
		{
			Name:     "multi3-3.7-SNAPSHOT.war",
			Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127-1.war",
			Checksum: Checksum{Sha1: "old-sha1"},
		},
	}

	newArtifacts := []Artifact{
		{
			Name:     "multi3-3.7-SNAPSHOT.war",
			Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127-2.war", // Same dir
			Checksum: Checksum{Sha1: "new-sha1"},                                     // Different SHA1
		},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	assert.Len(t, existingArtifacts, 1, "Should have 1 artifact (replaced)")
	assert.Equal(t, "new-sha1", existingArtifacts[0].Sha1, "Should have new SHA1")
	assert.Equal(t, "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127-2.war", existingArtifacts[0].Path, "Should have new path")
}

// TestMergeArtifacts_SameNameDifferentDir tests same name but different dir → add as new
func TestMergeArtifacts_SameNameDifferentDir(t *testing.T) {
	existingArtifacts := []Artifact{
		{
			Name:     "artifact.jar",
			Path:     "org/jfrog/module-a/1.0/artifact.jar",
			Checksum: Checksum{Sha1: "sha1-a"},
		},
	}

	newArtifacts := []Artifact{
		{
			Name:     "artifact.jar",                        // Same name
			Path:     "org/jfrog/module-b/1.0/artifact.jar", // Different directory
			Checksum: Checksum{Sha1: "sha1-b"},
		},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts (different directories)")
}

// TestMergeArtifacts_DifferentNames tests different names → add as new
func TestMergeArtifacts_DifferentNames(t *testing.T) {
	existingArtifacts := []Artifact{
		{Name: "artifact-a.jar", Path: "repo/artifact-a.jar", Checksum: Checksum{Sha1: "sha1-a"}},
	}

	newArtifacts := []Artifact{
		{Name: "artifact-b.jar", Path: "repo/artifact-b.jar", Checksum: Checksum{Sha1: "sha1-b"}},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts")
}

// TestMergeArtifacts_MavenSnapshotScenario tests the real-world Maven install + deploy scenario
func TestMergeArtifacts_MavenSnapshotScenario(t *testing.T) {
	// After: jf mvn install
	existingArtifacts := []Artifact{
		{
			Name:     "multi3-3.7-SNAPSHOT.war",
			Type:     "war",
			Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-1.war",
			Checksum: Checksum{Sha1: "install-sha1"},
		},
		{
			Name:     "multi3-3.7-SNAPSHOT.pom",
			Type:     "pom",
			Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-SNAPSHOT.pom",
			Checksum: Checksum{Sha1: "pom-sha1"},
		},
	}

	// After: jf mvn deploy (WAR rebuilt with new SHA1)
	newArtifacts := []Artifact{
		{
			Name:     "multi3-3.7-SNAPSHOT.war",
			Type:     "war",
			Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-2.war",
			Checksum: Checksum{Sha1: "deploy-sha1"}, // Different SHA1
		},
		{
			Name:     "multi3-3.7-SNAPSHOT.pom",
			Type:     "pom",
			Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-SNAPSHOT.pom",
			Checksum: Checksum{Sha1: "pom-sha1"}, // Same SHA1
		},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	// Should have 2 artifacts: WAR replaced, POM skipped (same SHA1)
	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts (no duplicates)")

	for _, artifact := range existingArtifacts {
		if artifact.Name == "multi3-3.7-SNAPSHOT.war" {
			assert.Equal(t, "deploy-sha1", artifact.Sha1, "WAR should have deploy SHA1")
		}
		if artifact.Name == "multi3-3.7-SNAPSHOT.pom" {
			assert.Equal(t, "pom-sha1", artifact.Sha1, "POM should be unchanged")
		}
	}
}

// TestMergeArtifacts_MultipleRebuilds simulates multiple Maven builds
func TestMergeArtifacts_MultipleRebuilds(t *testing.T) {
	existingArtifacts := []Artifact{
		{Name: "app.jar", Path: "repo/app/1.0/app.jar", Checksum: Checksum{Sha1: "build1-sha"}},
		{Name: "app.war", Path: "repo/app/1.0/app.war", Checksum: Checksum{Sha1: "build1-war-sha"}},
	}

	// Second build
	secondBuild := []Artifact{
		{Name: "app.jar", Path: "repo/app/1.0/app.jar", Checksum: Checksum{Sha1: "build2-sha"}},
		{Name: "app.war", Path: "repo/app/1.0/app.war", Checksum: Checksum{Sha1: "build2-war-sha"}},
	}

	mergeArtifacts(&secondBuild, &existingArtifacts)

	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts")
	for _, artifact := range existingArtifacts {
		if artifact.Name == "app.jar" {
			assert.Equal(t, "build2-sha", artifact.Sha1, "JAR should be from build 2")
		}
		if artifact.Name == "app.war" {
			assert.Equal(t, "build2-war-sha", artifact.Sha1, "WAR should be from build 2")
		}
	}
}

// TestBuildInfoAppend_MavenSnapshotScenario tests full BuildInfo.Append
func TestBuildInfoAppend_MavenSnapshotScenario(t *testing.T) {
	buildInfo1 := BuildInfo{
		Modules: []Module{{
			Id: "org.jfrog.test:multi3:3.7-SNAPSHOT",
			Artifacts: []Artifact{
				{
					Name:     "multi3-3.7-SNAPSHOT.war",
					Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-1.war",
					Checksum: Checksum{Sha1: "install-sha"},
				},
			},
		}},
	}

	buildInfo2 := BuildInfo{
		Modules: []Module{{
			Id: "org.jfrog.test:multi3:3.7-SNAPSHOT",
			Artifacts: []Artifact{
				{
					Name:     "multi3-3.7-SNAPSHOT.war",
					Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-2.war",
					Checksum: Checksum{Sha1: "deploy-sha"},
				},
			},
		}},
	}

	buildInfo1.Append(&buildInfo2)

	assert.Len(t, buildInfo1.Modules, 1, "Should have 1 module")
	assert.Len(t, buildInfo1.Modules[0].Artifacts, 1, "Should have 1 artifact")
	assert.Equal(t, "deploy-sha", buildInfo1.Modules[0].Artifacts[0].Sha1, "Should have newer SHA1")
}
