package tests

import (
	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"testing"
)

func TestGenerateBuildInfoForGoProject(t *testing.T) {
	service := build.NewBuildInfoService()
	goBuild, err := service.GetOrCreateBuild("build-info-go-test", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, goBuild.Clean())
	}()
	goModule, err := goBuild.AddGoModule(filepath.Join("testdata", "golang", "project"))
	assert.NoError(t, err)
	err = goModule.CalcDependencies()
	assert.NoError(t, err)
	err = goModule.AddArtifacts(entities.Artifact{Name: "artifactName", Type: "artifactType", Path: "artifactPath", Checksum: &entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}})
	assert.NoError(t, err)
	buildInfo, err := goBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 5, 1, "github.com/jfrog/dependency", entities.Go)
	validateRequestedBy(t, buildInfo.Modules[0])
}

func validateModule(t *testing.T, module entities.Module, expectedDependencies, expectedArtifacts int, moduleName string, moduleType entities.ModuleType) {
	assert.Equal(t, moduleName, module.Id, "Unexpected module name")
	assert.Len(t, module.Dependencies, expectedDependencies, "Incorrect number of dependencies found in the build-info")
	assert.Len(t, module.Artifacts, expectedArtifacts, "Incorrect number of artifacts found in the build-info")
	assert.Equal(t, module.Type, moduleType)
	assert.Equal(t, module.Artifacts[0].Checksum.Sha1, "123", "Unexpected Sha1 field.")
	assert.Equal(t, module.Artifacts[0].Checksum.Md5, "456", "Unexpected MD5 field.")
	assert.Equal(t, module.Artifacts[0].Checksum.Sha256, "789", "Unexpected SHA256 field.")
	assert.NotEmpty(t, module.Dependencies[0].Checksum.Sha1, "Empty Sha1 field.")
	assert.NotEmpty(t, module.Dependencies[0].Checksum.Md5, "Empty MD5 field.")
	assert.NotEmpty(t, module.Dependencies[0].Checksum.Sha256, "Empty SHA256 field.")
}

func validateRequestedBy(t *testing.T, module entities.Module) {
	for _, dep := range module.Dependencies {
		assert.NotEmpty(t, dep.RequestedBy)
		assert.NotEmpty(t, dep.RequestedBy[0])
		if dep.Id == "github.com/pkg/errors:v0.8.0" {
			assert.Len(t, dep.RequestedBy, 2, "errors:v0.8.0 dependency should contain 2 requestedBy paths")
		}
	}

}
