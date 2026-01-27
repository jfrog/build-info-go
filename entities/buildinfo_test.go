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

// TestMergeArtifacts_SameNameDifferentSHA1 tests the fix for the Maven SNAPSHOT artifact duplicate issue
// When artifacts have the same name but different SHA1s (e.g., rebuilt Maven WAR files),
// the newer artifact should replace the older one, not create a duplicate entry.
// The newer SHA1 is used, but the path is preserved if the newer one doesn't have it.
func TestMergeArtifacts_SameNameDifferentSHA1(t *testing.T) {
	// Scenario: Maven WAR file rebuilt between install and deploy
	// First build: multi3-3.7-SNAPSHOT.war with SHA1: old-sha1
	existingArtifacts := []Artifact{
		{
			Name: "multi3-3.7-SNAPSHOT.war",
			Type: "war",
			Path: "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-1.war",
			Checksum: Checksum{
				Sha1: "old-sha1-from-first-build",
				Md5:  "old-md5",
			},
		},
		{
			Name: "multi3-3.7-SNAPSHOT.pom",
			Type: "pom",
			Path: "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-SNAPSHOT.pom",
			Checksum: Checksum{
				Sha1: "pom-sha1",
				Md5:  "pom-md5",
			},
		},
	}

	// Second build: multi3-3.7-SNAPSHOT.war rebuilt with new SHA1 and new path
	newArtifacts := []Artifact{
		{
			Name: "multi3-3.7-SNAPSHOT.war",
			Type: "war",
			Path: "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-2.war",
			Checksum: Checksum{
				Sha1: "new-sha1-from-second-build",
				Md5:  "new-md5",
			},
		},
		{
			Name: "multi3-3.7-SNAPSHOT.pom",
			Type: "pom",
			Path: "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-SNAPSHOT.pom",
			Checksum: Checksum{
				Sha1: "pom-sha1", // POM unchanged
				Md5:  "pom-md5",
			},
		},
	}

	// Merge artifacts
	mergeArtifacts(&newArtifacts, &existingArtifacts)

	// Verify results
	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts, not 3 (no duplicate WAR)")

	// Find the WAR artifact
	var warArtifact *Artifact
	for i := range existingArtifacts {
		if existingArtifacts[i].Name == "multi3-3.7-SNAPSHOT.war" {
			warArtifact = &existingArtifacts[i]
			break
		}
	}

	// Verify the WAR was merged with newer SHA1 and newer path
	assert.NotNil(t, warArtifact, "WAR artifact should exist")
	assert.Equal(t, "new-sha1-from-second-build", warArtifact.Sha1, "WAR should have new SHA1")
	assert.Equal(t, "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-2.war", warArtifact.Path, "WAR should have new path")
}

// TestMergeArtifacts_PathPreservation tests that the path is preserved when the newer artifact doesn't have one
// This handles the case where build info is collected before deployment completes
func TestMergeArtifacts_PathPreservation(t *testing.T) {
	// First artifact: has path (from install/deploy)
	existingArtifacts := []Artifact{
		{
			Name:                   "artifact.war",
			Type:                   "war",
			Path:                   "org/example/artifact/1.0-SNAPSHOT/artifact-1.0-20260127-1.war",
			OriginalDeploymentRepo: "libs-snapshot-local",
			Checksum: Checksum{
				Sha1: "old-sha1",
				Md5:  "old-md5",
			},
		},
	}

	// Second artifact: rebuilt with new checksum but no path yet (build info collected before deployment)
	newArtifacts := []Artifact{
		{
			Name:                   "artifact.war",
			Type:                   "war",
			Path:                   "", // No path yet!
			OriginalDeploymentRepo: "",
			Checksum: Checksum{
				Sha1: "new-sha1",
				Md5:  "new-md5",
			},
		},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	assert.Len(t, existingArtifacts, 1, "Should have 1 artifact")

	// Verify: newer checksums, but old path preserved
	assert.Equal(t, "new-sha1", existingArtifacts[0].Sha1, "Should have new SHA1")
	assert.Equal(t, "org/example/artifact/1.0-SNAPSHOT/artifact-1.0-20260127-1.war",
		existingArtifacts[0].Path, "Should preserve old path when new one is empty")
	assert.Equal(t, "libs-snapshot-local",
		existingArtifacts[0].OriginalDeploymentRepo, "Should preserve deployment repo")
}

// TestMergeArtifacts_DifferentNames tests that artifacts with different names are all preserved
func TestMergeArtifacts_DifferentNames(t *testing.T) {
	existingArtifacts := []Artifact{
		{Name: "artifact-a.jar", Checksum: Checksum{Sha1: "sha1-a"}},
		{Name: "artifact-b.jar", Checksum: Checksum{Sha1: "sha1-b"}},
	}

	newArtifacts := []Artifact{
		{Name: "artifact-c.jar", Checksum: Checksum{Sha1: "sha1-c"}},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	assert.Len(t, existingArtifacts, 3, "Should have 3 distinct artifacts")
	assert.Contains(t, existingArtifacts, Artifact{Name: "artifact-a.jar", Checksum: Checksum{Sha1: "sha1-a"}})
	assert.Contains(t, existingArtifacts, Artifact{Name: "artifact-b.jar", Checksum: Checksum{Sha1: "sha1-b"}})
	assert.Contains(t, existingArtifacts, Artifact{Name: "artifact-c.jar", Checksum: Checksum{Sha1: "sha1-c"}})
}

// TestMergeArtifacts_SameNameSameSHA1 tests backward compatibility when SHA1 matches
func TestMergeArtifacts_SameNameSameSHA1(t *testing.T) {
	existingArtifacts := []Artifact{
		{
			Name:     "artifact.jar",
			Checksum: Checksum{Sha1: "same-sha1"},
		},
	}

	newArtifacts := []Artifact{
		{
			Name:     "artifact.jar",
			Checksum: Checksum{Sha1: "same-sha1"},
		},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	// Should replace by name first, resulting in 1 artifact
	assert.Len(t, existingArtifacts, 1, "Should have 1 artifact (deduplicated by name)")
}

// TestMergeArtifacts_MultipleRebuilds simulates multiple Maven builds with same build name/number
func TestMergeArtifacts_MultipleRebuilds(t *testing.T) {
	// First build
	existingArtifacts := []Artifact{
		{Name: "multi1-3.7-SNAPSHOT.jar", Checksum: Checksum{Sha1: "build1-jar-sha"}},
		{Name: "multi3-3.7-SNAPSHOT.war", Checksum: Checksum{Sha1: "build1-war-sha"}},
	}

	// Second build (both rebuilt with new SHA1s)
	secondBuildArtifacts := []Artifact{
		{Name: "multi1-3.7-SNAPSHOT.jar", Checksum: Checksum{Sha1: "build2-jar-sha"}},
		{Name: "multi3-3.7-SNAPSHOT.war", Checksum: Checksum{Sha1: "build2-war-sha"}},
	}

	mergeArtifacts(&secondBuildArtifacts, &existingArtifacts)

	// Should still have only 2 artifacts (replaced, not duplicated)
	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts (no duplicates)")

	// Verify both were replaced with newer versions
	for _, artifact := range existingArtifacts {
		if artifact.Name == "multi1-3.7-SNAPSHOT.jar" {
			assert.Equal(t, "build2-jar-sha", artifact.Sha1, "JAR should be from build 2")
		}
		if artifact.Name == "multi3-3.7-SNAPSHOT.war" {
			assert.Equal(t, "build2-war-sha", artifact.Sha1, "WAR should be from build 2")
		}
	}
}

// TestMergeArtifacts_EmptyName tests handling of artifacts without names
func TestMergeArtifacts_EmptyName(t *testing.T) {
	existingArtifacts := []Artifact{
		{Name: "", Checksum: Checksum{Sha1: "sha1-a"}},
	}

	newArtifacts := []Artifact{
		{Name: "", Checksum: Checksum{Sha1: "sha1-b"}},
	}

	mergeArtifacts(&newArtifacts, &existingArtifacts)

	// Without names, should fall back to SHA1 comparison
	// Different SHA1s means both should be present
	assert.Len(t, existingArtifacts, 2, "Should have 2 artifacts (no name to match, different SHA1s)")
}

// TestBuildInfoAppend_MavenSnapshotScenario tests the full scenario of Maven install + deploy
func TestBuildInfoAppend_MavenSnapshotScenario(t *testing.T) {
	// BuildInfo after: jf mvn install --build-name=froggy --build-number=10.0.1
	buildInfo1 := BuildInfo{
		Modules: []Module{{
			Id: "org.jfrog.test:multi3:3.7-SNAPSHOT",
			Artifacts: []Artifact{
				{
					Name:     "multi3-3.7-SNAPSHOT.war",
					Type:     "war",
					Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-1.war",
					Checksum: Checksum{Sha1: "war-build1-sha"},
				},
			},
		}},
	}

	// BuildInfo after: jf mvn deploy --build-name=froggy --build-number=10.0.1 (same build!)
	buildInfo2 := BuildInfo{
		Modules: []Module{{
			Id: "org.jfrog.test:multi3:3.7-SNAPSHOT",
			Artifacts: []Artifact{
				{
					Name:     "multi3-3.7-SNAPSHOT.war",
					Type:     "war",
					Path:     "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-2.war",
					Checksum: Checksum{Sha1: "war-build2-sha"},
				},
			},
		}},
	}

	// Append (merge) the second build into the first
	buildInfo1.Append(&buildInfo2)

	// Verify: Should have only ONE WAR artifact (the newer one), not two
	assert.Len(t, buildInfo1.Modules, 1, "Should have 1 module")
	assert.Len(t, buildInfo1.Modules[0].Artifacts, 1, "Should have 1 artifact (no duplicate)")
	assert.Equal(t, "war-build2-sha", buildInfo1.Modules[0].Artifacts[0].Sha1, "Should have newer WAR (from second build)")
	assert.Equal(t, "org/jfrog/test/multi3/3.7-SNAPSHOT/multi3-3.7-20260127.075227-2.war",
		buildInfo1.Modules[0].Artifacts[0].Path, "Should have path from second build")
}
