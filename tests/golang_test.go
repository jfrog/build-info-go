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
	goBuild, err := service.GetOrCreateBuild("build-info-go-test-golang", "1")
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
	validateModule(t, buildInfo.Modules[0], 4, 1, "github.com/jfrog/dependency", entities.Go)
}
