package nix

// NixChannelConfig holds configuration for Nix channel-based build-info collection.
type NixChannelConfig struct {
	WorkingDirectory string
	ChannelName      string // e.g. "nixos-25.11"
}

// NixStorePathInfo represents a single entry from "nix path-info --json -r" output.
// The JSON output is a map keyed by store path.
type NixStorePathInfo struct {
	Path             string   `json:"path,omitempty"`
	NarHash          string   `json:"narHash"`
	NarSize          int64    `json:"narSize"`
	Deriver          string   `json:"deriver,omitempty"`
	References       []string `json:"references,omitempty"`
	RegistrationTime int64    `json:"registrationTime,omitempty"`
	Signatures       []string `json:"signatures,omitempty"`
}

// NixNarInfo represents the parsed content of a .narinfo file.
// Narinfo is a plain-text key-value format served by Nix binary caches.
type NixNarInfo struct {
	StorePath   string // /nix/store/<hash>-<name>
	URL         string // nar/<content-hash>.nar.xz
	Compression string // xz
	FileHash    string // sha256:<nix32-hash>
	FileSize    int64
	NarHash     string // sha256:<nix32-hash>
	NarSize     int64
	References  string // space-separated store path basenames
	Deriver     string // <hash>-<name>.drv
	System      string // x86_64-linux, aarch64-darwin
	Sig         string // cache.nixos.org-1:<base64>
}
