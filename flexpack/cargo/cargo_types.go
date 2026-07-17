package cargo

// CargoConfig holds configuration for Cargo build-info collection.
type CargoConfig struct {
	// WorkingDirectory is the directory containing Cargo.toml.
	WorkingDirectory string
	// CargoExecutable is the path to cargo (auto-detected if empty).
	CargoExecutable string
	// IncludeDevDependencies includes dev-dependencies when true.
	IncludeDevDependencies bool
	// MetadataArgs are extra args appended to `cargo metadata` (already filtered to metadata-valid flags by the caller, e.g. --features/--all-features/--locked).
	MetadataArgs []string
}

// CargoMetadata maps `cargo metadata --format-version 1` output.
type CargoMetadata struct {
	Packages         []CargoPackage `json:"packages"`
	Resolve          CargoResolve   `json:"resolve"`
	WorkspaceMembers []string       `json:"workspace_members"`
}

type CargoPackage struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Id           string `json:"id"`
	Source       string `json:"source"`
	ManifestPath string `json:"manifest_path"`
}

type CargoResolve struct {
	Nodes []CargoNode `json:"nodes"`
	Root  string      `json:"root"`
}

type CargoNode struct {
	Id           string         `json:"id"`
	Dependencies []string       `json:"dependencies"`
	Deps         []CargoNodeDep `json:"deps"`
}

type CargoNodeDep struct {
	Name     string         `json:"name"`
	Pkg      string         `json:"pkg"`
	DepKinds []CargoDepKind `json:"dep_kinds"`
}

type CargoDepKind struct {
	// Kind is "" (normal), "dev", or "build".
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

// CargoLock maps Cargo.lock (TOML).
type CargoLock struct {
	Package []CargoLockPackage `toml:"package"`
}

type CargoLockPackage struct {
	Name     string `toml:"name"`
	Version  string `toml:"version"`
	Source   string `toml:"source"`
	Checksum string `toml:"checksum"`
}
