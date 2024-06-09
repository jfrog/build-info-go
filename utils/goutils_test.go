package utils

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListToMap(t *testing.T) {
	content := `github.com/you/hello
github.com/dsnet/compress:v0.0.0-20171208185109-cc9eb1d7ad76
github.com/golang/snappy:v0.0.0-20180518054509-2e65f85255db
github.com/mholt/archiver:v2.1.0+incompatible
github.com/nwaples/rardecode:v0.0.0-20171029023500-e06696f847ae
github.com/pierrec/lz4:v2.0.5+incompatible
github.com/ulikunitz/xz:v0.5.4
rsc.io/quote:v1.5.2
rsc.io/sampler:v1.3.0
	`

	actual := listToMap(content)
	expected := map[string]bool{
		"github.com/dsnet/compress:v0.0.0-20171208185109-cc9eb1d7ad76":    true,
		"github.com/golang/snappy:v0.0.0-20180518054509-2e65f85255db":     true,
		"github.com/mholt/archiver:v2.1.0+incompatible":                   true,
		"github.com/nwaples/rardecode:v0.0.0-20171029023500-e06696f847ae": true,
		"github.com/pierrec/lz4:v2.0.5+incompatible":                      true,
		"github.com/ulikunitz/xz:v0.5.4":                                  true,
		"rsc.io/quote:v1.5.2":                                             true,
		"rsc.io/sampler:v1.3.0":                                           true,
	}

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Expecting: \n%v \nGot: \n%v", expected, actual)
	}
}

func TestGetProjectRoot(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, os.Chdir(wd))
	}()

	// CD into a directory with a go.mod file.
	projectRoot := filepath.Join("testdata", "project")
	assert.NoError(t, os.Chdir(projectRoot))

	// Make projectRoot an absolute path.
	projectRoot, err = os.Getwd()
	assert.NoError(t, err)

	// Get the project root.
	root, err := GetProjectRoot()
	assert.NoError(t, err)
	assert.Equal(t, projectRoot, root)

	// CD back to the original directory.
	assert.NoError(t, os.Chdir(wd))

	// CD into a subdirectory in the same project, and expect to get the same project root.
	projectSubDirectory := filepath.Join("testdata", "project", "dir")
	assert.NoError(t, os.Chdir(projectSubDirectory))
	root, err = GetProjectRoot()
	assert.NoError(t, err)
	assert.Equal(t, projectRoot, root)

	// CD back to the original directory.
	if !assert.NoError(t, os.Chdir(wd)) {
		return
	}

	// Now CD into a directory outside the project, and expect to get a different project root.
	noProjectRoot := filepath.Join("testdata", "noproject")
	assert.NoError(t, os.Chdir(noProjectRoot))
	root, err = GetProjectRoot()
	assert.NoError(t, err)
	assert.NotEqual(t, projectRoot, root)
}

func TestGetDependenciesList(t *testing.T) {
	testGetDependenciesList(t, "testGoList", nil)
}

func TestGetDependenciesListWithIgnoreErrors(t *testing.T) {
	// In some cases, we see that running go list on some Go packages may fail.
	// We should allow ignoring the errors in such cases and build the Go dependency tree, even if partial.
	testGetDependenciesList(t, "testBadGoList", nil)
	// In some cases we would like to exit after we receive an error. This can be done with custom error handle func.
	// This test handleErrorFunc return an error
	testGetDependenciesList(t, "testBadGoList", func(err error) (bool, error) {
		if err != nil {
			return true, err
		}
		return false, nil
	})
}

func testGetDependenciesList(t *testing.T, testDir string, errorFunc HandleErrorFunc) {
	log := NewDefaultLogger(ERROR)
	goModPath := filepath.Join("testdata", "mods", testDir)
	err := os.Rename(filepath.Join(goModPath, "go.mod.txt"), filepath.Join(goModPath, "go.mod"))
	assert.NoError(t, err)
	defer func() {
		err = os.Rename(filepath.Join(goModPath, "go.mod"), filepath.Join(goModPath, "go.mod.txt"))
		assert.NoError(t, err)
	}()
	err = os.Rename(filepath.Join(goModPath, "go.sum.txt"), filepath.Join(goModPath, "go.sum"))
	assert.NoError(t, err)
	defer func() {
		err = os.Rename(filepath.Join(goModPath, "go.sum"), filepath.Join(goModPath, "go.sum.txt"))
		assert.NoError(t, err)
	}()
	originSumFileContent, err := getGoSum(goModPath, log)
	err = os.Rename(filepath.Join(goModPath, "test.go.txt"), filepath.Join(goModPath, "test.go"))
	assert.NoError(t, err)
	defer func() {
		err = os.Rename(filepath.Join(goModPath, "test.go"), filepath.Join(goModPath, "test.go.txt"))
		assert.NoError(t, err)
	}()
	actual, err := GetDependenciesList(goModPath, log, errorFunc)
	if errorFunc != nil {
		assert.Error(t, err)
		return
	}
	assert.NoError(t, err)

	// Since Go 1.16 'go list' command won't automatically update go.mod and go.sum.
	// Check that we roll back changes properly.
	newSumFileContent, err := getGoSum(goModPath, log)
	assert.Equal(t, newSumFileContent, originSumFileContent, "go.sum has been modified and didn't rollback properly")

	expected := map[string]bool{
		"golang.org/x/text:v0.3.3": true,
		"rsc.io/quote:v1.5.2":      true,
		"rsc.io/sampler:v1.3.0":    true,
		testDir + ":":              true,
	}
	assert.Equal(t, expected, actual)
}

func TestParseGoPathWindows(t *testing.T) {
	log := NewDefaultLogger(DEBUG)
	if !IsWindows() {
		log.Debug("Skipping the test since not running on Windows OS")
		return
	}
	tests := []struct {
		name     string
		goPath   string
		expected string
	}{
		{"One go path", "C:\\Users\\JFrog\\go", "C:\\Users\\JFrog\\go"},
		{"Multiple go paths", "C:\\Users\\JFrog\\go;C:\\Users\\JFrog\\go2;C:\\Users\\JFrog\\go3", "C:\\Users\\JFrog\\go"},
		{"Empty path", "", ""},
	}

	runGoPathTests(tests, t)
}

func TestParseGoPathUnix(t *testing.T) {
	if IsWindows() {
		return
	}
	tests := []struct {
		name     string
		goPath   string
		expected string
	}{
		{"One go path", "/Users/jfrog/go", "/Users/jfrog/go"},
		{"Multiple go paths", "/Users/jfrog/go:/Users/jfrog/go2:/Users/jfrog/go3", "/Users/jfrog/go"},
		{"Empty path", "", ""},
	}

	runGoPathTests(tests, t)
}

func runGoPathTests(tests []struct {
	name     string
	goPath   string
	expected string
}, t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := parseGoPath(test.goPath)
			if !strings.EqualFold(actual, test.expected) {
				t.Errorf("Test name: %s: Expected: %v, Got: %v", test.name, test.expected, actual)
			}
		})
	}
}
