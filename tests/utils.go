package tests

import (
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"testing"
)

func validateModule(t *testing.T, module buildinfo.Module, expectedDependencies, expectedArtifacts int, moduleName string, moduleType buildinfo.ModuleType, depsContainChecksums bool) {
	assert.Equal(t, moduleName, module.Id, "Unexpected module name")
	assert.Len(t, module.Dependencies, expectedDependencies, "Incorrect number of dependencies found in the build-info")
	assert.Len(t, module.Artifacts, expectedArtifacts, "Incorrect number of artifacts found in the build-info")
	if expectedArtifacts > 0 {
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
