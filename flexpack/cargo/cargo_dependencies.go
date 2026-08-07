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

// scopeForDepKinds maps cargo dep_kinds to a build-info scope and decides inclusion,
// using cargo's own dep-kind names verbatim ("normal", "build", "dev") rather than a
// renaming layer. Cargo represents a normal dependency with an empty Kind, which we
// surface as "normal" so build-info readers see the same vocabulary as `cargo tree`
// and Cargo.toml. A dependency with multiple kinds prefers normal > build > dev.
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
		return "normal", true
	case hasBuild:
		return "build", true
	case hasDev:
		return "dev", includeDev
	default:
		return "normal", true
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

// metadataArgs builds the argument list for `cargo metadata`, appending caller-supplied extra args.
func metadataArgs(extra []string) []string {
	args := []string{"metadata", "--format-version", "1"}
	return append(args, extra...)
}

// countRegistryNodes returns how many resolve nodes the collector should produce for
// the given includeDev setting: registry-sourced, not workspace members, and passing the
// same dep-kind inclusion filter as collectDependenciesFromMeta. Applying that filter
// keeps the reconciliation count aligned with collection (e.g. registry-sourced
// dev-dependencies are excluded when includeDev is false), so the mismatch warning only
// fires on genuine dependency loss rather than on every project that has dev-dependencies.
func countRegistryNodes(meta *CargoMetadata, includeDev bool) int {
	workspace := make(map[string]bool)
	for _, id := range meta.WorkspaceMembers {
		workspace[id] = true
	}
	// Seed from every workspace member plus the resolve root, then walk the same kind-aware
	// reachability the collector uses — so the expected count excludes dev-only subtrees exactly
	// as collection does, and the mismatch warning only fires on genuine dependency loss.
	roots := make(map[string]bool)
	for id := range workspace {
		roots[id] = true
	}
	if meta.Resolve.Root != "" {
		roots[meta.Resolve.Root] = true
	}
	reachable := reachableFrom(meta, roots, includeDev)
	n := 0
	for id := range reachable {
		if workspace[id] {
			continue
		}
		_, _, source := parsePackageId(id)
		if !strings.HasPrefix(source, "registry+") {
			continue
		}
		n++
	}
	return n
}

// runCargoMetadata runs `cargo metadata --format-version 1` in the working dir.
func (cf *CargoFlexPack) runCargoMetadata() ([]byte, error) {
	cmd := exec.Command(cf.config.CargoExecutable, metadataArgs(cf.config.MetadataArgs)...)
	cmd.Dir = cf.config.WorkingDirectory
	return cmd.Output()
}

// fileId maps a cargo resolve-node id to the identifier used in build-info: registry crates
// become "<name>-<version>.crate"; first-party nodes (workspace/root, git, path) use the crate name.
func fileId(nodeId string) string {
	name, version, source := parsePackageId(nodeId)
	if strings.HasPrefix(source, "registry+") {
		return name + "-" + version + ".crate"
	}
	return name
}

// nodeLabel is the identifier a node contributes to a dependency's requestedBy path. Registry
// crates use their build-info dependency id ("<name>-<version>.crate"); a workspace member or the
// resolve root uses the module id ("<name>:<version>") — so a requestedBy path terminates at the
// same id as the module it belongs to, matching the Go/npm/yarn/nuget convention (root element ==
// module.Id).
func (cf *CargoFlexPack) nodeLabel(nodeId string, workspace map[string]bool) string {
	if workspace[nodeId] || nodeId == cf.meta.Resolve.Root {
		return moduleIdForMember(nodeId)
	}
	return fileId(nodeId)
}

// allRootIds returns the seed set for the whole-project dependency list: every workspace
// member plus the resolve root (for single-crate projects the two coincide).
func (cf *CargoFlexPack) allRootIds() map[string]bool {
	roots := make(map[string]bool)
	for _, id := range cf.meta.WorkspaceMembers {
		roots[id] = true
	}
	if cf.meta.Resolve.Root != "" {
		roots[cf.meta.Resolve.Root] = true
	}
	return roots
}

// edgeHasNonDevKind reports whether a resolve edge was pulled in by a normal ("") or build kind.
// An edge with no dep_kinds recorded (older cargo metadata) is treated as normal.
func edgeHasNonDevKind(kinds []CargoDepKind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if k.Kind == "" || k.Kind == "build" {
			return true
		}
	}
	return false
}

// childEdges returns the child node ids of a node. When includeDev is false, dev-only edges are
// skipped, so a package reachable ONLY through a dev-dependency (e.g. a dev-dep's own transitive
// subtree) is not traversed. Uses node.Deps (which carries dep_kinds); falls back to the flat
// node.Dependencies list (kind-unaware) when Deps is absent (older cargo metadata).
func childEdges(node CargoNode, includeDev bool) []string {
	if len(node.Deps) == 0 {
		return node.Dependencies
	}
	out := make([]string, 0, len(node.Deps))
	for _, d := range node.Deps {
		if includeDev || edgeHasNonDevKind(d.DepKinds) {
			out = append(out, d.Pkg)
		}
	}
	return out
}

// reachableFrom returns the set of resolve-node ids reachable (transitively) from any id in
// rootIds. When includeDev is false, dev-only edges are not followed — so a dependency reachable
// only through a dev-dependency is correctly excluded (matching what a non-test build compiles).
// Workspace members are traversed through (so a member's deps pulled in via a sibling
// path-dependency are still reached) but the caller excludes members from the emitted list.
func reachableFrom(meta *CargoMetadata, rootIds map[string]bool, includeDev bool) map[string]bool {
	byId := make(map[string]CargoNode, len(meta.Resolve.Nodes))
	for _, n := range meta.Resolve.Nodes {
		byId[n.Id] = n
	}
	reached := make(map[string]bool)
	var stack []string
	for id := range rootIds {
		stack = append(stack, id)
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[id] {
			continue
		}
		reached[id] = true
		for _, child := range childEdges(byId[id], includeDev) {
			if !reached[child] {
				stack = append(stack, child)
			}
		}
	}
	return reached
}

// depsForRoots builds the build-info dependency list for the subgraph reachable from rootIds:
// registry-sourced crates only, workspace members skipped, dev-deps filtered per config, scopes
// taken verbatim from cargo's dep_kinds ("normal"/"build"/"dev"), and RequestedBy as a list of
// single-element paths — one per direct parent — matching the pnpm/conan/poetry/maven convention
// (no full chains, no root at the end). Cycle-guarded via NodeHasLoop, capped at
// entities.RequestedByMaxLength.
//
// Called once with every member/root for the whole-project list (cf.dependencies) and once per
// workspace member to build that member's own module — so each module carries the dependencies
// it actually pulls in, matching Maven/Gradle multi-module build-info.
func (cf *CargoFlexPack) depsForRoots(rootIds map[string]bool) []entities.Dependency {
	includeDev := cf.config.IncludeDevDependencies
	workspace := make(map[string]bool)
	for _, id := range cf.meta.WorkspaceMembers {
		workspace[id] = true
	}
	reachable := reachableFrom(cf.meta, rootIds, includeDev)

	// Map id -> the dep_kinds it was pulled in with, unioned across edges within the reachable set.
	// Dev-only edges are ignored when includeDev is false so a crate's scope reflects the non-dev
	// path that actually reaches it.
	kindsById := make(map[string][]CargoDepKind)
	for _, node := range cf.meta.Resolve.Nodes {
		if !reachable[node.Id] {
			continue
		}
		for _, d := range node.Deps {
			if !reachable[d.Pkg] {
				continue
			}
			if !includeDev && !edgeHasNonDevKind(d.DepKinds) {
				continue
			}
			kindsById[d.Pkg] = append(kindsById[d.Pkg], d.DepKinds...)
		}
	}

	// First pass: build included registry dependencies (without RequestedBy), keyed by build-info
	// id, preserving encounter order for stable output.
	included := make(map[string]bool) // node id -> included
	byKey := make(map[string]entities.Dependency)
	nodeKey := make(map[string]string) // node id -> build-info id
	var order []string
	for _, node := range cf.meta.Resolve.Nodes {
		if !reachable[node.Id] || workspace[node.Id] {
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
		key := name + "-" + version + ".crate"
		byKey[key] = entities.Dependency{
			Id:       key,
			Type:     "crate",
			Scopes:   []string{scope},
			Checksum: cf.resolveChecksum(name, version, cf.lockChecksums[name+"|"+version]),
		}
		included[node.Id] = true
		nodeKey[node.Id] = key
		order = append(order, key)
	}

	// Second pass: for every edge parent -> child within the reachable subgraph where the child
	// is an included registry dep, record the parent as a direct requester of that child. Each
	// direct parent contributes one single-element path. Parents that are workspace members or
	// the resolve root are labelled by their module id via nodeLabel; registry parents by the
	// same "<name>-<version>.crate" build-info id used elsewhere.
	for _, node := range cf.meta.Resolve.Nodes {
		if !reachable[node.Id] {
			continue
		}
		parentLabel := cf.nodeLabel(node.Id, workspace)
		for _, childId := range childEdges(node, includeDev) {
			childKey, ok := nodeKey[childId]
			if !ok {
				continue
			}
			child := byKey[childKey]
			if parentLabel == child.Id {
				continue // cargo can list the same node in its own edge list on cycles; skip self-parents
			}
			if len(child.RequestedBy) >= entities.RequestedByMaxLength {
				continue
			}
			// Deduplicate: skip if this parent already recorded.
			already := false
			for _, path := range child.RequestedBy {
				if len(path) == 1 && path[0] == parentLabel {
					already = true
					break
				}
			}
			if already {
				continue
			}
			child.RequestedBy = append(child.RequestedBy, []string{parentLabel})
			byKey[childKey] = child
		}
	}

	deps := make([]entities.Dependency, 0, len(order))
	for _, key := range order {
		deps = append(deps, byKey[key])
	}
	return deps
}

// collectDependenciesFromMeta populates cf.dependencies with the whole-project dependency list
// (every workspace member + resolve root as seeds). Per-module lists are built separately by
// buildModules via depsForRoots.
func (cf *CargoFlexPack) collectDependenciesFromMeta() error {
	cf.dependencies = cf.depsForRoots(cf.allRootIds())
	return nil
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
	if err := cf.collectDependenciesFromMeta(); err != nil {
		return err
	}
	expected := countRegistryNodes(cf.meta, cf.config.IncludeDevDependencies)
	log.Debug(fmt.Sprintf("cargo: reconciliation — collected %d dependencies, %d registry nodes in resolve graph, %d packages in Cargo.lock",
		len(cf.dependencies), expected, len(cf.lockChecksums)))
	if len(cf.dependencies) != expected {
		log.Warn(fmt.Sprintf("cargo: dependency count mismatch — collected %d but resolve graph has %d registry nodes", len(cf.dependencies), expected))
	}
	return nil
}
