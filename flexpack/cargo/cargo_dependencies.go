package cargo

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// parsePackageId normalizes a cargo metadata package id into (name, version, source).
// Handles both the pre-1.77 form "name version (source)" and the >=1.77
// PackageIdSpec form "source#name@version" or "source#version".
func parsePackageId(id string) (name, version, source string) {
	id = strings.TrimSpace(id)
	// New PackageIdSpec form: contains '#'.
	if hashIdx := strings.LastIndex(id, "#"); hashIdx != -1 {
		source = id[:hashIdx]
		spec := id[hashIdx+1:]
		if at := strings.LastIndex(spec, "@"); at != -1 {
			// "name@version"
			return spec[:at], spec[at+1:], source
		}
		// "version" only — derive name from the last path segment of source.
		version = spec
		name = lastPathSegment(source)
		return name, version, source
	}
	// Old form: "name version (source)".
	openParen := strings.Index(id, " (")
	if openParen != -1 && strings.HasSuffix(id, ")") {
		source = id[openParen+2 : len(id)-1]
		id = id[:openParen]
	}
	fields := strings.Fields(id)
	if len(fields) >= 2 {
		return fields[0], fields[1], source
	}
	return id, "", source
}

// lastPathSegment returns the final path/url segment, stripping any scheme prefix
// like "path+file:///a/b/mycrate" -> "mycrate".
func lastPathSegment(s string) string {
	s = strings.TrimRight(s, "/")
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		return s[idx+1:]
	}
	return s
}

// scopeForDepKinds maps cargo dep_kinds to a build-info scope and decides inclusion.
// Normal ("") -> "prod", "build" -> "build", "dev" -> "dev" (only if includeDev).
// A dependency with multiple kinds prefers normal > build > dev.
func scopeForDepKinds(kinds []CargoDepKind, includeDev bool) (string, bool) {
	hasNormal, hasBuild, hasDev := false, false, false
	for _, k := range kinds {
		switch k.Kind {
		case "":
			hasNormal = true
		case "build":
			hasBuild = true
		case "dev":
			hasDev = true
		}
	}
	switch {
	case hasNormal:
		return "prod", true
	case hasBuild:
		return "build", true
	case hasDev:
		return "dev", includeDev
	default:
		return "prod", true
	}
}

// buildRequestedBy reverses the resolve graph: dependency id -> parent ids.
func buildRequestedBy(meta *CargoMetadata) map[string][]string {
	rb := make(map[string][]string)
	for _, node := range meta.Resolve.Nodes {
		for _, childId := range node.Dependencies {
			rb[childId] = appendUnique(rb[childId], node.Id)
		}
	}
	return rb
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}

func cargoHome() string {
	if h := os.Getenv("CARGO_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cargo")
}

// findCachedCrate searches $CARGO_HOME/registry/cache/<registry-hash>/<name>-<version>.crate
// across all registry-hash subdirectories.
func findCachedCrate(home, name, version string) string {
	if home == "" {
		return ""
	}
	pattern := filepath.Join(home, "registry", "cache", "*", name+"-"+version+".crate")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// resolveChecksum returns checksums for a dependency: local .crate first (all three
// hashes), else the lockfile sha256, else empty (Artifactory enriches server-side).
func (cf *CargoFlexPack) resolveChecksum(name, version, lockSha256 string) entities.Checksum {
	if path := findCachedCrate(cargoHome(), name, version); path != "" {
		if fd, err := crypto.GetFileDetails(path, true); err == nil {
			log.Debug("cargo: checksums for " + name + "-" + version + " from local cache")
			return entities.Checksum{Sha1: fd.Checksum.Sha1, Sha256: fd.Checksum.Sha256, Md5: fd.Checksum.Md5}
		}
	}
	if lockSha256 != "" {
		log.Debug("cargo: checksum for " + name + "-" + version + " from Cargo.lock (sha256 only)")
		return entities.Checksum{Sha256: lockSha256}
	}
	log.Debug("cargo: no local checksum for " + name + "-" + version + ", leaving for server enrichment")
	return entities.Checksum{}
}

func parseMetadata(data []byte) (*CargoMetadata, error) {
	var m CargoMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse cargo metadata: %w", err)
	}
	return &m, nil
}

func parseLockfile(path string) (map[string]string, error) {
	var lock CargoLock
	if _, err := toml.DecodeFile(path, &lock); err != nil {
		return nil, fmt.Errorf("parse Cargo.lock: %w", err)
	}
	out := make(map[string]string, len(lock.Package))
	for _, p := range lock.Package {
		out[p.Name+"|"+p.Version] = p.Checksum
	}
	return out, nil
}

// runCargoMetadata runs `cargo metadata --format-version 1` in the working dir.
func (cf *CargoFlexPack) runCargoMetadata() ([]byte, error) {
	cmd := exec.Command(cf.config.CargoExecutable, "metadata", "--format-version", "1")
	cmd.Dir = cf.config.WorkingDirectory
	return cmd.Output()
}

// collectDependenciesFromMeta walks cf.meta and populates cf.dependencies,
// skipping workspace members and non-registry sources.
func (cf *CargoFlexPack) collectDependenciesFromMeta() error {
	workspace := make(map[string]bool)
	for _, id := range cf.meta.WorkspaceMembers {
		workspace[id] = true
	}
	requestedBy := buildRequestedBy(cf.meta)
	// Map id -> the dep_kinds it was pulled in with (union across parents).
	kindsById := make(map[string][]CargoDepKind)
	for _, node := range cf.meta.Resolve.Nodes {
		for _, d := range node.Deps {
			kindsById[d.Pkg] = append(kindsById[d.Pkg], d.DepKinds...)
		}
	}
	cf.dependencies = nil
	for _, node := range cf.meta.Resolve.Nodes {
		if workspace[node.Id] {
			continue
		}
		name, version, source := parsePackageId(node.Id)
		if !strings.HasPrefix(source, "registry+") {
			continue // skip git/path/local sources
		}
		scope, include := scopeForDepKinds(kindsById[node.Id], cf.config.IncludeDevDependencies)
		if !include {
			continue
		}
		dep := entities.Dependency{
			Id:       name + "-" + version + ".crate",
			Type:     "crate",
			Scopes:   []string{scope},
			Checksum: cf.resolveChecksum(name, version, cf.lockChecksums[name+"|"+version]),
		}
		if parents := requestedBy[node.Id]; len(parents) > 0 {
			dep.RequestedBy = [][]string{toFilenames(parents)}
		}
		cf.dependencies = append(cf.dependencies, dep)
	}
	return nil
}

// toFilenames converts parent package ids to <name>-<version>.crate (root stays as id).
func toFilenames(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name, version, source := parsePackageId(id)
		if strings.HasPrefix(source, "registry+") {
			out = append(out, name+"-"+version+".crate")
		} else {
			out = append(out, name) // workspace/root: use crate name
		}
	}
	return out
}

// collectDependencies runs cargo metadata, loads the lockfile, and populates deps.
func (cf *CargoFlexPack) collectDependencies() error {
	out, err := cf.runCargoMetadata()
	if err == nil {
		if meta, perr := parseMetadata(out); perr == nil {
			cf.meta = meta
		}
	}
	// Load lockfile checksums (best-effort).
	lockPath := filepath.Join(cf.config.WorkingDirectory, "Cargo.lock")
	if lock, lerr := parseLockfile(lockPath); lerr == nil {
		cf.lockChecksums = lock
	}
	if cf.meta == nil {
		return fmt.Errorf("could not obtain cargo metadata in %s", cf.config.WorkingDirectory)
	}
	return cf.collectDependenciesFromMeta()
}
