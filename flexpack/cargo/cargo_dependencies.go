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

// cargo-specific string constants surfaced across parsing, id-building and metadata invocation.
// cargoMetadataFormatVersion pins the stable schema (1) that build-info-go targets; cargo has
// not stabilised a v2 as of writing, so a hard-coded "1" is intentional — the const gives a
// single edit-point if that ever changes. cargoRegistrySourcePrefix / crateFileSuffix name the
// literals cargo uses in resolve-node source strings and in the artifact filename convention.
const (
	cargoMetadataFormatVersion = "1"
	cargoRegistrySourcePrefix  = "registry+"
	crateFileSuffix            = ".crate"
)

// parsePackageId normalizes a cargo metadata package id into (name, version, source).
// Handles both the pre-1.77 form "name version (source)" and the >=1.77
// PackageIdSpec form "source#name@version" or "source#version".
func parsePackageId(id string) (name, version, source string) {
	id = strings.TrimSpace(id)
	// New PackageIdSpec form: contains '#'. LastIndex guarantees 0 <= hashIdx < len(id), so
	// id[:hashIdx] is always in range; id[hashIdx+1:] is also in range (len(id) at worst,
	// yielding an empty spec). The explicit hashIdx+1 <= len(id) guard is there for the reader
	// so the bound is obvious at the call site, not because Go slicing needs it.
	if hashIdx := strings.LastIndex(id, "#"); hashIdx != -1 && hashIdx+1 <= len(id) {
		source = id[:hashIdx]
		spec := id[hashIdx+1:]
		if at := strings.LastIndex(spec, "@"); at != -1 && at+1 <= len(spec) {
			// "name@version"
			return spec[:at], spec[at+1:], source
		}
		// "version" only — derive name from the last path segment of source.
		version = spec
		name = lastPathSegment(source)
		return name, version, source
	}
	// Old form: "name version (source)". openParen+2 addresses the first char AFTER " (";
	// len(id)-1 is the trailing ')'. The explicit openParen+2 <= len(id)-1 guard rejects the
	// degenerate "x ()"-style input where the source would be an empty substring — same reason
	// as above: the reader shouldn't have to prove slice bounds in their head.
	openParen := strings.Index(id, " (")
	if openParen != -1 && strings.HasSuffix(id, ")") && openParen+2 <= len(id)-1 {
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
	// hasNormal/hasBuild/hasDev accumulate: they are set to true and never back to false, so a
	// later iteration can only re-assert an existing true. That is why the loop has no `break`
	// or per-kind `continue` — a dependency may appear multiple times with different Kind values
	// (a crate can be BOTH a normal AND a dev dependency), and we need to know every kind that
	// pulled it in so the normal>build>dev precedence below picks the strongest one, not the
	// first one cargo happened to emit.
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
			return entities.Checksum{Sha1: fd.Checksum.Sha1, Sha256: fd.Checksum.Sha256, Md5: fd.Checksum.Md5}
		}
	}
	if lockSha256 != "" {
		return entities.Checksum{Sha256: lockSha256}
	}
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
// The --format-version is pinned to cargoMetadataFormatVersion (currently "1"); cargo has not
// stabilised any other value, so hard-coding it via the const is intentional.
func metadataArgs(extra []string) []string {
	args := []string{"metadata", "--format-version", cargoMetadataFormatVersion}
	return append(args, extra...)
}

// countRegistryNodes returns how many resolve nodes the collector should produce for
// the given includeDev setting: registry-sourced, not workspace members, and passing the
// same dep-kind inclusion filter as collectDependenciesFromMeta. Applying that filter
// keeps the reconciliation count aligned with collection (e.g. registry-sourced
// dev-dependencies are excluded when includeDev is false), so the mismatch warning only
// fires on genuine dependency loss rather than on every project that has dev-dependencies.
func countRegistryNodes(meta *CargoMetadata, includeDev bool) int {
	// One pass over WorkspaceMembers seeds both maps: `workspace` is the exclusion set (workspace
	// members are not counted in the registry-node total), and `roots` is the seed for the
	// kind-aware reachability walk. Splitting them into two ranges is equivalent but wasted work.
	workspace := make(map[string]bool)
	roots := make(map[string]bool)
	for _, id := range meta.WorkspaceMembers {
		workspace[id] = true
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
		if !strings.HasPrefix(source, cargoRegistrySourcePrefix) {
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
	if strings.HasPrefix(source, cargoRegistrySourcePrefix) {
		return name + "-" + version + crateFileSuffix
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
// FULL CHAINS from the direct parent up to the workspace/root module id — the canonical
// build-info format shared by pip/pipenv/uv/maven ("every requestedBy path terminates at the
// module id"; see applyModuleOverride in commands/cargo/publish.go). Cycle-guarded by skipping
// any child whose label already appears in the growing chain; capped at
// entities.RequestedByMaxLength paths per dep.
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
		if !strings.HasPrefix(source, cargoRegistrySourcePrefix) {
			continue // skip git/path/local sources
		}
		scope, include := scopeForDepKinds(kindsById[node.Id], cf.config.IncludeDevDependencies)
		if !include {
			continue
		}
		key := name + "-" + version + crateFileSuffix
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

	// Second pass: walk the graph from each root in DFS and accumulate the full ancestor chain
	// (direct parent → ... → root module id). For every included registry dep encountered, record
	// the ancestor chain as one of its RequestedBy paths. This matches pip/pipenv/uv/maven's
	// canonical format ("every requestedBy path terminates at the module id"), which the UI's
	// impact analysis relies on to trace a transitive dep back to a direct dep declared in
	// Cargo.toml.
	nodesById := make(map[string]CargoNode, len(cf.meta.Resolve.Nodes))
	for _, n := range cf.meta.Resolve.Nodes {
		nodesById[n.Id] = n
	}
	for rootId := range rootIds {
		rootLabel := cf.nodeLabel(rootId, workspace)
		cf.walkRequestedByChains(rootId, []string{rootLabel}, nodesById, byKey, nodeKey, workspace, includeDev)
	}

	deps := make([]entities.Dependency, 0, len(order))
	for _, key := range order {
		deps = append(deps, byKey[key])
	}
	return deps
}

// walkRequestedByChains DFS-visits nodeId's children and, for each included registry child,
// appends `chain` (a copy) to the child's RequestedBy — then recurses into the child with the
// child's own label prepended. `chain` on entry represents the requestedBy path that any DIRECT
// child of nodeId should carry: chain[0] is the direct parent (== nodeId's label) and the last
// element is the root module id.
//
// Guards:
//   - Cycle: a child whose label already appears in `chain` is skipped. This subsumes the old
//     self-edge case (a proc-macro that dev-depends on itself would have parentLabel==childLabel,
//     so childLabel would already be at chain[0] and the child would be skipped).
//   - Cap: once a child's RequestedBy reaches entities.RequestedByMaxLength, no more chains are
//     appended for it (still recurse so downstream deps see this chain via their own count).
//   - Dedup by DIRECT PARENT: only ONE chain per unique direct parent (chain[0]) is recorded.
//     Cargo graphs are dense DAGs — a shared transitive like proc-macro2 has three direct
//     parents (serde_derive, quote, syn), and each of those is reachable via multiple middle
//     paths (e.g. serde_derive can be reached via serde_json→serde→serde_core or the shorter
//     serde_json→serde_core). Enumerating every path produced 8+ chains per dep, all just
//     different middle-paths under the same direct parent — noise the UI cannot present
//     meaningfully. Keeping the first-discovered chain per direct parent preserves the
//     "which of my direct deps introduced this transitive?" answer (the load-bearing UI use)
//     while collapsing the redundant middle-path variants.
func (cf *CargoFlexPack) walkRequestedByChains(
	nodeId string,
	chain []string,
	nodes map[string]CargoNode,
	byKey map[string]entities.Dependency,
	nodeKey map[string]string,
	workspace map[string]bool,
	includeDev bool,
) {
	node, ok := nodes[nodeId]
	if !ok {
		return
	}
	for _, childId := range childEdges(node, includeDev) {
		childLabel := cf.nodeLabel(childId, workspace)
		if containsString(chain, childLabel) {
			continue // cycle (subsumes proc-macro self-edge)
		}
		if childKey, ok := nodeKey[childId]; ok {
			child := byKey[childKey]
			if len(child.RequestedBy) < entities.RequestedByMaxLength && !hasChainWithSameDirectParent(child.RequestedBy, chain) {
				pathCopy := append([]string(nil), chain...)
				child.RequestedBy = append(child.RequestedBy, pathCopy)
				byKey[childKey] = child
			}
		}
		// Recurse with childLabel prepended so downstream deps carry [descendantParent, ..., childLabel, chain...].
		next := make([]string, 0, len(chain)+1)
		next = append(next, childLabel)
		next = append(next, chain...)
		cf.walkRequestedByChains(childId, next, nodes, byKey, nodeKey, workspace, includeDev)
	}
}

// containsString reports whether s occurs in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// hasChainWithSameDirectParent reports whether paths already contains a chain whose direct
// parent (element [0]) equals path[0]. Different direct parents contribute separate chains;
// multiple middle-paths under the same direct parent collapse to the first-recorded chain.
// An empty candidate path never matches anything.
func hasChainWithSameDirectParent(paths [][]string, path []string) bool {
	if len(path) == 0 {
		return false
	}
	for _, p := range paths {
		if len(p) > 0 && p[0] == path[0] {
			return true
		}
	}
	return false
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
	// Cargo metadata is REQUIRED — it is the only source of the resolved dep graph, dep_kinds and
	// workspace membership; without it we cannot build the module or dependencies. Both the exec
	// failure and the parse failure are surfaced up (Uday's PR #399 review comments) instead of
	// silently leaving cf.meta==nil and returning the generic wrapper below.
	out, err := cf.runCargoMetadata()
	if err != nil {
		return fmt.Errorf("cargo metadata in %s: %w", cf.config.WorkingDirectory, err)
	}
	meta, perr := parseMetadata(out)
	if perr != nil {
		return fmt.Errorf("cargo metadata in %s: %w", cf.config.WorkingDirectory, perr)
	}
	cf.meta = meta

	// Cargo.lock is OPTIONAL — cargo generates it on the first build, so a fresh checkout may not
	// have one yet, and we can still produce a build-info from cargo metadata alone (checksums come
	// from $CARGO_HOME/registry/cache/... or from Artifactory as a fallback). Two distinct cases:
	//   - File missing (os.IsNotExist): expected pre-first-build; skip silently.
	//   - File present but malformed: unexpected; log.Warn so the user sees it, but still continue
	//     — checksums degrade gracefully to the cache/Artifactory fallback path.
	lockPath := filepath.Join(cf.config.WorkingDirectory, "Cargo.lock")
	if lock, lerr := parseLockfile(lockPath); lerr == nil {
		cf.lockChecksums = lock
	} else if !os.IsNotExist(lerr) {
		log.Warn(fmt.Sprintf("cargo: Cargo.lock present but could not be parsed (%s); continuing without lockfile-sourced checksums", lerr.Error()))
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
