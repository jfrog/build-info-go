package utils

import (
	"io/ioutil"
	"path/filepath"
	"reflect"
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
	err = parseDependencies([]byte(dependenciesJsonList), "myScope", []string{"root"}, &dependencies, nullLog)
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
