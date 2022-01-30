package utils

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	if err != nil {
		t.Error(err)
	}
	defer os.Chdir(wd)

	// CD into a directory with a go.mod file.
	projectRoot := filepath.Join("testdata", "project")
	err = os.Chdir(projectRoot)
	if err != nil {
		t.Error(err)
	}

	// Make projectRoot an absolute path.
	projectRoot, err = os.Getwd()
	if err != nil {
		t.Error(err)
	}

	// Get the project root.
	root, err := GetProjectRoot()
	if err != nil {
		t.Error(err)
	}
	if root != projectRoot {
		t.Error("Expecting", projectRoot, "got:", root)
	}

	// CD back to the original directory.
	if err := os.Chdir(wd); err != nil {
		t.Error(err)
	}

	// CD into a sub directory in the same project, and expect to get the same project root.
	os.Chdir(wd)
	projectSubDirectory := filepath.Join("testdata", "project", "dir")
	err = os.Chdir(projectSubDirectory)
	if err != nil {
		t.Error(err)
	}
	root, err = GetProjectRoot()
	if err != nil {
		t.Error(err)
	}
	if root != projectRoot {
		t.Error("Expecting", projectRoot, "got:", root)
	}

	// CD back to the original directory.
	if err := os.Chdir(wd); err != nil {
		t.Error(err)
	}

	// Now CD into a directory outside the project, and expect to get a different project root.
	noProjectRoot := filepath.Join("testdata", "noproject")
	err = os.Chdir(noProjectRoot)
	if err != nil {
		t.Error(err)
	}
	root, err = GetProjectRoot()
	if err != nil {
		t.Error(err)
	}
	if root == projectRoot {
		t.Error("Expecting a different value than", root)
	}
}

func TestGetDependenciesList(t *testing.T) {
	testGetDependenciesList(t, "testGoList")
}

func TestGetDependenciesListWithIgnoreErrors(t *testing.T) {
	// In some cases, we see that running go list on some Go packages may fail.
	// We should allow ignoring the errors in such cases and build the Go dependency tree, even if partial.
	testGetDependenciesList(t, "testBadGoList")
}

func testGetDependenciesList(t *testing.T, testDir string) {
	log := NewDefaultLogger(ERROR)
	gomodPath := filepath.Join("testdata", "mods", testDir)
	err := os.Rename(filepath.Join(gomodPath, "go.mod.txt"), filepath.Join(gomodPath, "go.mod"))
	assert.NoError(t, err)
	defer func() {
		err := os.Rename(filepath.Join(gomodPath, "go.mod"), filepath.Join(gomodPath, "go.mod.txt"))
		assert.NoError(t, err)
	}()
	err = os.Rename(filepath.Join(gomodPath, "go.sum.txt"), filepath.Join(gomodPath, "go.sum"))
	assert.NoError(t, err)
	defer func() {
		err = os.Rename(filepath.Join(gomodPath, "go.sum"), filepath.Join(gomodPath, "go.sum.txt"))
		assert.NoError(t, err)
	}()
	originSumFileContent, _, err := getGoSum(gomodPath, log)
	err = os.Rename(filepath.Join(gomodPath, "test.go.txt"), filepath.Join(gomodPath, "test.go"))
	assert.NoError(t, err)
	defer func() {
		err := os.Rename(filepath.Join(gomodPath, "test.go"), filepath.Join(gomodPath, "test.go.txt"))
		assert.NoError(t, err)
	}()
	actual, err := GetDependenciesList(filepath.Join(gomodPath), log)
	if err != nil {
		t.Error(err)
	}

	// Since Go 1.16 'go list' command won't automatically update go.mod and go.sum.
	// Check that we rollback changes properly.
	newSumFileContent, _, err := getGoSum(gomodPath, log)
	if !reflect.DeepEqual(originSumFileContent, newSumFileContent) {
		t.Errorf("go.sum has been modified and didn't rollback properly")
	}

	expected := map[string]bool{
		"golang.org/x/text:v0.3.3": true,
		"rsc.io/quote:v1.5.2":      true,
		"rsc.io/sampler:v1.3.0":    true,
		testDir + ":":              true,
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Expecting: \n%v \nGot: \n%v", expected, actual)
	}
}

func TestParseGoPathWindows(t *testing.T) {
	log := NewDefaultLogger(DEBUG)
	if runtime.GOOS != "windows" {
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
	if runtime.GOOS == "windows" {
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
