package nix

// NixConfig holds configuration for Nix build-info collection.
// The collector reads directly from the local Nix store via the `nix` CLI,
// so the same configuration works for channel-based (`nix-build`) and
// flakes-based (`nix build`) workflows.
type NixConfig struct {
	// WorkingDirectory is the project root. Defaults to "." when empty.
	// Used to discover the conventional `./result` build symlink when no
	// store paths are provided explicitly.
	WorkingDirectory string
	// NixExecutable overrides the `nix` binary used to run `path-info`
	// and `--version`. Defaults to "nix" (resolved on PATH).
	NixExecutable string
}

// NixStorePathInfo represents a single entry from "nix path-info --json --recursive" output.
// The JSON output is a map keyed by store path; we copy the key into Path
// after decoding so consumers don't have to track it separately.
type NixStorePathInfo struct {
	Path             string   `json:"path,omitempty"`
	NarHash          string   `json:"narHash"`
	NarSize          int64    `json:"narSize"`
	Deriver          string   `json:"deriver,omitempty"`
	References       []string `json:"references,omitempty"`
	RegistrationTime int64    `json:"registrationTime,omitempty"`
	Signatures       []string `json:"signatures,omitempty"`
}
