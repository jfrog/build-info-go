package build

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

func TestDownloadExtractorsFromReleases(t *testing.T) {
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath))
		assert.NoError(t, utils.CleanOldDirs())
	}()

	// Download JAR
	err = downloadGradleDependencies(tempDirPath, nil, &utils.NullLog{})
	assert.NoError(t, err)

	// Make sure the Gradle build-info extractor JAR exist
	expectedJarPath := filepath.Join(tempDirPath, fmt.Sprintf(GradleExtractorFileName, GradleExtractorDependencyVersion))
	assert.FileExists(t, expectedJarPath)
}
