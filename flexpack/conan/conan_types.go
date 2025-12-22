package conan

// Conan types and data structures

// ConanConfig represents configuration for Conan FlexPack
type ConanConfig struct {
	WorkingDirectory string
	ConanExecutable  string
	Profile          string
	Settings         map[string]string
	Options          map[string]string
}

// ConanGraphOutput represents the output of 'conan graph info --format=json'
type ConanGraphOutput struct {
	Graph struct {
		Nodes map[string]ConanGraphNode `json:"nodes"`
	} `json:"graph"`
	RootRef string `json:"root_ref"`
}

// ConanGraphNode represents a node in the Conan dependency graph
type ConanGraphNode struct {
	Ref          string                         `json:"ref"`
	DisplayName  string                         `json:"display_name"`
	Context      string                         `json:"context"`
	Dependencies map[string]ConanDependencyEdge `json:"dependencies"`
	Settings     map[string]string              `json:"settings"`
	Options      map[string]string              `json:"options"`
	Path         string                         `json:"path"`
	PackageId    string                         `json:"package_id"`
	Revision     string                         `json:"rrev"`
	Binary       string                         `json:"binary"`
	Name         string                         `json:"name"`
	Version      string                         `json:"version"`
}

// ConanDependencyEdge represents an edge in the dependency graph
type ConanDependencyEdge struct {
	Ref     string `json:"ref"`
	Require string `json:"require"`
	Build   bool   `json:"build"`
	Test    bool   `json:"test"`
	Direct  bool   `json:"direct"`
	Run     bool   `json:"run"`
	Visible bool   `json:"visible"`
}

// ConanLockFile represents the structure of conan.lock file
type ConanLockFile struct {
	Version        string                   `json:"version"`
	Requires       []string                 `json:"requires"`
	BuildRequires  []string                 `json:"build_requires"`
	PythonRequires []string                 `json:"python_requires"`
	Graph          map[string]ConanLockNode `json:"graph"`
}

// ConanLockNode represents a node in conan.lock
type ConanLockNode struct {
	Ref           string            `json:"ref"`
	Options       map[string]string `json:"options"`
	Settings      map[string]string `json:"settings"`
	Requires      []string          `json:"requires"`
	BuildRequires []string          `json:"build_requires"`
	Path          string            `json:"path"`
	PackageId     string            `json:"package_id"`
}

