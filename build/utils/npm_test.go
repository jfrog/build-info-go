package utils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

func TestReadPackageInfoFromPackageJson(t *testing.T) {
	logger := utils.NewDefaultLogger(utils.DEBUG)
	npmVersion, _, err := GetNpmVersionAndExecPath(logger)
	if err != nil {
		assert.NoError(t, err)
		return
	}

	tests := []struct {
		json string
		pi   *PackageInfo
	}{
		{`{ "name": "jfrog-cli-tests", "version": "1.0.0", "description": "test package"}`,
			&PackageInfo{Name: "jfrog-cli-tests", Version: "1.0.0", Scope: ""}},
		{`{ "name": "@jfrog/jfrog-cli-tests", "version": "1.0.0", "description": "test package"}`,
			&PackageInfo{Name: "jfrog-cli-tests", Version: "1.0.0", Scope: "@jfrog"}},
	}
	for _, test := range tests {
		t.Run(test.json, func(t *testing.T) {
			packInfo, err := ReadPackageInfo([]byte(test.json), npmVersion)
			if err != nil {
				t.Error("No error was expected in this test", err)
			}

			equals := reflect.DeepEqual(test.pi, packInfo)
			if !equals {
				t.Error("expected:", test.pi, "got:", packInfo)
			}
		})
	}
}

func TestGetDeployPath(t *testing.T) {
	tests := []struct {
		expectedPath string
		pi           *PackageInfo
	}{
		{`jfrog-cli-tests/-/jfrog-cli-tests-1.0.0.tgz`, &PackageInfo{Name: "jfrog-cli-tests", Version: "1.0.0", Scope: ""}},
		{`@jfrog/jfrog-cli-tests/-/jfrog-cli-tests-1.0.0.tgz`, &PackageInfo{Name: "jfrog-cli-tests", Version: "1.0.0", Scope: "@jfrog"}},
	}
	for _, test := range tests {
		t.Run(test.expectedPath, func(t *testing.T) {
			actualPath := test.pi.GetDeployPath()
			if actualPath != test.expectedPath {
				t.Error("expected:", test.expectedPath, "got:", actualPath)
			}
		})
	}
}

func TestParseDependencies(t *testing.T) {
	dependenciesJsonList, err := ioutil.ReadFile(filepath.Join("..", "testdata", "dependenciesList.json"))
	if err != nil {
		t.Error(err)
	}

	expectedDependenciesList := []struct {
		Key        string
		pathToRoot [][]string
	}{
		{"underscore:1.4.4", [][]string{{"binary-search-tree:0.2.4", "nedb:1.0.2", "root"}}},
		{"@jfrog/npm_scoped:1.0.0", [][]string{{"root"}}},
		{"xml:1.0.1", [][]string{{"root"}}},
		{"xpm:0.1.1", [][]string{{"@jfrog/npm_scoped:1.0.0", "root"}}},
		{"binary-search-tree:0.2.4", [][]string{{"nedb:1.0.2", "root"}}},
		{"nedb:1.0.2", [][]string{{"root"}}},
		{"@ilg/es6-promisifier:0.1.9", [][]string{{"@ilg/cli-start-options:0.1.19", "xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
		{"wscript-avoider:3.0.2", [][]string{{"@ilg/cli-start-options:0.1.19", "xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
		{"yaml:0.2.3", [][]string{{"root"}}},
		{"@ilg/cli-start-options:0.1.19", [][]string{{"xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
		{"async:0.2.10", [][]string{{"nedb:1.0.2", "root"}}},
		{"find:0.2.7", [][]string{{"root"}}},
		{"jquery:3.2.0", [][]string{{"root"}}},
		{"nub:1.0.0", [][]string{{"find:0.2.7", "root"}, {"root"}}},
		{"shopify-liquid:1.d7.9", [][]string{{"xpm:0.1.1", "@jfrog/npm_scoped:1.0.0", "root"}}},
	}
	dependencies := make(map[string]*entities.Dependency)
	nullLog := &utils.NullLog{}
	err = parseDependencies([]byte(dependenciesJsonList), "myScope", []string{"root"}, dependencies, nullLog)
	if err != nil {
		t.Error(err)
	}
	if len(expectedDependenciesList) != len(dependencies) {
		t.Error("The expected dependencies list length is", len(expectedDependenciesList), "and should be:\n", expectedDependenciesList,
			"\nthe actual dependencies list length is", len(dependencies), "and the list is:\n", dependencies)
		t.Error("The expected dependencies list length is", len(expectedDependenciesList), "and should be:\n", expectedDependenciesList,
			"\nthe actual dependencies list length is", len(dependencies), "and the list is:\n", dependencies)
	}
	for _, eDependency := range expectedDependenciesList {
		found := false
		for aDependency, v := range dependencies {
			if aDependency == eDependency.Key && assert.ElementsMatch(t, v.RequestedBy, eDependency.pathToRoot) {
				found = true
				break
			}
		}
		if !found {
			t.Error("The expected dependency:", eDependency, "is missing from the actual dependencies list:\n", dependencies)
		}
	}
}

func TestCalculateDependencies(t *testing.T) {
	// Create npm project.
	projectPath, err := filepath.Abs(filepath.Join("..", "testdata", "npm", "project"))
	assert.NoError(t, err)
	tmpProjectPath, cleanup := CreateTestProject(t, projectPath)
	defer cleanup()

	// Install dependencies in the npm project.
	_, _, err = RunNpmCmd("npm", tmpProjectPath, Install, nil, &utils.NullLog{})
	assert.NoError(t, err)

	// Calculate dependencies.
	dependenciesList, err := CalculateDependenciesList(All, "npm", tmpProjectPath, "", nil, &utils.NullLog{})
	assert.NoError(t, err)
	assert.NotEmpty(t, dependenciesList)
}

func TestPackageLock(t *testing.T) {
	projectPath, err := filepath.Abs(filepath.Join("..", "testdata", "npm", "project"))
	assert.NoError(t, err)

	packageLock, err := newPackageLock(projectPath)
	assert.NoError(t, err)
	assert.Len(t, packageLock.Dependencies, 3)
	assert.Equal(t, "1.0.1", packageLock.Dependencies["node_modules/xml"].Version)
	assert.Equal(t, "sha1-eLpyAgApxbyHuKgaPPzXS0ovweU=", packageLock.Dependencies["node_modules/xml"].Integrity)
	assert.Equal(t, "9.0.6", packageLock.Dependencies["node_modules/json"].Version)
	assert.Equal(t, "sha1-eXLCpaSKQmeNsnMMfCxO5uTiRYU=", packageLock.Dependencies["node_modules/json"].Integrity)

	assert.Len(t, packageLock.LegacyDependencies, 2)
	assert.Equal(t, "1.0.1", packageLock.LegacyDependencies["xml"].Version)
	assert.Equal(t, "sha1-eLpyAgApxbyHuKgaPPzXS0ovweU=", packageLock.LegacyDependencies["xml"].Integrity)
	assert.Equal(t, "9.0.6", packageLock.LegacyDependencies["json"].Version)
	assert.Equal(t, "sha1-eXLCpaSKQmeNsnMMfCxO5uTiRYU=", packageLock.LegacyDependencies["json"].Integrity)

	integrity := packageLock.getIntegrityMap()
	assert.Equal(t, "sha1-eLpyAgApxbyHuKgaPPzXS0ovweU=", integrity["xml:1.0.1"])
	assert.Equal(t, "sha1-eXLCpaSKQmeNsnMMfCxO5uTiRYU=", integrity["json:9.0.6"])

}

func TestGetDepTarball(t *testing.T) {
	projectPath, err := filepath.Abs(filepath.Join("..", "testdata", "npm", "_cacache"))
	assert.NoError(t, err)

	// Get tarball sha512 hash
	path, err := getTarball(projectPath, "sha512-dWe4nWO/ruEOY7HkUJ5gFt1DCFV9zPRoJr8pV0/ASQermOZjtq8jMjOprC0Kd10GLN+l7xaUPvxzJFWtxGu8Fg==")
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join("testdata", "npm", "_cacache", "content-v2", "sha512", "75", "67", "b89d63bfaee10e63b1e4509e6016dd4308557dccf46826bf29574fc04907ab98e663b6af233233a9ac2d0a775d062cdfa5ef16943efc732455adc46bbc16")))

	// Get tarball sha1 hash
	path, err = getTarball(projectPath, "sha1-Z29us8OZl8LuGsOpJP1hJHSPV40=")
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join("testdata", "npm", "_cacache", "content-v2", "sha1", "67", "6f", "6eb3c39997c2ee1ac3a924fd6124748f578d")))
}

func TestIntegrityToSha(t *testing.T) {
	hashAlgorithm, hash, err := integrityToSha("sha512-dWe4nWO/ruEOY7HkUJ5gFt1DCFV9zPRoJr8pV0/ASQermOZjtq8jMjOprC0Kd10GLN+l7xaUPvxzJFWtxGu8Fg==")
	assert.NoError(t, err)
	assert.Equal(t, "7567b89d63bfaee10e63b1e4509e6016dd4308557dccf46826bf29574fc04907ab98e663b6af233233a9ac2d0a775d062cdfa5ef16943efc732455adc46bbc16", hash)
	assert.Equal(t, "sha512", hashAlgorithm)

	hashAlgorithm, hash, err = integrityToSha("sha1-Z29us8OZl8LuGsOpJP1hJHSPV40=")
	assert.NoError(t, err)
	assert.Equal(t, "676f6eb3c39997c2ee1ac3a924fd6124748f578d", hash)
	assert.Equal(t, "sha1", hashAlgorithm)

}

func TestGetNpmConfigCache(t *testing.T) {
	// Create npm project.
	projectPath, err := filepath.Abs(filepath.Join("..", "testdata", "npm", "project"))
	assert.NoError(t, err)
	tmpProjectPath, cleanup := CreateTestProject(t, projectPath)
	defer cleanup()

	cachePath, err := getNpmConfigCache(tmpProjectPath, "npm", []string{"--cache=abc"}, &utils.NullLog{})
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(cachePath, filepath.Join("abc","_cacache")))

	oldCache := os.Getenv("npm_config_cache")
	if oldCache != "" {
		defer func(){
			assert.NoError(t,os.Setenv("npm_config_cache",oldCache))
		}()
	}
	assert.NoError(t,os.Setenv("npm_config_cache","def"))
	cachePath, err = getNpmConfigCache(tmpProjectPath, "npm", []string{}, &utils.NullLog{})
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(cachePath, filepath.Join("def","_cacache")))
}
