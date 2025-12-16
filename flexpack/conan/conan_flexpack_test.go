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

func TestParseDependenciesFromGraphInfo(t *testing.T) {
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
	cf.parseDependenciesFromGraphInfo(graphData)

	assert.Len(t, cf.dependencies, 2)

	zlibDep := findDependencyByID(cf.dependencies, "zlib:1.2.13")
	assert.NotNil(t, zlibDep)
	assert.Contains(t, zlibDep.Scopes, "runtime")

	cmakeDep := findDependencyByID(cf.dependencies, "cmake:3.25.0")
	assert.NotNil(t, cmakeDep)
	assert.Contains(t, cmakeDep.Scopes, "build")
}

func TestParseDependenciesWithTransitiveRequestedBy(t *testing.T) {
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
	cf.parseDependenciesFromGraphInfo(graphData)

	assert.Len(t, cf.dependencies, 3)

	// Check requestedBy relationships
	assert.Contains(t, cf.requestedByMap["libA:1.0"], "myapp:1.0")
	assert.Contains(t, cf.requestedByMap["libB:2.0"], "libA:1.0")
	assert.Contains(t, cf.requestedByMap["libC:3.0"], "libB:2.0")
}

func TestParseDependenciesWithDiamondDependency(t *testing.T) {
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
	cf.parseDependenciesFromGraphInfo(graphData)

	assert.Len(t, cf.dependencies, 3)

	// Check requestedBy - libC should have 2 requesters
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

func TestParseDependenciesFromLock(t *testing.T) {
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
			ConanExecutable:  "echo", // Prevent actual conan calls
		},
		dependencies: []entities.Dependency{},
	}

	err = cf.parseFromLockFile()
	require.NoError(t, err)

	assert.Len(t, cf.dependencies, 4)

	// Check requires (runtime dependencies)
	zlibDep := findDependencyByID(cf.dependencies, "zlib:1.2.13")
	assert.NotNil(t, zlibDep)
	assert.Contains(t, zlibDep.Scopes, "runtime")

	opensslDep := findDependencyByID(cf.dependencies, "openssl:3.0.5")
	assert.NotNil(t, opensslDep)
	assert.Contains(t, opensslDep.Scopes, "runtime")

	// Check build_requires
	cmakeDep := findDependencyByID(cf.dependencies, "cmake:3.25.0")
	assert.NotNil(t, cmakeDep)
	assert.Contains(t, cmakeDep.Scopes, "build")

	// Check python_requires
	pylintDep := findDependencyByID(cf.dependencies, "pylint:2.17.0")
	assert.NotNil(t, pylintDep)
	assert.Contains(t, pylintDep.Scopes, "python")
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

func TestCollectBuildInfo(t *testing.T) {
	cf := &ConanFlexPack{
		projectName:    "testproject",
		projectVersion: "1.0.0",
		user:           "_",
		channel:        "_",
		initialized:    true, // Skip lazy init
		dependencies: []entities.Dependency{
			{
				Id:     "dep1:1.0",
				Type:   "conan",
				Scopes: []string{"runtime"},
			},
			{
				Id:     "dep2:2.0",
				Type:   "conan",
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

// findDependencyByID finds a dependency by its ID
func findDependencyByID(deps []entities.Dependency, id string) *entities.Dependency {
	for _, dep := range deps {
		if dep.Id == id {
			return &dep
		}
	}
	return nil
}
