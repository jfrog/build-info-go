package utils

import (
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetYarnDependencyKeyFromLocator(t *testing.T) {
	testCases := []struct {
		yarnDepLocator string
		expectedDepKey string
	}{
		{"camelcase@npm:6.2.0", "camelcase@npm:6.2.0"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight@npm:7.14.0"},
		{"fsevents@patch:fsevents@npm%3A2.3.2#builtin<compat/fsevents>::version=2.3.2&hash=11e9ea", "fsevents@patch:fsevents@npm%3A2.3.2#builtin<compat/fsevents>::version=2.3.2&hash=11e9ea"},
		{"follow-redirects@virtual:c192f6b3b32cd5d11a443145a3883a70c04cbd7c813b53085dbaf50263735f1162f10fdbddd53c24e162ec3bc#npm:1.14.1", "follow-redirects@npm:1.14.1"},
	}

	for _, testCase := range testCases {
		assert.Equal(t, testCase.expectedDepKey, GetYarnDependencyKeyFromLocator(testCase.yarnDepLocator))
	}
}

func TestGetYarnDependencies(t *testing.T) {
	// Copy the project directory to a temporary directory
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}()
	testDataSource := filepath.Join("..", "testdata", "yarn")
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)
	projectSrcPath := filepath.Join(testDataTarget, "project")
	dependenciesMap, root, err := GetYarnDependencies(executablePath, projectSrcPath, &PackageInfo{Name: "build-info-go-tests", Version: "v1.0.0"}, &utils.NullLog{})
	assert.NoError(t, err)
	assert.NotNil(t, root)
	assert.True(t, strings.HasPrefix(root.Value, "build-info-go-tests@"))
	assert.Equal(t, "v1.0.0", root.Details.Version)
	assert.Len(t, dependenciesMap, 3)
}

/*
TESTS TO ADD:
1) conversion from v1 unmarshall to v2 struct
2) correct build of v1 root
*/

func TestBuildYarnV1DependencyMap(t *testing.T) {

}
