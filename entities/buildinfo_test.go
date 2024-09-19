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
