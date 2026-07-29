package dependencies

import (
	"encoding/json"
	deptree "github.com/jfrog/build-info-go/build/utils/dotnet/dependenciestree"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"reflect"
	"testing"
)

var logger = utils.NewDefaultLogger(utils.INFO)

func TestJson(t *testing.T) {
	content := []byte(`{
  "version": 3,
  "targets": {
    "t1": {
      "dep1/1.0.1": {},
      "dep2/1.0.2": {
        "dependencies": {
          "dep1": "1.0.1"
        }
      }
    }
  },
  "libraries": {
    "dep1/1.0.1": {
      "type": "project",
      "path": "dep1/path/1.0.1",
      "files": [
        "file1",
        "file2"
      ]
    },
    "dep2/1.0.2": {
      "path": "dep2/path/1.0.2",
      "files": [
        "file1",
        "file2"
      ]
    }
  },
  "project": {
    "version": "1.0.0",
    "restore": {
      "packagesPath": "path/to/packages"
    },
    "frameworks": {
      "net461": {
        "dependencies": {
          "dep1": {
            "target": "Package",
            "version": "[1.0.1, )"
          }
        }
      }
    }
  }
}`)

	var assetsObj assets
	assert.NoError(t, json.Unmarshal(content, &assetsObj))

	expected := assets{
		Version: 3,
		Targets: map[string]map[string]targetDependency{"t1": {
			"dep1/1.0.1": targetDependency{},
			"dep2/1.0.2": targetDependency{Dependencies: map[string]string{"dep1": "1.0.1"}},
		}},
		Libraries: map[string]library{
			"dep1/1.0.1": {Type: "project", Path: "dep1/path/1.0.1", Files: []string{"file1", "file2"}},
			"dep2/1.0.2": {Path: "dep2/path/1.0.2", Files: []string{"file1", "file2"}},
		},
		Project: project{Version: "1.0.0", Restore: restore{PackagesPath: "path/to/packages"},
			Frameworks: map[string]framework{"net461": {
				Dependencies: map[string]dependency{"dep1": {Target: "Package", Version: "[1.0.1, )"}}}}},
	}

	if !reflect.DeepEqual(expected, assetsObj) {
		t.Errorf("Expected: \n%v, \nGot: \n%v", expected, assetsObj)
	}
}

func TestNewAssetsExtractor(t *testing.T) {
	assets := assetsExtractor{}
	extractor, err := assets.new(filepath.Join("testdata", "assetsproject", "obj", "project.assets.json"), logger)
	assert.NoError(t, err)

	directDependencies, err := extractor.DirectDependencies()
	assert.NoError(t, err)

	// Keys are now name:version to preserve multi-TFM entries
	expectedDirectDependencies := []string{"dep1:1.0.1"}
	if !reflect.DeepEqual(expectedDirectDependencies, directDependencies) {
		t.Errorf("Expected: \n%s, \nGot: \n%s", expectedDirectDependencies, directDependencies)
	}

	allDependencies, err := extractor.AllDependencies(logger)
	assert.NoError(t, err)
	expectedAllDependencies := []string{"dep1:1.0.1", "dep2:1.0.2"}
	for _, v := range expectedAllDependencies {
		if _, ok := allDependencies[v]; !ok {
			t.Error("Expecting", v, "dependency")
		}
	}

	childrenMap, err := extractor.ChildrenMap()
	assert.NoError(t, err)
	assert.Len(t, childrenMap["dep1:1.0.1"], 0)
	assert.Len(t, childrenMap["dep2:1.0.2"], 1)
	assert.Equal(t, "dep1:1.0.1", childrenMap["dep2:1.0.2"][0])
}

func TestGetDependencyIdForBuildInfo(t *testing.T) {
	args := []string{
		"dep1/1.0.1",
		"dep2.another.hierarchy/1.0.2",
		"dep3:with;special?chars@test/1.0.3",
	}

	expected := []string{
		"dep1:1.0.1",
		"dep2.another.hierarchy:1.0.2",
		"dep3:with;special?chars@test:1.0.3",
	}

	for index, test := range args {
		actualId := getDependencyIdForBuildInfo(test)
		assert.Equal(t, expected[index], actualId)
	}
}

func TestGetDirectDependenciesDeterministic(t *testing.T) {
	// Test that direct dependencies are returned in sorted name:version order
	content := []byte(`{
  "version": 3,
  "targets": {},
  "libraries": {
    "Zebra/1.0.0":  {"path": "", "files": []},
    "Alpha/1.0.0":  {"path": "", "files": []},
    "Middle/1.0.0": {"path": "", "files": []}
  },
  "project": {
    "restore": {"packagesPath": "unused"},
    "frameworks": {
      "net8.0": {
        "dependencies": {
          "Zebra":  {"target": "Package", "version": "1.0.0"},
          "Alpha":  {"target": "Package", "version": "1.0.0"},
          "Middle": {"target": "Package", "version": "1.0.0"}
        }
      }
    }
  }
}`)

	var assetsObj assets
	assert.NoError(t, json.Unmarshal(content, &assetsObj))

	// Run multiple times to verify consistency; keys are now name:version
	expected := []string{"alpha:1.0.0", "middle:1.0.0", "zebra:1.0.0"}
	for i := 0; i < 10; i++ {
		result := assetsObj.getDirectDependencies()
		assert.Equal(t, expected, result, "Run %d produced different order", i)
	}
}

func TestGetChildrenMapDeterministic(t *testing.T) {
	// Test that children map returns sorted children across multiple target frameworks
	content := []byte(`{
  "version": 3,
  "targets": {
    ".NETCoreApp,Version=v8.0": {
      "Parent/1.0.0": {
        "dependencies": {
          "Zebra": "1.0.0",
          "Alpha": "1.0.0"
        }
      }
    },
    ".NETCoreApp,Version=v7.0": {
      "Parent/1.0.0": {
        "dependencies": {
          "Middle": "1.0.0",
          "Alpha": "1.0.0"
        }
      }
    }
  },
  "project": {
    "restore": {"packagesPath": "unused"},
    "frameworks": {}
  }
}`)

	var assetsObj assets
	assert.NoError(t, json.Unmarshal(content, &assetsObj))

	// Run multiple times to verify consistency
	// Alpha appears in both TFMs (should be deduplicated); keys are now name:version
	expected := []string{"alpha:1.0.0", "middle:1.0.0", "zebra:1.0.0"}
	for i := 0; i < 10; i++ {
		result := assetsObj.getChildrenMap()
		assert.Equal(t, expected, result["parent:1.0.0"], "Run %d produced different order", i)
	}
}

func TestMultiTFMVersionPreservation(t *testing.T) {
	// Regression test: for a multi-TFM project where the same package resolves to
	// different versions per TFM, all (name, version) pairs must be preserved in the
	// build-info — none dropped due to map-key collisions.
	extractor, err := (&assetsExtractor{}).new(
		filepath.Join("testdata", "multitfm", "obj", "project.assets.json"), logger)
	assert.NoError(t, err)

	allDeps, err := extractor.AllDependencies(logger)
	assert.NoError(t, err)
	// 2 packages × 2 TFM versions = 4 entries; previously only 2 survived (nondeterministically)
	assert.Len(t, allDeps, 4, "all per-TFM versions must be present, none dropped")
	assert.Contains(t, allDeps, "pkga:1.0.0", "net8 version of pkgA must be present")
	assert.Contains(t, allDeps, "pkga:2.0.0", "net10 version of pkgA must be present")
	assert.Contains(t, allDeps, "pkgb:1.0.0", "net8 version of pkgB must be present")
	assert.Contains(t, allDeps, "pkgb:2.0.0", "net10 version of pkgB must be present")

	directDeps, err := extractor.DirectDependencies()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"pkga:1.0.0", "pkga:2.0.0"}, directDeps)

	childrenMap, err := extractor.ChildrenMap()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"pkgb:1.0.0"}, childrenMap["pkga:1.0.0"])
	assert.ElementsMatch(t, []string{"pkgb:2.0.0"}, childrenMap["pkga:2.0.0"])
}

func TestSetToSortedSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]struct{}
		expected []string
	}{
		{
			name:     "empty map",
			input:    map[string]struct{}{},
			expected: []string{},
		},
		{
			name:     "single element",
			input:    map[string]struct{}{"alpha": {}},
			expected: []string{"alpha"},
		},
		{
			name: "multiple elements sorted",
			input: map[string]struct{}{
				"zebra":  {},
				"alpha":  {},
				"middle": {},
			},
			expected: []string{"alpha", "middle", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := setToSortedSlice(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMultiTFMDependencyTree(t *testing.T) {
	// End-to-end regression: verifies that the three extractor maps connect
	// correctly through CreateDependencyTree — previously, name-only map keys
	// caused pkgB to be silently dropped from pkgA's children.
	extractor, err := (&assetsExtractor{}).new(
		filepath.Join("testdata", "multitfm", "obj", "project.assets.json"), logger)
	assert.NoError(t, err)

	tree, err := CreateDependencyTree(extractor, logger)
	assert.NoError(t, err)

	// 2 root nodes: pkgA per TFM version
	assert.Len(t, tree, 2, "expected one root node per TFM version of pkgA")

	roots := map[string][]*deptree.DependenciesTree{}
	for _, node := range tree {
		roots[node.Id] = node.DirectDependencies
	}

	// Each root must carry exactly its TFM-matching child
	assert.Contains(t, roots, "pkga:1.0.0")
	assert.Contains(t, roots, "pkga:2.0.0")
	require.Len(t, roots["pkga:1.0.0"], 1, "pkgA:1.0.0 must have exactly one child")
	require.Len(t, roots["pkga:2.0.0"], 1, "pkgA:2.0.0 must have exactly one child")
	assert.Equal(t, "pkgb:1.0.0", roots["pkga:1.0.0"][0].Id)
	assert.Equal(t, "pkgb:2.0.0", roots["pkga:2.0.0"][0].Id)
}

func TestGetChildrenMapMixedCaseVersion(t *testing.T) {                                                                                                
	// Version labels in targets[...].dependencies come from consuming packages'                                                                     
	// .nuspec and may use non-normalized casing (e.g. "1.0.0-Beta").
	// getAllDependencies lowercases the full name:version, so getChildrenMap                                                                        
	// must do the same or populateRequestedBy lookups silently miss.                      
	content := []byte(`{                                                                                                                             
    "version": 3,                                                                                                                                        
    "targets": {                                               
      ".NETCoreApp,Version=v8.0": {                                                                                                                      
        "Parent/1.0.0": {                                                                      
          "dependencies": {                                    
            "Child": "1.0.0-Beta"              
          }                                                                                                                                              
        },
        "Child/1.0.0-Beta": {}                                                                                                                           
      }                                                                                        
    },                                                         
    "project": {                               
      "restore": {"packagesPath": "unused"},
      "frameworks": {}
    }                                                                                                                                                    
  }`)
	var assetsObj assets                                                                   
	assert.NoError(t, json.Unmarshal(content, &assetsObj)) 
	result := assetsObj.getChildrenMap()
	assert.Equal(t, []string{"child:1.0.0-beta"}, result["parent:1.0.0"])
}

// TestGetChildrenMapBracketRangeVersion guards the RTECO-1265 regression: when
// a parent .nuspec declares a child with a constraint expression (e.g. bracket
// range "[1.12.9, )") instead of a plain version, getChildrenMap must still
// produce a child key that matches the resolved library entry (e.g.
// "popper.js:1.12.9"). Otherwise populateRequestedBy in solution.BuildInfo
// silently misses and the transitive is dropped from the published build-info.
func TestGetChildrenMapBracketRangeVersion(t *testing.T) {
	content := []byte(`{
  "version": 3,
  "targets": {
    ".NETFramework,Version=v4.5": {
      "bootstrap/4.0.0": {
        "dependencies": {
          "jQuery": "3.0.0",
          "popper.js": "[1.12.9, 2.0.0)"
        }
      },
      "jQuery/3.0.0": {},
      "popper.js/1.12.9": {}
    }
  },
  "project": {
    "restore": {"packagesPath": "unused"},
    "frameworks": {}
  }
}`)
	var assetsObj assets
	assert.NoError(t, json.Unmarshal(content, &assetsObj))
	result := assetsObj.getChildrenMap()
	// Both children must resolve to the library entry's version, NOT the declared constraint string.
	assert.ElementsMatch(t, []string{"jquery:3.0.0", "popper.js:1.12.9"}, result["bootstrap:4.0.0"])
}