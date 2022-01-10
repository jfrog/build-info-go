package build

import (
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	buildinfo "github.com/jfrog/build-info-go/entities"
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
	assert.NoError(t, err)
	err = goModule.CalcDependencies()
	assert.NoError(t, err)
	err = goModule.AddArtifacts(entities.Artifact{Name: "artifactName", Type: "artifactType", Path: "artifactPath", Checksum: &entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}})
	assert.NoError(t, err)
	buildInfo, err := goBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 6, 1, "github.com/jfrog/dependency", entities.Go, true)
	validateRequestedBy(t, buildInfo.Modules[0])
}

func validateModule(t *testing.T, module buildinfo.Module, expectedDependencies, expectedArtifacts int, moduleName string, moduleType buildinfo.ModuleType, depsContainChecksums bool) {
	assert.Equal(t, moduleName, module.Id, "Unexpected module name")
	assert.Len(t, module.Dependencies, expectedDependencies, "Incorrect number of dependencies found in the build-info")
	assert.Len(t, module.Artifacts, expectedArtifacts, "Incorrect number of artifacts found in the build-info")
	if expectedArtifacts > 0 {
		assert.Equal(t, "artifactName", module.Artifacts[0].Name, "Unexpected Name field.")
		assert.Equal(t, "artifactType", module.Artifacts[0].Type, "Unexpected Type field.")
		assert.Equal(t, "artifactPath", module.Artifacts[0].Path, "Unexpected Path field.")
		assert.Equal(t, "123", module.Artifacts[0].Checksum.Sha1, "Unexpected Sha1 field.")
		assert.Equal(t, "456", module.Artifacts[0].Checksum.Md5, "Unexpected MD5 field.")
		assert.Equal(t, "789", module.Artifacts[0].Checksum.Sha256, "Unexpected SHA256 field.")
	}
	assert.Equal(t, moduleType, module.Type)
	assert.Equal(t, depsContainChecksums, module.Dependencies[0].Checksum != nil)
	if depsContainChecksums {
		assert.NotEmpty(t, module.Dependencies[0].Checksum.Sha1, "Empty Sha1 field.")
		assert.NotEmpty(t, module.Dependencies[0].Checksum.Md5, "Empty MD5 field.")
		assert.NotEmpty(t, module.Dependencies[0].Checksum.Sha256, "Empty SHA256 field.")
	}
}

func validateRequestedBy(t *testing.T, module entities.Module) {
	for _, dep := range module.Dependencies {
		if assert.NotEmpty(t, dep.RequestedBy, dep.Id+" RequestedBy filed is empty") {
			switch dep.Id {

			case "golang.org/x/text:v0.0.0-20170915032832-14c0d48ead0c":
				assert.Equal(t, [][]string{{"rsc.io/sampler:v1.3.0", "rsc.io/quote:v1.5.2", "github.com/jfrog/dependency"}}, dep.RequestedBy)
			case "rsc.io/quote:v1.5.2":
				assert.Equal(t, [][]string{{"github.com/jfrog/dependency"}}, dep.RequestedBy)
			case "rsc.io/sampler:v1.3.0":
				assert.Equal(t, [][]string{{"rsc.io/quote:v1.5.2", "github.com/jfrog/dependency"}}, dep.RequestedBy)
			case "github.com/jfrog/gofrog:v1.1.1":
				assert.Equal(t, [][]string{{"github.com/jfrog/dependency"}}, dep.RequestedBy)

				// 2 requestedBy lists
			case "github.com/pkg/errors:v0.8.0":
				assert.Equal(t, [][]string{{"github.com/jfrog/gofrog:v1.1.1", "github.com/jfrog/dependency"}, {"github.com/jfrog/dependency"}}, dep.RequestedBy)

				// uppercase encoded module
			case "github.com/!burnt!sushi/toml:v0.4.2-0.20211125115023-7d0236fe7476":
				assert.Equal(t, [][]string{{"github.com/jfrog/dependency"}}, dep.RequestedBy)
			default:
				assert.Fail(t, "Unexpected dependency "+dep.Id)
			}
		}
	}
}
