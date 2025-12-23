package dependencies

import (
	"encoding/json"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
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

	expectedDirectDependencies := []string{"dep1"}
	if !reflect.DeepEqual(expectedDirectDependencies, directDependencies) {
		t.Errorf("Expected: \n%s, \nGot: \n%s", expectedDirectDependencies, directDependencies)
	}

	allDependencies, err := extractor.AllDependencies(logger)
	assert.NoError(t, err)
	expectedAllDependencies := []string{"dep1", "dep2"}
	for _, v := range expectedAllDependencies {
		if _, ok := allDependencies[v]; !ok {
			t.Error("Expecting", v, "dependency")
		}
	}

	childrenMap, err := extractor.ChildrenMap()
	assert.NoError(t, err)
	assert.Len(t, childrenMap["dep1"], 0)
	assert.Len(t, childrenMap["dep2"], 1)
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
	// Test that direct dependencies are returned in sorted order
	content := []byte(`{
  "version": 3,
  "targets": {},
  "project": {
    "restore": {"packagesPath": "unused"},
    "frameworks": {
      "net8.0": {
        "dependencies": {
          "Zebra": {"target": "Package", "version": "1.0.0"},
          "Alpha": {"target": "Package", "version": "1.0.0"},
          "Middle": {"target": "Package", "version": "1.0.0"}
        }
      }
    }
  }
}`)

	var assetsObj assets
	assert.NoError(t, json.Unmarshal(content, &assetsObj))

	// Run multiple times to verify consistency
	expected := []string{"alpha", "middle", "zebra"}
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
	// Alpha appears in both frameworks (should be deduplicated)
	expected := []string{"alpha", "middle", "zebra"}
	for i := 0; i < 10; i++ {
		result := assetsObj.getChildrenMap()
		assert.Equal(t, expected, result["parent"], "Run %d produced different order", i)
	}
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
