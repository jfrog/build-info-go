package build

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

func TestDownloadDependencies(t *testing.T) {
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath))
		assert.NoError(t, utils.CleanOldDirs())
	}()

	// Download JAR and create classworlds.conf
	err = downloadMavenExtractor(tempDirPath, nil, &utils.NullLog{})
	assert.NoError(t, err)

	// Make sure the Maven build-info extractor JAR and the classwords.conf file exist.
	expectedJarPath := filepath.Join(tempDirPath, fmt.Sprintf(MavenExtractorFileName, MavenExtractorDependencyVersion))
	assert.FileExists(t, expectedJarPath)
	expectedClasswordsPath := filepath.Join(tempDirPath, "classworlds.conf")
	assert.FileExists(t, expectedClasswordsPath)
}
