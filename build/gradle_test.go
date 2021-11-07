package build

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jfrog/jfrog-client-go/utils/io/fileutils"
	"github.com/stretchr/testify/assert"
)

func TestDownloadExtractorsFromReleases(t *testing.T) {
	tempDirPath, err := fileutils.CreateTempDir()
	assert.NoError(t, err)
	defer fileutils.RemoveTempDir(tempDirPath)

	// Download JAR
	err = downloadGradleDependencies(tempDirPath, nil)
	assert.NoError(t, err)

	// Make sure the Gradle build-info extractor JAR exist
	expectedJarPath := filepath.Join(tempDirPath, fmt.Sprintf(GradleExtractorFileName, MavenExtractorDependencyVersion))
	assert.FileExists(t, expectedJarPath)
}
