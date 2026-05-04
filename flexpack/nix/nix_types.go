package nix

// NixConfig holds configuration specific to Nix FlexPack operations
type NixConfig struct {
	WorkingDirectory string
	// IncludeDevInputs is intentionally unimplemented — Nix flakes do not
	// distinguish dev vs non-dev inputs at the lock file level. Reserved
	// for forward compatibility.
	IncludeDevInputs bool
}

// NixFlakeLock represents the top-level flake.lock JSON structure (version 7)
type NixFlakeLock struct {
	Version int                         `json:"version"`
	Root    string                      `json:"root"`
	Nodes   map[string]NixFlakeLockNode `json:"nodes"`
}

// NixFlakeLockNode represents a single node in flake.lock
type NixFlakeLockNode struct {
	// Inputs maps input names to either a string (direct node ref) or
	// an array of strings (follows alias path).
	Inputs   map[string]interface{} `json:"inputs,omitempty"`
	Locked   *NixLockedRef          `json:"locked,omitempty"`
	Original *NixOriginalRef        `json:"original,omitempty"`
	Flake    *bool                  `json:"flake,omitempty"`
}

// NixLockedRef represents the resolved/pinned reference for a dependency
type NixLockedRef struct {
	Type         string `json:"type"`
	Owner        string `json:"owner,omitempty"`
	Repo         string `json:"repo,omitempty"`
	Rev          string `json:"rev,omitempty"`
	Ref          string `json:"ref,omitempty"`
	NarHash      string `json:"narHash,omitempty"`
	LastModified int64  `json:"lastModified,omitempty"`
	URL          string `json:"url,omitempty"`
	Host         string `json:"host,omitempty"`
	Path         string `json:"path,omitempty"`
}

// NixPathInfo represents the output of "nix path-info --json" for a store path
type NixPathInfo struct {
	NarHash    string   `json:"narHash"`
	NarSize    int64    `json:"narSize"`
	Deriver    string   `json:"deriver,omitempty"`
	References []string `json:"references,omitempty"`
}

// NixOriginalRef represents the original (pre-resolution) input specification
type NixOriginalRef struct {
	Type  string `json:"type"`
	Owner string `json:"owner,omitempty"`
	Repo  string `json:"repo,omitempty"`
	Ref   string `json:"ref,omitempty"`
	URL   string `json:"url,omitempty"`
	Host  string `json:"host,omitempty"`
	Path  string `json:"path,omitempty"`
}
