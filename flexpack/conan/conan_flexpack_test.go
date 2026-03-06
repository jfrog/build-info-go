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

func TestExtractProjectInfoUsingConanInspect(t *testing.T) {
	// Skip if conan is not installed
	if _, err := findConanExecutable(); err != nil {
		t.Skip("Conan not installed, skipping conan inspect test")
	}
	tempDir, err := os.MkdirTemp("", "conan-inspect-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	// Create a conanfile.py with project metadata
	conanfileContent := `from conan import ConanFile

class TestPackage(ConanFile):
    name = "testpkg"
    version = "2.5.0"
    user = "myorg"
    channel = "testing"
`
	err = os.WriteFile(filepath.Join(tempDir, "conanfile.py"), []byte(conanfileContent), 0644)
	require.NoError(t, err)
	cf := &ConanFlexPack{
		config: ConanConfig{
			WorkingDirectory: tempDir,
			ConanExecutable:  "conan",
		},
		conanfilePath: filepath.Join(tempDir, "conanfile.py"),
	}
	err = cf.extractProjectInfoUsingConanInspect()
	require.NoError(t, err)
	assert.Equal(t, "testpkg", cf.projectName)
	assert.Equal(t, "2.5.0", cf.projectVersion)
	assert.Equal(t, "myorg", cf.user)
	assert.Equal(t, "testing", cf.channel)
}

func TestExtractProjectInfoFallbackToPythonParsing(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conan-fallback-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	// Create a conanfile.py with project metadata
	conanfileContent := `from conan import ConanFile

class TestPackage(ConanFile):
    name = "fallbackpkg"
    version = "1.0.0"
`
	err = os.WriteFile(filepath.Join(tempDir, "conanfile.py"), []byte(conanfileContent), 0644)
	require.NoError(t, err)
	cf := &ConanFlexPack{
		config: ConanConfig{
			WorkingDirectory: tempDir,
			ConanExecutable:  "nonexistent-conan-binary", // Force fallback
		},
		conanfilePath: filepath.Join(tempDir, "conanfile.py"),
	}
	// extractProjectInfoFromConanfilePy should fallback to regex parsing
	err = cf.extractProjectInfoFromConanfilePy()
	require.NoError(t, err)
	assert.Equal(t, "fallbackpkg", cf.projectName)
	assert.Equal(t, "1.0.0", cf.projectVersion)
	assert.Equal(t, "_", cf.user)
	assert.Equal(t, "_", cf.channel)
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
	t.Run("no conanfile uses defaults", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		cf := &ConanFlexPack{
			config: ConanConfig{WorkingDirectory: tempDir},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err, "loadConanfile should not fail when no conanfile exists (--requires mode)")
		assert.Empty(t, cf.conanfilePath)
		assert.Equal(t, filepath.Base(tempDir), cf.projectName)
		assert.Equal(t, "", cf.projectVersion)
		assert.Equal(t, "_", cf.user)
		assert.Equal(t, "_", cf.channel)
	})
	t.Run("no conanfile with name/version overrides", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		cf := &ConanFlexPack{
			config: ConanConfig{
				WorkingDirectory:       tempDir,
				ProjectNameOverride:    "my-virtual-pkg",
				ProjectVersionOverride: "2.0.0",
			},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Equal(t, "my-virtual-pkg", cf.projectName)
		assert.Equal(t, "2.0.0", cf.projectVersion)
	})
	t.Run("no conanfile with all reference overrides", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		cf := &ConanFlexPack{
			config: ConanConfig{
				WorkingDirectory:       tempDir,
				ProjectNameOverride:    "mypkg",
				ProjectVersionOverride: "1.0.0",
				UserOverride:           "myuser",
				ChannelOverride:        "stable",
			},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Equal(t, "mypkg", cf.projectName)
		assert.Equal(t, "1.0.0", cf.projectVersion)
		assert.Equal(t, "myuser", cf.user)
		assert.Equal(t, "stable", cf.channel)
	})
	t.Run("user/channel overrides with conanfile", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		pyContent := `name = "original"
version = "1.0.0"`
		err = os.WriteFile(filepath.Join(tempDir, "conanfile.py"), []byte(pyContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{
				WorkingDirectory: tempDir,
				UserOverride:     "ci-user",
				ChannelOverride:  "testing",
			},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Equal(t, "original", cf.projectName)
		assert.Equal(t, "ci-user", cf.user)
		assert.Equal(t, "testing", cf.channel)
	})
	t.Run("RecipeFilePath finds conanfile in subdirectory", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		recipeDir := filepath.Join(tempDir, "recipes", "mylib")
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		pyContent := `name = "mylib"
version = "1.0.0"`
		err = os.WriteFile(filepath.Join(recipeDir, "conanfile.py"), []byte(pyContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{
				WorkingDirectory: tempDir,
				RecipeFilePath:   recipeDir,
			},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Contains(t, cf.conanfilePath, filepath.Join("recipes", "mylib", "conanfile.py"))
		assert.Equal(t, "mylib", cf.projectName)
		assert.Equal(t, "1.0.0", cf.projectVersion)
	})
	t.Run("RecipeFilePath with no conanfile in cwd succeeds", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		rootDir := filepath.Join(tempDir, "project")
		recipeDir := filepath.Join(tempDir, "recipes")
		require.NoError(t, os.MkdirAll(rootDir, 0755))
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		txtContent := "[requires]\nzlib/1.2.13"
		err = os.WriteFile(filepath.Join(recipeDir, "conanfile.txt"), []byte(txtContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{
				WorkingDirectory: rootDir,
				RecipeFilePath:   recipeDir,
			},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Contains(t, cf.conanfilePath, filepath.Join("recipes", "conanfile.txt"))
	})
	t.Run("name and version overrides from CLI flags", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		pyContent := `from conan import ConanFile
class MyPkg(ConanFile):
    pass`
		err = os.WriteFile(filepath.Join(tempDir, "conanfile.py"), []byte(pyContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{
				WorkingDirectory:       tempDir,
				ProjectNameOverride:    "overridden-name",
				ProjectVersionOverride: "9.9.9",
			},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Equal(t, "overridden-name", cf.projectName)
		assert.Equal(t, "9.9.9", cf.projectVersion)
	})
	t.Run("overrides only apply when set", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "conan-load-test-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()
		pyContent := `name = "original"
version = "1.0.0"`
		err = os.WriteFile(filepath.Join(tempDir, "conanfile.py"), []byte(pyContent), 0644)
		require.NoError(t, err)
		cf := &ConanFlexPack{
			config: ConanConfig{WorkingDirectory: tempDir},
		}
		err = cf.loadConanfile()
		assert.NoError(t, err)
		assert.Equal(t, "original", cf.projectName)
		assert.Equal(t, "1.0.0", cf.projectVersion)
	})
}

func TestBuildGraphInfoArgs(t *testing.T) {
	t.Run("with conanfile path", func(t *testing.T) {
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
	})
	t.Run("without conanfile uses --requires from args", func(t *testing.T) {
		cf := &ConanFlexPack{
			conanfilePath: "",
			config: ConanConfig{
				ConanArgs: []string{"--requires", "zlib/1.2.11", "--build", "zlib/1.2.11"},
			},
		}
		args := cf.buildGraphInfoArgs()
		assert.Contains(t, args, "graph")
		assert.Contains(t, args, "info")
		assert.Contains(t, args, "--requires=zlib/1.2.11")
		assert.Contains(t, args, "--format=json")
		assert.NotContains(t, args, ".")
	})
	t.Run("without conanfile with --requires=value form", func(t *testing.T) {
		cf := &ConanFlexPack{
			conanfilePath: "",
			config: ConanConfig{
				ConanArgs: []string{"--requires=zlib/1.2.11", "--requires=openssl/3.0.0"},
			},
		}
		args := cf.buildGraphInfoArgs()
		assert.Contains(t, args, "--requires=zlib/1.2.11")
		assert.Contains(t, args, "--requires=openssl/3.0.0")
		assert.NotContains(t, args, ".")
	})
	t.Run("without conanfile uses --tool-requires from args", func(t *testing.T) {
		cf := &ConanFlexPack{
			conanfilePath: "",
			config: ConanConfig{
				ConanArgs: []string{"--tool-requires", "cmake/3.23.5", "--tool-requires=ninja/1.11.0"},
			},
		}
		args := cf.buildGraphInfoArgs()
		assert.Contains(t, args, "--tool-requires=cmake/3.23.5")
		assert.Contains(t, args, "--tool-requires=ninja/1.11.0")
		assert.NotContains(t, args, ".")
	})
	t.Run("without conanfile with both --requires and --tool-requires", func(t *testing.T) {
		cf := &ConanFlexPack{
			conanfilePath: "",
			config: ConanConfig{
				ConanArgs: []string{"--requires=zlib/1.2.11", "--tool-requires=cmake/3.23.5"},
			},
		}
		args := cf.buildGraphInfoArgs()
		assert.Contains(t, args, "--requires=zlib/1.2.11")
		assert.Contains(t, args, "--tool-requires=cmake/3.23.5")
		assert.NotContains(t, args, ".")
	})
	t.Run("only --tool-requires without --requires still works", func(t *testing.T) {
		cf := &ConanFlexPack{
			conanfilePath: "",
			config: ConanConfig{
				ConanArgs: []string{"--tool-requires=cmake/3.23.5"},
			},
		}
		args := cf.buildGraphInfoArgs()
		assert.Contains(t, args, "--tool-requires=cmake/3.23.5")
		assert.NotContains(t, args, ".")
	})
	t.Run("without conanfile and no inline deps falls back to dot", func(t *testing.T) {
		cf := &ConanFlexPack{
			conanfilePath: "",
			config: ConanConfig{
				ConanArgs: []string{"--build", "zlib/1.2.11"},
			},
		}
		args := cf.buildGraphInfoArgs()
		assert.Contains(t, args, ".")
	})
}

func TestExtractRequiresFromArgs(t *testing.T) {
	t.Run("--requires value form", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--requires", "zlib/1.2.11", "--build", "zlib/1.2.11"},
			},
		}
		result := cf.extractRequiresFromArgs()
		assert.Equal(t, []string{"--requires=zlib/1.2.11"}, result)
	})
	t.Run("--requires=value form", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--requires=zlib/1.2.11"},
			},
		}
		result := cf.extractRequiresFromArgs()
		assert.Equal(t, []string{"--requires=zlib/1.2.11"}, result)
	})
	t.Run("multiple requires", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--requires=zlib/1.2.11", "--requires", "openssl/3.0.0"},
			},
		}
		result := cf.extractRequiresFromArgs()
		assert.Equal(t, []string{"--requires=zlib/1.2.11", "--requires=openssl/3.0.0"}, result)
	})
	t.Run("no requires returns empty", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--build", "missing"},
			},
		}
		result := cf.extractRequiresFromArgs()
		assert.Empty(t, result)
	})
}

func TestExtractToolRequiresFromArgs(t *testing.T) {
	t.Run("--tool-requires value form", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--tool-requires", "cmake/3.23.5"},
			},
		}
		result := cf.extractToolRequiresFromArgs()
		assert.Equal(t, []string{"--tool-requires=cmake/3.23.5"}, result)
	})
	t.Run("--tool-requires=value form", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--tool-requires=cmake/3.23.5"},
			},
		}
		result := cf.extractToolRequiresFromArgs()
		assert.Equal(t, []string{"--tool-requires=cmake/3.23.5"}, result)
	})
	t.Run("multiple tool-requires", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--tool-requires=cmake/3.23.5", "--tool-requires", "ninja/1.11.0"},
			},
		}
		result := cf.extractToolRequiresFromArgs()
		assert.Equal(t, []string{"--tool-requires=cmake/3.23.5", "--tool-requires=ninja/1.11.0"}, result)
	})
	t.Run("no tool-requires returns empty", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--requires=zlib/1.2.11"},
			},
		}
		result := cf.extractToolRequiresFromArgs()
		assert.Empty(t, result)
	})
	t.Run("does not pick up --requires", func(t *testing.T) {
		cf := &ConanFlexPack{
			config: ConanConfig{
				ConanArgs: []string{"--requires=zlib/1.2.11", "--tool-requires=cmake/3.23.5"},
			},
		}
		result := cf.extractToolRequiresFromArgs()
		assert.Equal(t, []string{"--tool-requires=cmake/3.23.5"}, result)
	})
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
