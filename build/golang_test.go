package build

import (
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBuildInfoForGoProject(t *testing.T) {
	service := NewBuildInfoService()
	goBuild, err := service.GetOrCreateBuild("build-info-go-test-golang", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, goBuild.Clean())
	}()
	goModule, err := goBuild.AddGoModule(filepath.Join("testdata", "golang", "project"))
	if assert.NoError(t, err) {
		err = goModule.CalcDependencies()
		assert.NoError(t, err)
		err = goModule.AddArtifacts(entities.Artifact{Name: "artifactName", Type: "artifactType", Path: "artifactPath", Checksum: entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}})
		assert.NoError(t, err)
		buildInfo, err := goBuild.ToBuildInfo()
		assert.NoError(t, err)
		assert.Len(t, buildInfo.Modules, 1)
		validateModule(t, buildInfo.Modules[0], 6, 1, "github.com/jfrog/dependency", entities.Go, true)
		validateRequestedBy(t, buildInfo.Modules[0])
	}
}

func validateRequestedBy(t *testing.T, module entities.Module) {
	for _, dep := range module.Dependencies {
		if assert.NotEmpty(t, dep.RequestedBy, dep.Id+" RequestedBy field is empty") {
			switch dep.Id {
			// Direct dependencies:
			case "rsc.io/quote:v1.5.2", "github.com/jfrog/gofrog:v1.1.1":
				assert.Equal(t, [][]string{{module.Id}}, dep.RequestedBy)

			// Indirect dependencies:
			case "golang.org/x/text:v0.0.0-20170915032832-14c0d48ead0c":
				assert.Equal(t, [][]string{{"rsc.io/sampler:v1.3.0", "rsc.io/quote:v1.5.2", module.Id}}, dep.RequestedBy)

			case "rsc.io/sampler:v1.3.0":
				assert.Equal(t, [][]string{{"rsc.io/quote:v1.5.2", module.Id}}, dep.RequestedBy)

			// 2 requestedBy lists:
			case "github.com/pkg/errors:v0.8.0":
				assert.Equal(t, [][]string{
					{"github.com/jfrog/gofrog:v1.1.1", module.Id},
					{module.Id},
				}, dep.RequestedBy)

			// Uppercase encoded module (!burnt!sushi --> BurntSushi)
			case "github.com/!burnt!sushi/toml:v0.4.2-0.20211125115023-7d0236fe7476":
				assert.Equal(t, [][]string{{module.Id}}, dep.RequestedBy)

			default:
				assert.Fail(t, "Unexpected dependency "+dep.Id)
			}
		}
	}
}
