package entities

import (
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
			Checksum: &Checksum{
				Sha1: "1",
				Md5:  "2",
			},
		}},
		Dependencies: []Dependency{{
			Id:   "alpine",
			Type: "docker",
			Checksum: &Checksum{
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
			Checksum: &Checksum{
				Sha1: "1",
				Md5:  "2",
			},
		}},
		Dependencies: []Dependency{{
			Id:   "alpine",
			Type: "docker",
			Checksum: &Checksum{
				Sha1: "3",
				Md5:  "4",
			},
		}},
	}}
	assert.True(t, IsEqualModuleSlices(a, b))

	b[0].Type = "other"
	assert.False(t, IsEqualModuleSlices(a, b))

	b[0].Type = "docker"
	b[0].Id = "other"
	assert.False(t, IsEqualModuleSlices(a, b))

	b[0].Id = "manifest"
	b[0].Artifacts[0].Name = "other"
	assert.False(t, IsEqualModuleSlices(a, b))

	b[0].Artifacts[0].Name = "layer"
	newDependency := Dependency{
		Id:   "alpine",
		Type: "docker",
		Checksum: &Checksum{
			Sha1: "3",
			Md5:  "4",
		},
	}
	b[0].Dependencies = append(b[0].Dependencies, newDependency)
	assert.False(t, IsEqualModuleSlices(a, b))
	a[0].Dependencies = append(a[0].Dependencies, newDependency)
	assert.True(t, IsEqualModuleSlices(a, b))

	newArtifact := Artifact{
		Name:     "a",
		Type:     "s",
		Path:     "s",
		Checksum: &Checksum{},
	}
	a[0].Artifacts = append(a[0].Artifacts, newArtifact)
	assert.False(t, IsEqualModuleSlices(a, b))

}
