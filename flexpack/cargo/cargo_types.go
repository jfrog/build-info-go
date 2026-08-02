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
	// SelectedPackages narrows build-info modules to just these workspace members (by their
	// package name — no version). Populated by the CLI from user-supplied -p/--package flags on
	// the underlying cargo command. When empty, cargo metadata's workspace_default_members is
	// used; older cargo (<1.71) that lacks that field falls back to all workspace members.
	SelectedPackages []string
}

// CargoMetadata maps `cargo metadata --format-version 1` output.
type CargoMetadata struct {
	Packages         []CargoPackage `json:"packages"`
	Resolve          CargoResolve   `json:"resolve"`
	WorkspaceMembers []string       `json:"workspace_members"`
	// WorkspaceDefaultMembers lists the members cargo would build by default when no -p is
	// given — respects the [workspace.default-members] setting. Added in cargo 1.71; older
	// cargo omits the field, in which case callers fall back to WorkspaceMembers.
	WorkspaceDefaultMembers []string `json:"workspace_default_members"`
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
