package build

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDownload mimics the download function of the CLI: it writes the jar, or reports a Curation block.
func fakeDownload(blocked map[string]bool, requested *[]string) func(downloadTo, downloadFrom string) error {
	return func(downloadTo, downloadFrom string) error {
		// downloadFrom is '<remote path>/<version>/<file name>'.
		v := path.Base(path.Dir(downloadFrom))
		*requested = append(*requested, v)
		if blocked[v] {
			return fmt.Errorf("server response: 403 %s: %w", v, utils.ErrBlockedByCuration)
		}
		return os.WriteFile(downloadTo, []byte("jar"), 0644)
	}
}

func listVersions(t *testing.T, expectedMajorLine string, versions ...string) utils.ExtractorVersionsFunc {
	return func(majorLine string) (utils.ExtractorVersions, error) {
		assert.Equal(t, expectedMajorLine, majorLine)
		return utils.ExtractorVersions{Versions: versions}, nil
	}
}

func TestMavenExtractorVersionSelection(t *testing.T) {
	baseDir := t.TempDir()
	var requested []string
	module := &MavenModule{
		containingBuild: &Build{logger: &utils.NullLog{}},
		extractorDetails: &extractorDetails{
			localPath:             filepath.Join(baseDir, MavenExtractorDependencyVersion),
			downloadExtractorFunc: fakeDownload(map[string]bool{"2.99.2": true, "2.99.1": true}, &requested),
		},
	}
	module.SetExtractorVersionsFunc(baseDir, listVersions(t, "2", "2.99.2", "2.99.1", "2.99.0"))

	require.NoError(t, module.prepareExtractor())

	assert.Equal(t, []string{"2.99.2", "2.99.1", "2.99.0"}, requested)
	assert.Equal(t, filepath.Join(baseDir, "2.99.0"), module.extractorDetails.localPath)
	assert.FileExists(t, filepath.Join(baseDir, "2.99.0", fmt.Sprintf(MavenExtractorFileName, "2.99.0")))
	assert.FileExists(t, filepath.Join(baseDir, "2.99.0", classworldsConfFileName))
	assert.NoFileExists(t, filepath.Join(baseDir, "2.99.2", fmt.Sprintf(MavenExtractorFileName, "2.99.2")))

	// The chosen jar is on disk now: a second run downloads nothing.
	requested = nil
	require.NoError(t, module.prepareExtractor())
	assert.Equal(t, []string{"2.99.2", "2.99.1"}, requested, "blocked versions are checked again, the allowed one is not downloaded")
}

// A previous run may have stopped after the jar was written and before classworlds.conf. The jar is then taken from the local cache.
func TestMavenLocalCacheGetsClassworldsConfig(t *testing.T) {
	baseDir := t.TempDir()
	cached := filepath.Join(baseDir, "2.99.0")
	require.NoError(t, os.MkdirAll(cached, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(cached, fmt.Sprintf(MavenExtractorFileName, "2.99.0")), []byte("jar"), 0644))
	module := &MavenModule{
		containingBuild: &Build{logger: &utils.NullLog{}},
		extractorDetails: &extractorDetails{
			localPath:             filepath.Join(baseDir, MavenExtractorDependencyVersion),
			downloadExtractorFunc: func(string, string) error { return fmt.Errorf("offline") },
		},
	}
	// The lookup fails and the default version cannot be downloaded: the cached jar is used.
	module.SetExtractorVersionsFunc(baseDir, func(string) (utils.ExtractorVersions, error) {
		return utils.ExtractorVersions{}, fmt.Errorf("offline")
	})
	require.NoError(t, module.prepareExtractor())

	assert.Equal(t, cached, module.extractorDetails.localPath)
	assert.FileExists(t, filepath.Join(cached, classworldsConfFileName))
}

func TestMavenExtractorWithoutVersionsFunc(t *testing.T) {
	baseDir := t.TempDir()
	var requested []string
	localPath := filepath.Join(baseDir, MavenExtractorDependencyVersion)
	module := &MavenModule{
		containingBuild:  &Build{logger: &utils.NullLog{}},
		extractorDetails: &extractorDetails{localPath: localPath, downloadExtractorFunc: fakeDownload(nil, &requested)},
	}

	require.NoError(t, module.prepareExtractor())

	assert.Equal(t, []string{MavenExtractorDependencyVersion}, requested)
	assert.Equal(t, localPath, module.extractorDetails.localPath)
}

func TestGradleExtractorVersionSelection(t *testing.T) {
	baseDir := t.TempDir()
	var requested []string
	module := &GradleModule{
		containingBuild: &Build{logger: &utils.NullLog{}},
		gradleExtractorDetails: &gradleExtractorDetails{
			localPath:             baseDir,
			downloadExtractorFunc: fakeDownload(map[string]bool{"5.9.9": true}, &requested),
		},
	}
	// The extractor of Gradle >= 6.8.1 is in the 5.x line.
	module.SetExtractorVersionsFunc(listVersions(t, "5", "5.9.9", "5.9.8"))

	chosen, err := module.downloadExtractorVersion(gradleExtractor5DependencyVersion)

	require.NoError(t, err)
	assert.Equal(t, "5.9.8", chosen)
	assert.Equal(t, []string{"5.9.9", "5.9.8"}, requested)
	assert.FileExists(t, filepath.Join(baseDir, "5.9.8", fmt.Sprintf(gradleExtractorFileName, "5.9.8")))
}

func TestGradleExtractorWithoutVersionsFunc(t *testing.T) {
	baseDir := t.TempDir()
	var requested []string
	module := &GradleModule{
		containingBuild:        &Build{logger: &utils.NullLog{}},
		gradleExtractorDetails: &gradleExtractorDetails{localPath: baseDir, downloadExtractorFunc: fakeDownload(nil, &requested)},
	}

	chosen, err := module.downloadExtractorVersion(gradleExtractor4DependencyVersion)

	require.NoError(t, err)
	assert.Equal(t, gradleExtractor4DependencyVersion, chosen)
	assert.Equal(t, []string{gradleExtractor4DependencyVersion}, requested)
}

// The init script must point at the jar of the version that was chosen, not at the one of the fixed version.
func TestGradleDownloadExtractorUsesChosenVersion(t *testing.T) {
	gradleExe, err := GetGradleExecPath(true)
	require.NoError(t, err)
	testCases := []struct {
		name         string
		project      string
		majorLine    string
		versions     []string
		blocked      map[string]bool
		expectedUsed string
		fixedVersion string
	}{
		{"extractor 5", "gradle-7.0", "5", []string{"5.9.9", "5.9.8"}, map[string]bool{"5.9.9": true}, "5.9.8", gradleExtractor5DependencyVersion},
		{"extractor 4", "gradle-6.8", "4", []string{"4.99.9", "4.99.8"}, map[string]bool{"4.99.9": true}, "4.99.8", gradleExtractor4DependencyVersion},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			baseDir := t.TempDir()
			var requested []string
			module := &GradleModule{
				containingBuild: &Build{logger: &utils.NullLog{}},
				gradleExtractorDetails: &gradleExtractorDetails{
					localPath:             baseDir,
					downloadExtractorFunc: fakeDownload(testCase.blocked, &requested),
				},
			}
			module.SetExtractorVersionsFunc(listVersions(t, testCase.majorLine, testCase.versions...))

			require.NoError(t, module.downloadGradleExtractor(filepath.Join("testdata", "gradle", testCase.project, gradleExe)))

			initScript := module.gradleExtractorDetails.initScript
			assert.Equal(t, filepath.Join(baseDir, testCase.expectedUsed), filepath.Dir(initScript))
			content, err := os.ReadFile(initScript)
			require.NoError(t, err)
			assert.Contains(t, string(content), fmt.Sprintf(gradleExtractorFileName, testCase.expectedUsed))
			assert.NotContains(t, string(content), fmt.Sprintf(gradleExtractorFileName, testCase.fixedVersion))
		})
	}
}
