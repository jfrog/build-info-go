package conan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConanFlexPack(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T, dir string)
		expectError bool
	}{
		{
			name: "creates instance without loading conanfile (lazy init)",
			setupFunc: func(t *testing.T, dir string) {
				// Empty directory - conanfile will be loaded lazily
			},
			expectError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "conan-test-*")
			require.NoError(t, err)
			defer func() { _ = os.RemoveAll(tempDir) }()
			tt.setupFunc(t, tempDir)
			config := ConanConfig{
				WorkingDirectory: tempDir,
				ConanExecutable:  "echo",
			}
			cf, err := NewConanFlexPack(config)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, cf)
			}
		})
	}
}

func TestFindConanExecutable(t *testing.T) {
	// Test that findConanExecutable returns an error when conan is not found
	// We can't easily test the success case without conan installed
	path, err := findConanExecutable()
	if err != nil {
		// Expected when conan is not installed
		assert.Contains(t, err.Error(), "conan executable not found")
	} else {
		// If conan is installed, path should be non-empty
		assert.NotEmpty(t, path)
	}
}

func TestParseConanReference(t *testing.T) {
	cf := &ConanFlexPack{}
	tests := []struct {
		ref             string
		expectedName    string
		expectedVersion string
	}{
		{
			ref:             "zlib/1.2.13",
			expectedName:    "zlib",
			expectedVersion: "1.2.13",
		},
		{
			ref:             "zlib/1.2.13@myuser/stable",
			expectedName:    "zlib",
			expectedVersion: "1.2.13",
		},
		{
			ref:             "zlib/1.2.13#abc123def456",
			expectedName:    "zlib",
			expectedVersion: "1.2.13",
		},
		{
			ref:             "zlib/1.2.13@myuser/stable#abc123def456:1234567890abcdef",
			expectedName:    "zlib",
			expectedVersion: "1.2.13",
		},
		{
			ref:             "boost/1.80.0@company/release#revision123:packageid456",
			expectedName:    "boost",
			expectedVersion: "1.80.0",
		},
		{
			ref:             "single-name",
			expectedName:    "single-name",
			expectedVersion: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, version := cf.parseConanReference(tt.ref)
			assert.Equal(t, tt.expectedName, name)
			assert.Equal(t, tt.expectedVersion, version)
		})
	}
}

func TestExtractDependenciesFromGraph(t *testing.T) {
	cf := &ConanFlexPack{
		requestedByMap: make(map[string][]string),
		projectName:    "myapp",
		projectVersion: "1.0",
		user:           "user",
		channel:        "channel",
	}
	graphData := &ConanGraphOutput{
		RootRef: "myapp/1.0@user/channel",
		Graph: struct {
			Nodes map[string]ConanGraphNode `json:"nodes"`
		}{
			Nodes: map[string]ConanGraphNode{
				"0": {
					Ref:         "myapp/1.0@user/channel",
					DisplayName: "myapp/1.0",
					Context:     "host",
					Dependencies: map[string]ConanDependencyEdge{
						"1": {Ref: "zlib/1.2.13", Direct: true},
						"2": {Ref: "cmake/3.25.0", Build: true, Direct: true},
					},
				},
				"1": {
					Ref:          "zlib/1.2.13",
					DisplayName:  "zlib/1.2.13",
					Context:      "host",
					Dependencies: map[string]ConanDependencyEdge{},
				},
				"2": {
					Ref:          "cmake/3.25.0",
					DisplayName:  "cmake/3.25.0",
					Context:      "build",
					Dependencies: map[string]ConanDependencyEdge{},
				},
			},
		},
	}
	cf.graphData = graphData
	cf.extractDependenciesFromGraph()
	assert.Len(t, cf.dependencies, 2)
	zlibDep := findDependencyByID(cf.dependencies, "zlib:1.2.13")
	assert.NotNil(t, zlibDep)
	assert.Contains(t, zlibDep.Scopes, "runtime")
	cmakeDep := findDependencyByID(cf.dependencies, "cmake:3.25.0")
	assert.NotNil(t, cmakeDep)
	assert.Contains(t, cmakeDep.Scopes, "build")
}

func TestExtractDependenciesWithTransitiveRequestedBy(t *testing.T) {
	cf := &ConanFlexPack{
		requestedByMap: make(map[string][]string),
		projectName:    "myapp",
		projectVersion: "1.0",
		user:           "_",
		channel:        "_",
	}
	graphData := &ConanGraphOutput{
		RootRef: "myapp/1.0",
		Graph: struct {
			Nodes map[string]ConanGraphNode `json:"nodes"`
		}{
			Nodes: map[string]ConanGraphNode{
				"0": {
					Ref:     "myapp/1.0",
					Context: "host",
					Dependencies: map[string]ConanDependencyEdge{
						"1": {Ref: "libA/1.0", Direct: true},
					},
				},
				"1": {
					Ref:     "libA/1.0",
					Context: "host",
					Dependencies: map[string]ConanDependencyEdge{
						"2": {Ref: "libB/2.0", Direct: true},
					},
				},
				"2": {
					Ref:     "libB/2.0",
					Context: "host",
					Dependencies: map[string]ConanDependencyEdge{
						"3": {Ref: "libC/3.0", Direct: true},
					},
				},
				"3": {
					Ref:          "libC/3.0",
					Context:      "host",
					Dependencies: map[string]ConanDependencyEdge{},
				},
			},
		},
	}
	cf.graphData = graphData
	cf.extractDependenciesFromGraph()
	assert.Len(t, cf.dependencies, 3)
	assert.Contains(t, cf.requestedByMap["libA:1.0"], "myapp:1.0")
	assert.Contains(t, cf.requestedByMap["libB:2.0"], "libA:1.0")
	assert.Contains(t, cf.requestedByMap["libC:3.0"], "libB:2.0")
}

func TestExtractDependenciesWithDiamondDependency(t *testing.T) {
	cf := &ConanFlexPack{
		requestedByMap: make(map[string][]string),
		projectName:    "myapp",
		projectVersion: "1.0",
		user:           "_",
		channel:        "_",
	}
	graphData := &ConanGraphOutput{
		RootRef: "myapp/1.0",
		Graph: struct {
			Nodes map[string]ConanGraphNode `json:"nodes"`
		}{
			Nodes: map[string]ConanGraphNode{
				"0": {
					Ref:     "myapp/1.0",
					Context: "host",
					Dependencies: map[string]ConanDependencyEdge{
						"1": {Ref: "libA/1.0", Direct: true},
						"2": {Ref: "libB/1.0", Direct: true},
					},
				},
				"1": {
					Ref:     "libA/1.0",
					Context: "host",
					Dependencies: map[string]ConanDependencyEdge{
						"3": {Ref: "libC/1.0", Direct: true},
					},
				},
				"2": {
					Ref:     "libB/1.0",
					Context: "host",
					Dependencies: map[string]ConanDependencyEdge{
						"3": {Ref: "libC/1.0", Direct: true},
					},
				},
				"3": {
					Ref:          "libC/1.0",
					Context:      "host",
					Dependencies: map[string]ConanDependencyEdge{},
				},
			},
		},
	}
	cf.graphData = graphData
	cf.extractDependenciesFromGraph()
	assert.Len(t, cf.dependencies, 3)
	assert.Len(t, cf.requestedByMap["libC:1.0"], 2, "libC should be requested by both libA and libB")
	assert.Contains(t, cf.requestedByMap["libC:1.0"], "libA:1.0")
	assert.Contains(t, cf.requestedByMap["libC:1.0"], "libB:1.0")
}

func TestMapConanContextToScopes(t *testing.T) {
	cf := &ConanFlexPack{}
	tests := []struct {
		context       string
		expectedScope string
	}{
		{"host", "runtime"},
		{"build", "build"},
		{"test", "test"},
		{"unknown", "runtime"},
		{"", "runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.context, func(t *testing.T) {
			scopes := cf.mapConanContextToScopes(tt.context)
			assert.Contains(t, scopes, tt.expectedScope)
		})
	}
}

func TestDetermineScopesFromEdge(t *testing.T) {
	cf := &ConanFlexPack{}
	tests := []struct {
		name          string
		edge          ConanDependencyEdge
		context       string
		expectedScope string
	}{
		{
			name:          "build edge overrides context",
			edge:          ConanDependencyEdge{Build: true},
			context:       "host",
			expectedScope: "build",
		},
		{
			name:          "test edge overrides context",
			edge:          ConanDependencyEdge{Test: true},
			context:       "host",
			expectedScope: "test",
		},
		{
			name:          "no edge flags uses context",
			edge:          ConanDependencyEdge{},
			context:       "host",
			expectedScope: "runtime",
		},
		{
			name:          "build context when no edge flags",
			edge:          ConanDependencyEdge{},
			context:       "build",
			expectedScope: "build",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := cf.determineScopesFromEdge(tt.edge, tt.context)
			assert.Contains(t, scopes, tt.expectedScope)
		})
	}
}

func TestParseDependenciesFromLockFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conan-lock-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	lockContent := `{
		"version": "0.5",
		"requires": [
			"zlib/1.2.13",
			"openssl/3.0.5"
		],
		"build_requires": [
			"cmake/3.25.0"
		],
		"python_requires": [
			"pylint/2.17.0"
		],
		"config_requires": []
	}`
	lockPath := filepath.Join(tempDir, "conan.lock")
	err = os.WriteFile(lockPath, []byte(lockContent), 0644)
	require.NoError(t, err)
	cf := &ConanFlexPack{
		config: ConanConfig{
			WorkingDirectory: tempDir,
			ConanExecutable:  "echo",
		},
		dependencies: []entities.Dependency{},
	}
	err = cf.parseDependenciesFromLockFile()
	require.NoError(t, err)
	assert.Len(t, cf.dependencies, 4)
	zlibDep := findDependencyByID(cf.dependencies, "zlib:1.2.13")
	assert.NotNil(t, zlibDep)
	assert.Contains(t, zlibDep.Scopes, "runtime")
	opensslDep := findDependencyByID(cf.dependencies, "openssl:3.0.5")
	assert.NotNil(t, opensslDep)
	assert.Contains(t, opensslDep.Scopes, "runtime")
	cmakeDep := findDependencyByID(cf.dependencies, "cmake:3.25.0")
	assert.NotNil(t, cmakeDep)
	assert.Contains(t, cmakeDep.Scopes, "build")
	pylintDep := findDependencyByID(cf.dependencies, "pylint:2.17.0")
	assert.NotNil(t, pylintDep)
	assert.Contains(t, pylintDep.Scopes, "python")
}

func TestParseDependenciesFromLockFileError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conan-lock-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	cf := &ConanFlexPack{
		config: ConanConfig{
			WorkingDirectory: tempDir,
			ConanExecutable:  "echo",
		},
		dependencies: []entities.Dependency{},
	}
	err = cf.parseDependenciesFromLockFile()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read conan.lock")
}

func TestParseDependenciesFromLockFileInvalidJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conan-lock-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	lockPath := filepath.Join(tempDir, "conan.lock")
	err = os.WriteFile(lockPath, []byte("invalid json"), 0644)
	require.NoError(t, err)
	cf := &ConanFlexPack{
		config: ConanConfig{
			WorkingDirectory: tempDir,
			ConanExecutable:  "echo",
		},
		dependencies: []entities.Dependency{},
	}
	err = cf.parseDependenciesFromLockFile()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse conan.lock")
}

func TestAddRequestedByNoDuplicates(t *testing.T) {
	cf := &ConanFlexPack{
		requestedByMap: make(map[string][]string),
	}
	cf.addRequestedBy("libA:1.0", "root:1.0")
	cf.addRequestedBy("libA:1.0", "root:1.0")
	cf.addRequestedBy("libA:1.0", "root:1.0")
	assert.Len(t, cf.requestedByMap["libA:1.0"], 1)
	cf.addRequestedBy("libA:1.0", "another:2.0")
	assert.Len(t, cf.requestedByMap["libA:1.0"], 2)
}

func TestGetProjectRootId(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		version     string
		user        string
		channel     string
		expected    string
	}{
		{
			name:        "Conan 2.x style (no user/channel)",
			projectName: "myapp",
			version:     "1.0.0",
			user:        "_",
			channel:     "_",
			expected:    "myapp:1.0.0",
		},
		{
			name:        "Conan 1.x style (with user/channel)",
			projectName: "myapp",
			version:     "1.0.0",
			user:        "demo",
			channel:     "stable",
			expected:    "myapp/1.0.0@demo/stable",
		},
		{
			name:        "Empty version (consumer-only recipe)",
			projectName: "myapp",
			version:     "",
			user:        "_",
			channel:     "_",
			expected:    "myapp",
		},
		{
			name:        "Empty project name",
			projectName: "",
			version:     "1.0.0",
			user:        "_",
			channel:     "_",
			expected:    "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := &ConanFlexPack{
				projectName:    tt.projectName,
				projectVersion: tt.version,
				user:           tt.user,
				channel:        tt.channel,
			}
			result := cf.getProjectRootId()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPythonAttribute(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		attr     string
		expected string
	}{
		{
			name:     "double quotes",
			content:  `name = "mylib"`,
			attr:     "name",
			expected: "mylib",
		},
		{
			name:     "single quotes",
			content:  `name = 'mylib'`,
			attr:     "name",
			expected: "mylib",
		},
		{
			name:     "not found",
			content:  `version = "1.0"`,
			attr:     "name",
			expected: "",
		},
		{
			name:     "first occurrence wins (duplicate definitions)",
			content:  `name = "first"\nname = "second"`,
			attr:     "name",
			expected: "first",
		},
		{
			name:     "with class definition",
			content:  "class MyConan:\n    name = \"mylib\"\n    version = \"1.0\"",
			attr:     "name",
			expected: "mylib",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPythonAttribute(tt.content, tt.attr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectBuildInfo(t *testing.T) {
	cf := &ConanFlexPack{
		projectName:    "testproject",
		projectVersion: "1.0.0",
		user:           "_",
		channel:        "_",
		initialized:    true,
		dependencies: []entities.Dependency{
			{
				Id:     "dep1:1.0",
				Scopes: []string{"runtime"},
			},
			{
				Id:     "dep2:2.0",
				Scopes: []string{"build"},
			},
		},
		requestedByMap: map[string][]string{
			"dep2:2.0": {"dep1:1.0"},
		},
		config: ConanConfig{
			ConanExecutable: "echo",
		},
	}
	buildInfo, err := cf.CollectBuildInfo("test-build", "1")
	assert.NoError(t, err)
	assert.NotNil(t, buildInfo)
	assert.Equal(t, "test-build", buildInfo.Name)
	assert.Equal(t, "1", buildInfo.Number)
	assert.NotEmpty(t, buildInfo.Started)
	assert.Len(t, buildInfo.Modules, 1)
	module := buildInfo.Modules[0]
	assert.Equal(t, "testproject:1.0.0", module.Id)
	assert.Equal(t, entities.ModuleType("conan"), module.Type)
	assert.Len(t, module.Dependencies, 2)
}

func TestLoadConanfile(t *testing.T) {
	t.Run("conanfile.py preferred over conanfile.txt", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		// Create both files
		pyContent := `name = "mylib"\nversion = "1.0.0"`
		txtContent := "[requires]\nzlib/1.2.13"
		err = os.WriteFile(filepath.Join(tempDir, "conanfile.py"), []byte(pyContent), 0644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, "conanfile.txt"), []byte(txtContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{WorkingDirectory: tempDir},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Contains(t, cf.conanfilePath, "conanfile.py")
	})
	t.Run("conanfile.txt fallback", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		txtContent := "[requires]\nzlib/1.2.13"
		err = os.WriteFile(filepath.Join(tempDir, "conanfile.txt"), []byte(txtContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{WorkingDirectory: tempDir},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Contains(t, cf.conanfilePath, "conanfile.txt")
		assert.Equal(t, filepath.Base(tempDir), cf.projectName)
	})
	t.Run("no conanfile error", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		cf := &ConanFlexPack{
			config: ConanConfig{WorkingDirectory: tempDir},
		}
		err = cf.loadConanfile()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no conanfile.py or conanfile.txt found")
	})
}

func TestBuildGraphInfoArgs(t *testing.T) {
	cf := &ConanFlexPack{
		conanfilePath: "/path/to/conanfile.py",
		config: ConanConfig{
			Profile: "default",
			Settings: map[string]string{
				"build_type": "Release",
			},
			Options: map[string]string{
				"shared": "True",
			},
		},
	}
	args := cf.buildGraphInfoArgs()
	assert.Contains(t, args, "graph")
	assert.Contains(t, args, "info")
	assert.Contains(t, args, "/path/to/conanfile.py")
	assert.Contains(t, args, "--format=json")
	assert.Contains(t, args, "-pr")
	assert.Contains(t, args, "default")
	assert.Contains(t, args, "-s")
	assert.Contains(t, args, "build_type=Release")
	assert.Contains(t, args, "-o")
	assert.Contains(t, args, "shared=True")
}

func TestFindConanPackageFile(t *testing.T) {
	t.Run("finds manifest file", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-pkg-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		manifestPath := filepath.Join(tempDir, "conanmanifest.txt")
		err = os.WriteFile(manifestPath, []byte("manifest content"), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{}
		result, err := cf.findConanPackageFile(tempDir)
		assert.NoError(t, err)
		assert.Equal(t, manifestPath, result)
	})
	t.Run("finds conanfile.py as fallback", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-pkg-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		conanfilePath := filepath.Join(tempDir, "conanfile.py")
		err = os.WriteFile(conanfilePath, []byte("conanfile content"), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{}
		result, err := cf.findConanPackageFile(tempDir)
		assert.NoError(t, err)
		assert.Equal(t, conanfilePath, result)
	})
	t.Run("returns error when no file found", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-pkg-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		cf := &ConanFlexPack{}
		_, err = cf.findConanPackageFile(tempDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no checksummable file found")
	})
}

func TestProcessDependencyNodeWithEmptyRef(t *testing.T) {
	cf := &ConanFlexPack{
		requestedByMap: make(map[string][]string),
		dependencies:   []entities.Dependency{},
		graphData:      &ConanGraphOutput{},
	}
	processedDeps := make(map[string]bool)
	// Node with empty ref should be skipped
	cf.processDependencyNode(ConanGraphNode{Ref: ""}, ConanDependencyEdge{}, "root:1.0", processedDeps)
	assert.Len(t, cf.dependencies, 0)
}

func TestProcessDependencyNodeWithNameVersion(t *testing.T) {
	cf := &ConanFlexPack{
		requestedByMap: make(map[string][]string),
		dependencies:   []entities.Dependency{},
		graphData:      &ConanGraphOutput{},
		config:         ConanConfig{ConanExecutable: "echo"},
	}
	processedDeps := make(map[string]bool)
	// Node with name/version instead of ref
	cf.processDependencyNode(
		ConanGraphNode{Name: "zlib", Version: "1.2.13", Context: "host"},
		ConanDependencyEdge{},
		"root:1.0",
		processedDeps,
	)
	require.Len(t, cf.dependencies, 1)
	assert.Equal(t, "zlib:1.2.13", cf.dependencies[0].Id)
}

// findDependencyByID finds a dependency by its ID
func findDependencyByID(deps []entities.Dependency, id string) *entities.Dependency {
	for _, dep := range deps {
		if dep.Id == id {
			return &dep
		}
	}
	return nil
}
