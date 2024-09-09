package dependencies

import (
	"encoding/xml"
	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func getAllDependencies(dependencies map[string][]string) map[string]*buildinfo.Dependency {
	allDependencies := map[string]*buildinfo.Dependency{}
	for id := range dependencies {
		allDependencies[id] = &buildinfo.Dependency{Id: id}
	}
	return allDependencies
}

func TestGetRootDependencies(t *testing.T) {
	tests := []struct {
		name         string
		dependencies map[string][]string
		expected     []string
	}{
		{"simple1", map[string][]string{"a": {"b", "c"}, "b": {}, "c": {}}, []string{"a"}},
		{"simple2", map[string][]string{"a": {}, "b": {}, "c": {}}, []string{"a", "b", "c"}},
		{"simple3", map[string][]string{"a": {"b"}, "b": {}, "c": {}}, []string{"a", "c"}},
		{"simple4", map[string][]string{"a": {"b"}, "b": {}, "c": {"d"}, "d": {}}, []string{"a", "c"}},
		{"simple5", map[string][]string{"a": {"c"}, "b": {"c"}, "c": {"d", "e"}, "d": {}, "e": {}}, []string{"a", "b"}},
		{"nonexisting", map[string][]string{"a": {"nonexisting"}}, []string{"a"}},
		{"circular1", map[string][]string{"a": {"b", "c"}, "b": {}, "c": {"a"}}, []string{"a", "c"}},
		{"circular2", map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}}, []string{"a", "b", "c"}},
		{"circular3", map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}, "d": {"a"}}, []string{"a", "b", "c", "d"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := getDirectDependencies(getAllDependencies(test.dependencies), test.dependencies)
			sort.Strings(actual)
			sort.Strings(test.expected)
			if !reflect.DeepEqual(test.expected, actual) {
				t.Errorf("Expected: %s, Got: %s", test.expected, actual)
			}
		})
	}
}

func TestAlternativeVersionsForms(t *testing.T) {
	tests := []struct {
		version  string
		expected []string
	}{
		{"1.0", []string{"1.0.0.0", "1.0.0", "1"}},
		{"1", []string{"1.0.0.0", "1.0.0", "1.0"}},
		{"1.2", []string{"1.2.0.0", "1.2.0"}},
		{"1.22.33", []string{"1.22.33.0"}},
		{"1.22.33.44", []string{}},
		{"1.0.2", []string{"1.0.2.0"}},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			actual := CreateAlternativeVersionForms(test.version)
			sort.Strings(actual)
			sort.Strings(test.expected)
			if len(actual) != len(test.expected) {
				t.Errorf("Expected: %s, Got: %s", test.expected, actual)
			}

			if len(actual) > 0 && len(test.expected) > 0 && !reflect.DeepEqual(test.expected, actual) {
				t.Errorf("Expected: %s, Got: %s", test.expected, actual)
			}
		})
	}
}

func TestLoadPackagesConfig(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="utf-8"?>
<packages>
  <package id="id1" version="1.0.0" targetFramework="net461" />
  <package id="id2" version="2.0.0" targetFramework="net461" />
  <package id="Microsoft.Web.Infrastructure" version="1.0.0.0" targetFramework="net461" />
</packages>`)

	packagesObj := &packagesConfig{}
	err := xml.Unmarshal(xmlContent, packagesObj)
	assert.NoError(t, err)

	expected := &packagesConfig{
		XMLName: xml.Name{Local: "packages"},
		XmlPackages: []xmlPackage{
			{Id: "id1", Version: "1.0.0"},
			{Id: "id2", Version: "2.0.0"},
			{Id: "Microsoft.Web.Infrastructure", Version: "1.0.0.0"},
		},
	}

	if !reflect.DeepEqual(expected, packagesObj) {
		t.Errorf("Expected: %s, Got: %s", expected, packagesObj)
	}
}

func TestLoadNuspec(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="utf-8"?>
<package>
  <metadata>
    <id>ZKWeb.System.Drawing</id>
    <dependencies> 
      <group targetFramework="targetFramework">
        <dependency id="one" version="1.0.0" />
        <dependency id="two" version="2.0.0" />
      </group>
      <dependency id="three" version="3.0.0" />
    </dependencies>
  </metadata>
</package>>`)

	nuspecObj := &nuspec{}
	err := xml.Unmarshal(xmlContent, nuspecObj)
	assert.NoError(t, err)

	expected := &nuspec{
		XMLName: xml.Name{Local: "package"},
		Metadata: metadata{
			Dependencies: xmlDependencies{Groups: []group{{
				TargetFramework: "targetFramework",
				Dependencies: []xmlPackage{{
					Id:      "one",
					Version: "1.0.0",
				}, {
					Id:      "two",
					Version: "2.0.0",
				}}},
			},
				Dependencies: []xmlPackage{{
					Id:      "three",
					Version: "3.0.0",
				}},
			},
		},
	}

	if !reflect.DeepEqual(expected, nuspecObj) {
		t.Errorf("Expected: %s, Got: %s", expected, nuspecObj)
	}
}

func TestExtractDependencies(t *testing.T) {
	var log = utils.NewDefaultLogger(utils.INFO)
	extractor, err := extractDependencies(filepath.Join("testdata", "packagesproject", "localcache"), log)
	assert.NoError(t, err)

	expectedAllDependencies := []string{"id1", "id2", "microsoft.web.infrastructure"}
	allDependencies, err := extractor.AllDependencies(log)
	assert.NoError(t, err)

	for _, v := range expectedAllDependencies {
		if _, ok := allDependencies[v]; !ok {
			t.Error("Expecting", v, "dependency")
		}
	}

	expectedChildrenMap := map[string][]string{"id1": {"id2"}, "id2": {"id1"}, "microsoft.web.infrastructure": {"id1"}}
	childrenMap, err := extractor.ChildrenMap()
	assert.NoError(t, err)

	if !reflect.DeepEqual(expectedChildrenMap, childrenMap) {
		t.Errorf("Expected: %s, Got: %s", expectedChildrenMap, childrenMap)
	}

	expectedDirectDependencies := []string{"microsoft.web.infrastructure", "id1", "id2"}
	directDependencies, err := extractor.DirectDependencies()
	assert.NoError(t, err)

	sort.Strings(directDependencies)
	sort.Strings(expectedDirectDependencies)
	if !reflect.DeepEqual(expectedDirectDependencies, directDependencies) {
		t.Errorf("Expected: %s, Got: %s", expectedDirectDependencies, directDependencies)
	}
}

func TestPackageNotFoundWithoutFailure(t *testing.T) {
	log := utils.NewDefaultLogger(utils.INFO)
	_, err := extractDependencies(filepath.Join("testdata", "packagesproject", "localcachenotexists"), log)
	assert.NoError(t, err)
}

func extractDependencies(globalPackagePath string, log utils.Log) (Extractor, error) {
	extractor := &packagesExtractor{allDependencies: map[string]*buildinfo.Dependency{}, childrenMap: map[string][]string{}}
	packagesConfig, err := extractor.loadPackagesConfig(filepath.Join("testdata", "packagesproject", "packages.config"), log)
	if err != nil {
		return extractor, err
	}
	err = extractor.extract(packagesConfig, globalPackagePath, log)
	if err != nil {
		return extractor, err
	}
	return extractor, nil
}
