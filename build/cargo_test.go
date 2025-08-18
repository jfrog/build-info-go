package build

import (
	"github.com/jfrog/build-info-go/utils"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBuildInfoForCargoProject(t *testing.T) {
	if utils.IsWindows() {
		return
	}
	service := NewBuildInfoService()
	cargoBuild, err := service.GetOrCreateBuild("build-info-go-test-cargo4", "5")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, cargoBuild.Clean())
	}()
	cargoModule, err := cargoBuild.AddCargoModule(filepath.Join("testdata", "cargo", "project"))
	if assert.NoError(t, err) {
		err = cargoModule.CalcDependencies()
		assert.NoError(t, err)
		err = cargoModule.AddArtifacts(entities.Artifact{Name: "artifactName", Type: "artifactType", Path: "artifactPath", Checksum: entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}})
		assert.NoError(t, err)
		buildInfo, err := cargoBuild.ToBuildInfo()
		assert.NoError(t, err)
		assert.Len(t, buildInfo.Modules, 1)
		validateModule(t, buildInfo.Modules[0], 6, 1, "jfrog-dependency:0.0.2", entities.Cargo, true)
		validateRequestedByCargo(t, buildInfo.Modules[0])
	}
}
func validateRequestedByCargo(t *testing.T, module entities.Module) {
	for _, dep := range module.Dependencies {
		if assert.NotEmpty(t, dep.RequestedBy, dep.Id+" RequestedBy field is empty") {
			switch dep.Id {
			// Direct dependencies:
			case "want:0.3.1", "http:0.2.11":
				assert.Equal(t, [][]string{{module.Id}}, dep.RequestedBy)

			// Indirect dependencies:
			case "try-lock:0.2.5":
				assert.Equal(t, [][]string{{"want:0.3.1", module.Id}}, dep.RequestedBy)

			case "itoa:1.0.10":
				assert.Equal(t, [][]string{{"http:0.2.11", module.Id}}, dep.RequestedBy)
			case "bytes:1.5.0":
				assert.Equal(t, [][]string{{"http:0.2.11", module.Id}}, dep.RequestedBy)
			case "fnv:1.0.7":
				assert.Equal(t, [][]string{{"http:0.2.11", module.Id}}, dep.RequestedBy)

			default:
				assert.Fail(t, "Unexpected dependency "+dep.Id)
			}
		}
	}
}
