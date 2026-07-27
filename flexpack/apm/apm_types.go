package apm

// ApmConfig holds configuration for APM build-info collection.
type ApmConfig struct {
	// WorkingDirectory is the project root containing apm.yml/apm.lock.yaml. Defaults to "." when empty.
	WorkingDirectory string
}

// ApmLockFile represents apm.lock.yaml. The schema is a flat list under "dependencies",
// not a nested graph - apm.lock.yaml has no parent/child structure to recurse into.
type ApmLockFile struct {
	LockfileVersion string             `yaml:"lockfile_version"`
	Dependencies    []ApmLockedPackage `yaml:"dependencies"`
}

// ApmLockedPackage is a single entry in apm.lock.yaml's flat dependencies list.
type ApmLockedPackage struct {
	RepoURL      string `yaml:"repo_url"` // "owner/repo"
	Name         string `yaml:"name"`
	Version      string `yaml:"version"`
	PackageType  string `yaml:"package_type"`
	Source       string `yaml:"source"` // "registry" for registry-resolved deps
	ContentHash  string `yaml:"content_hash"`
	ResolvedURL  string `yaml:"resolved_url"`
	ResolvedHash string `yaml:"resolved_hash"`
}

// ApmManifest represents apm.yml. Only the fields needed to derive a module ID are modeled.
type ApmManifest struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// apmDepsWhyResult is the `apm deps why <repo_url> --json` response. Preferred over the
// lockfile's own depth/resolved_by fields, which aren't part of any documented schema (and
// aren't even modeled in ApmLockedPackage above) - this is a stable, documented command
// surface built specifically to answer "is this direct, and who pulled it in".
type apmDepsWhyResult struct {
	Package struct {
		IsDirect bool `json:"is_direct"`
	} `json:"package"`
	Paths []struct {
		Chain []struct {
			RepoURL string `json:"repo_url"`
		} `json:"chain"`
	} `json:"paths"`
}
