package tests

import (
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"testing"
)

func validateModule(t *testing.T, module buildinfo.Module, expectedDependencies, expectedArtifacts int, moduleName string, moduleType buildinfo.ModuleType) {
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
