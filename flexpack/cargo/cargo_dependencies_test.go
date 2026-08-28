package cargo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jfrog/build-info-go/entities"
)

func TestParsePackageId(t *testing.T) {
	cases := []struct {
		id, wantName, wantVer, wantSrc string
	}{
		// pre-1.77 opaque form
		{"serde 1.0.197 (registry+https://github.com/rust-lang/crates.io-index)", "serde", "1.0.197", "registry+https://github.com/rust-lang/crates.io-index"},
		// >=1.77 PackageIdSpec form
		{"registry+https://github.com/rust-lang/crates.io-index#serde@1.0.197", "serde", "1.0.197", "registry+https://github.com/rust-lang/crates.io-index"},
		// PackageIdSpec without explicit name (name is last path segment / before @)
		{"path+file:///work/mycrate#0.1.0", "mycrate", "0.1.0", "path+file:///work/mycrate"},
		// PackageIdSpec local with name@version
		{"path+file:///work#mycrate@0.1.0", "mycrate", "0.1.0", "path+file:///work"},
	}
	for _, c := range cases {
		name, ver, src := parsePackageId(c.id)
		if name != c.wantName || ver != c.wantVer || src != c.wantSrc {
			t.Errorf("parsePackageId(%q) = (%q,%q,%q), want (%q,%q,%q)",
				c.id, name, ver, src, c.wantName, c.wantVer, c.wantSrc)
		}
	}
}

func TestScopeForDepKinds(t *testing.T) {
	// Cargo's dep_kind names are surfaced verbatim: "" -> "normal", "build" -> "build",
	// "dev" -> "dev". No synthesized labels like "prod" or "transitive".
	normal, inc := scopeForDepKinds([]CargoDepKind{{Kind: ""}}, false)
	if !inc || normal != "normal" {
		t.Errorf("normal dep: got (%q,%v), want (normal,true)", normal, inc)
	}
	build, inc := scopeForDepKinds([]CargoDepKind{{Kind: "build"}}, false)
	if !inc || build != "build" {
		t.Errorf("build dep: got (%q,%v), want (build,true)", build, inc)
	}
	// dev dep excluded when includeDev=false
	if _, inc := scopeForDepKinds([]CargoDepKind{{Kind: "dev"}}, false); inc {
		t.Error("dev dep should be excluded when includeDev=false")
	}
	dev, inc := scopeForDepKinds([]CargoDepKind{{Kind: "dev"}}, true)
	if !inc || dev != "dev" {
		t.Errorf("dev dep includeDev: got (%q,%v), want (dev,true)", dev, inc)
	}
}

func TestBuildRequestedBy(t *testing.T) {
	meta := &CargoMetadata{
		Resolve: CargoResolve{
			Root: "root 0.1.0 (path+file:///r)",
			Nodes: []CargoNode{
				{Id: "root 0.1.0 (path+file:///r)", Dependencies: []string{"a 1.0.0 (registry+x)"}},
				{Id: "a 1.0.0 (registry+x)", Dependencies: []string{"b 2.0.0 (registry+x)"}},
				{Id: "b 2.0.0 (registry+x)"},
			},
		},
	}
	rb := buildRequestedBy(meta)
	if len(rb["a 1.0.0 (registry+x)"]) != 1 || rb["a 1.0.0 (registry+x)"][0] != "root 0.1.0 (path+file:///r)" {
		t.Errorf("a should be requested by root, got %v", rb["a 1.0.0 (registry+x)"])
	}
	if len(rb["b 2.0.0 (registry+x)"]) != 1 || rb["b 2.0.0 (registry+x)"][0] != "a 1.0.0 (registry+x)" {
		t.Errorf("b should be requested by a, got %v", rb["b 2.0.0 (registry+x)"])
	}
}

func TestFindCachedCrate(t *testing.T) {
	home := t.TempDir()
	cacheDir := filepath.Join(home, "registry", "cache", "index.crates.io-abc123")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cratePath := filepath.Join(cacheDir, "serde-1.0.197.crate")
	if err := os.WriteFile(cratePath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := findCachedCrate(home, "serde", "1.0.197")
	if got != cratePath {
		t.Errorf("findCachedCrate = %q, want %q", got, cratePath)
	}
	if findCachedCrate(home, "missing", "9.9.9") != "" {
		t.Error("expected empty path for missing crate")
	}
}

func TestResolveChecksumFallsBackToLockfile(t *testing.T) {
	cf := &CargoFlexPack{config: CargoConfig{}}
	// no cached file; cargoHome points at empty temp dir
	t.Setenv("CARGO_HOME", t.TempDir())
	cs := cf.resolveChecksum("missing", "9.9.9", "deadbeefsha256")
	if cs.Sha256 != "deadbeefsha256" || cs.Sha1 != "" || cs.Md5 != "" {
		t.Errorf("expected lockfile sha256 fallback, got %+v", cs)
	}
}

func TestParseMetadataExtractsRegistryDepsOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Packages) != 2 || meta.Resolve.Root == "" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestParseLockfile(t *testing.T) {
	m, err := parseLockfile("testdata/Cargo.lock")
	if err != nil {
		t.Fatal(err)
	}
	if m["serde|1.0.197"] != "3fb1c873e1b9b056a4dc4c0c198b24c3ffa059243875552b2bd0933b1aee4ce2" {
		t.Errorf("lockfile sha256 mismatch: %v", m)
	}
}

func TestMetadataArgs(t *testing.T) {
	cases := []struct {
		extra []string
		want  []string
	}{
		{nil, []string{"metadata", "--format-version", "1"}},
		{[]string{"--all-features"}, []string{"metadata", "--format-version", "1", "--all-features"}},
		{[]string{"--features", "a,b", "--locked"}, []string{"metadata", "--format-version", "1", "--features", "a,b", "--locked"}},
	}
	for _, c := range cases {
		got := metadataArgs(c.extra)
		if len(got) != len(c.want) {
			t.Errorf("metadataArgs(%v) length = %d, want %d", c.extra, len(got), len(c.want))
			continue
		}
		for i, v := range got {
			if v != c.want[i] {
				t.Errorf("metadataArgs(%v)[%d] = %q, want %q", c.extra, i, v, c.want[i])
			}
		}
	}
}

func TestCountRegistryNodes(t *testing.T) {
	data, err := os.ReadFile("testdata/metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	got := countRegistryNodes(meta, false)
	if got != 1 {
		t.Errorf("countRegistryNodes(meta, false) = %d, want 1", got)
	}
}

// TestCountRegistryNodesExcludesDevDeps ensures the reconciliation count applies the same
// dev-dependency filter as collection: a registry-sourced dev-only dependency is counted
// only when includeDev is true, so the mismatch warning does not fire spuriously.
func TestCountRegistryNodesExcludesDevDeps(t *testing.T) {
	root := "root 0.1.0 (path+file:///r)"
	prod := "serde 1.0.0 (registry+x)"
	dev := "mockall 0.11.0 (registry+x)"
	meta := &CargoMetadata{
		WorkspaceMembers: []string{root},
		Resolve: CargoResolve{
			Root: root,
			Nodes: []CargoNode{
				{
					Id: root,
					Deps: []CargoNodeDep{
						{Name: "serde", Pkg: prod, DepKinds: []CargoDepKind{{Kind: ""}}},
						{Name: "mockall", Pkg: dev, DepKinds: []CargoDepKind{{Kind: "dev"}}},
					},
				},
				{Id: prod},
				{Id: dev},
			},
		},
	}
	if got := countRegistryNodes(meta, false); got != 1 {
		t.Errorf("countRegistryNodes(meta, false) = %d, want 1 (dev dep excluded)", got)
	}
	if got := countRegistryNodes(meta, true); got != 2 {
		t.Errorf("countRegistryNodes(meta, true) = %d, want 2 (dev dep included)", got)
	}
}

// TestDevTransitiveDepsExcluded guards the fix for the dev-dependency transitive leak: a crate
// reachable ONLY through a dev-dependency (rand -> rand_core, where rand is a dev-dep of root) must
// not appear in build-info when IncludeDevDependencies is false, even though the rand->rand_core
// edge itself is a normal edge.
func TestDevTransitiveDepsExcluded(t *testing.T) {
	root := "root 0.1.0 (path+file:///r)"
	serde := "serde 1.0.0 (registry+x)"
	rand := "rand 0.8.0 (registry+x)"
	randCore := "rand_core 0.6.0 (registry+x)"
	meta := &CargoMetadata{
		WorkspaceMembers: []string{root},
		Resolve: CargoResolve{
			Root: root,
			Nodes: []CargoNode{
				{
					Id:           root,
					Dependencies: []string{serde, rand},
					Deps: []CargoNodeDep{
						{Name: "serde", Pkg: serde, DepKinds: []CargoDepKind{{Kind: ""}}},
						{Name: "rand", Pkg: rand, DepKinds: []CargoDepKind{{Kind: "dev"}}},
					},
				},
				{
					Id:           rand,
					Dependencies: []string{randCore},
					Deps:         []CargoNodeDep{{Name: "rand_core", Pkg: randCore, DepKinds: []CargoDepKind{{Kind: ""}}}},
				},
				{Id: serde},
				{Id: randCore},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{IncludeDevDependencies: false}, meta: meta}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range cf.dependencies {
		got[d.Id] = true
	}
	if !got["serde-1.0.0.crate"] {
		t.Errorf("serde should be included, got %v", got)
	}
	if got["rand-0.8.0.crate"] {
		t.Errorf("rand is a dev-dependency and must be excluded, got %v", got)
	}
	if got["rand_core-0.6.0.crate"] {
		t.Errorf("rand_core is reachable only via the dev-dep rand and must be excluded, got %v", got)
	}
	if len(cf.dependencies) != 1 {
		t.Errorf("expected exactly 1 dependency (serde), got %d: %v", len(cf.dependencies), got)
	}
	// Reconciliation count must match the collected count (no spurious mismatch warning).
	if n := countRegistryNodes(meta, false); n != 1 {
		t.Errorf("countRegistryNodes(false) = %d, want 1", n)
	}
	// With dev included, all three registry crates appear.
	cfDev := &CargoFlexPack{config: CargoConfig{IncludeDevDependencies: true}, meta: meta}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cfDev.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	if len(cfDev.dependencies) != 3 {
		t.Errorf("with dev included expected 3 deps, got %d", len(cfDev.dependencies))
	}
}

func TestCollectDependenciesSkipsWorkspaceAndLocal(t *testing.T) {
	data, _ := os.ReadFile("testdata/metadata.json")
	meta, _ := parseMetadata(data)
	lock, _ := parseLockfile("testdata/Cargo.lock")
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: lock}
	t.Setenv("CARGO_HOME", t.TempDir()) // force lockfile fallback
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	// only serde is a registry dep; root is workspace-local and skipped
	if len(cf.dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d: %+v", len(cf.dependencies), cf.dependencies)
	}
	dep := cf.dependencies[0]
	if dep.Id != "serde-1.0.197.crate" {
		t.Errorf("dep id = %q, want serde-1.0.197.crate", dep.Id)
	}
	if dep.Sha256 == "" {
		t.Error("expected sha256 from lockfile")
	}
	if len(dep.Scopes) != 1 || dep.Scopes[0] != "normal" {
		t.Errorf("scopes = %v, want [normal]", dep.Scopes)
	}
}

func TestNormalDepScope(t *testing.T) {
	// Both direct and indirect normal deps carry cargo's own dep-kind name ("normal") — no
	// synthesized "transitive" marker, matching what the user sees in Cargo.toml and cargo tree.
	// RequestedBy lists only the direct parent as a single-element path, no chain to root.
	meta := &CargoMetadata{
		WorkspaceMembers: []string{"root 0.1.0 (path+file:///r)"},
		Resolve: CargoResolve{
			Root: "root 0.1.0 (path+file:///r)",
			Nodes: []CargoNode{
				{
					Id:           "root 0.1.0 (path+file:///r)",
					Dependencies: []string{"a 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{
						{
							Name:     "a",
							Pkg:      "a 1.0.0 (registry+x)",
							DepKinds: []CargoDepKind{{Kind: ""}},
						},
					},
				},
				{
					Id:           "a 1.0.0 (registry+x)",
					Dependencies: []string{"b 2.0.0 (registry+x)"},
					Deps: []CargoNodeDep{
						{
							Name:     "b",
							Pkg:      "b 2.0.0 (registry+x)",
							DepKinds: []CargoDepKind{{Kind: ""}},
						},
					},
				},
				{
					Id:           "b 2.0.0 (registry+x)",
					Dependencies: []string{},
				},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: map[string]string{}}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	if len(cf.dependencies) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(cf.dependencies), cf.dependencies)
	}

	// Find deps by id
	depById := make(map[string]*entities.Dependency)
	for i := range cf.dependencies {
		depById[cf.dependencies[i].Id] = &cf.dependencies[i]
	}

	// Both a (direct) and b (indirect) carry cargo's own kind "normal".
	a, ok := depById["a-1.0.0.crate"]
	if !ok {
		t.Fatalf("missing a-1.0.0.crate in dependencies")
	}
	if len(a.Scopes) != 1 || a.Scopes[0] != "normal" {
		t.Errorf("a scopes = %v, want [normal]", a.Scopes)
	}

	b, ok := depById["b-2.0.0.crate"]
	if !ok {
		t.Fatalf("missing b-2.0.0.crate in dependencies")
	}
	if len(b.Scopes) != 1 || b.Scopes[0] != "normal" {
		t.Errorf("b scopes = %v, want [normal]", b.Scopes)
	}

	// RequestedBy records a FULL CHAIN from direct parent up to the root module id — the
	// canonical build-info format (pip/pipenv/uv/maven).
	//   a  <- [[root:0.1.0]]                       (root is a's direct parent)
	//   b  <- [[a-1.0.0.crate, root:0.1.0]]        (a is b's direct parent; chain ends at root)
	wantA := [][]string{{"root:0.1.0"}}
	if !reflect.DeepEqual(a.RequestedBy, wantA) {
		t.Errorf("a.RequestedBy = %v, want %v", a.RequestedBy, wantA)
	}
	wantB := [][]string{{"a-1.0.0.crate", "root:0.1.0"}}
	if !reflect.DeepEqual(b.RequestedBy, wantB) {
		t.Errorf("b.RequestedBy = %v, want %v", b.RequestedBy, wantB)
	}
}

// TestRequestedByDiamondPaths verifies multiple paths to a shared transitive dependency.
// Graph: root -> a, root -> b, a -> d, b -> d. d should carry both paths to root.
func TestRequestedByDiamondPaths(t *testing.T) {
	dep := func(name, pkg string) CargoNodeDep {
		return CargoNodeDep{Name: name, Pkg: pkg, DepKinds: []CargoDepKind{{Kind: ""}}}
	}
	meta := &CargoMetadata{
		WorkspaceMembers: []string{"root 0.1.0 (path+file:///r)"},
		Resolve: CargoResolve{
			Root: "root 0.1.0 (path+file:///r)",
			Nodes: []CargoNode{
				{Id: "root 0.1.0 (path+file:///r)", Dependencies: []string{"a 1.0.0 (registry+x)", "b 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{dep("a", "a 1.0.0 (registry+x)"), dep("b", "b 1.0.0 (registry+x)")}},
				{Id: "a 1.0.0 (registry+x)", Dependencies: []string{"d 1.0.0 (registry+x)"}, Deps: []CargoNodeDep{dep("d", "d 1.0.0 (registry+x)")}},
				{Id: "b 1.0.0 (registry+x)", Dependencies: []string{"d 1.0.0 (registry+x)"}, Deps: []CargoNodeDep{dep("d", "d 1.0.0 (registry+x)")}},
				{Id: "d 1.0.0 (registry+x)", Dependencies: []string{}},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: map[string]string{}}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	var d *entities.Dependency
	for i := range cf.dependencies {
		if cf.dependencies[i].Id == "d-1.0.0.crate" {
			d = &cf.dependencies[i]
		}
	}
	if d == nil {
		t.Fatal("missing d-1.0.0.crate")
	}
	// Two full chains, one per direct requester (a and b), each terminating at the root module id.
	// Chain order follows DFS visit order (root -> a first, then root -> b), so a's chain comes first.
	want := [][]string{
		{"a-1.0.0.crate", "root:0.1.0"},
		{"b-1.0.0.crate", "root:0.1.0"},
	}
	if !reflect.DeepEqual(d.RequestedBy, want) {
		t.Errorf("d.RequestedBy = %v, want %v", d.RequestedBy, want)
	}
}

// TestRequestedByCollapsesMiddlePathVariants verifies that when a dep has a single direct
// parent reachable from the root via MULTIPLE distinct middle paths (dense DAG — the real-world
// cargo scenario where a shared transitive like proc-macro2 shows up through many overlapping
// diamond routes), the output records ONE chain per unique direct parent — not one per
// full-DAG path.
//
// Graph: root -> X -> A -> leaf, root -> Y -> A -> leaf.
//   - A is leaf's ONLY direct parent
//   - A is reachable from root via TWO distinct middle nodes (X and Y)
//   - naive enumeration would produce 2 chains for leaf: [A, X, root] and [A, Y, root] — both
//     have the same direct parent A and differ only in the middle
//   - expected: 1 chain (whichever middle path DFS discovered first)
func TestRequestedByCollapsesMiddlePathVariants(t *testing.T) {
	dep := func(name, pkg string) CargoNodeDep {
		return CargoNodeDep{Name: name, Pkg: pkg, DepKinds: []CargoDepKind{{Kind: ""}}}
	}
	meta := &CargoMetadata{
		WorkspaceMembers: []string{"root 0.1.0 (path+file:///r)"},
		Resolve: CargoResolve{
			Root: "root 0.1.0 (path+file:///r)",
			Nodes: []CargoNode{
				{Id: "root 0.1.0 (path+file:///r)",
					Dependencies: []string{"x 1.0.0 (registry+x)", "y 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{
						dep("x", "x 1.0.0 (registry+x)"),
						dep("y", "y 1.0.0 (registry+x)"),
					}},
				{Id: "x 1.0.0 (registry+x)",
					Dependencies: []string{"a 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{dep("a", "a 1.0.0 (registry+x)")}},
				{Id: "y 1.0.0 (registry+x)",
					Dependencies: []string{"a 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{dep("a", "a 1.0.0 (registry+x)")}},
				{Id: "a 1.0.0 (registry+x)",
					Dependencies: []string{"leaf 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{dep("leaf", "leaf 1.0.0 (registry+x)")}},
				{Id: "leaf 1.0.0 (registry+x)", Dependencies: []string{}},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: map[string]string{}}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	var leaf *entities.Dependency
	for i := range cf.dependencies {
		if cf.dependencies[i].Id == "leaf-1.0.0.crate" {
			leaf = &cf.dependencies[i]
		}
	}
	if leaf == nil {
		t.Fatal("missing leaf-1.0.0.crate")
	}
	// Exactly ONE chain because there's one unique direct parent (a).
	if len(leaf.RequestedBy) != 1 {
		t.Errorf("leaf should have 1 chain (single direct parent), got %d: %v", len(leaf.RequestedBy), leaf.RequestedBy)
	}
	if len(leaf.RequestedBy) > 0 {
		p := leaf.RequestedBy[0]
		if len(p) == 0 || p[0] != "a-1.0.0.crate" {
			t.Errorf("chain direct parent should be a-1.0.0.crate, got %v", p)
		}
		if len(p) == 0 || p[len(p)-1] != "root:0.1.0" {
			t.Errorf("chain should terminate at root:0.1.0, got %v", p)
		}
	}
}

// TestRequestedByDeduplication verifies that when cargo's resolve graph would produce the exact
// same requestedBy chain twice for the same dep (e.g. the same parent listed twice on a node
// after cargo's own merge, which can happen with rename/crate + rename/optional edges), the
// deduplicated result carries ONE entry — not two identical ones. Uday's review comment on
// PR #399 asked for this coverage.
func TestRequestedByDeduplication(t *testing.T) {
	dep := func(name, pkg string) CargoNodeDep {
		return CargoNodeDep{Name: name, Pkg: pkg, DepKinds: []CargoDepKind{{Kind: ""}}}
	}
	// root -> a (listed twice via dep_kinds merging or duplicate resolve edges); a -> b
	meta := &CargoMetadata{
		WorkspaceMembers: []string{"root 0.1.0 (path+file:///r)"},
		Resolve: CargoResolve{
			Root: "root 0.1.0 (path+file:///r)",
			Nodes: []CargoNode{
				{Id: "root 0.1.0 (path+file:///r)",
					Dependencies: []string{"a 1.0.0 (registry+x)", "a 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{
						dep("a", "a 1.0.0 (registry+x)"),
						dep("a", "a 1.0.0 (registry+x)"),
					}},
				{Id: "a 1.0.0 (registry+x)", Dependencies: []string{"b 1.0.0 (registry+x)"},
					Deps: []CargoNodeDep{dep("b", "b 1.0.0 (registry+x)")}},
				{Id: "b 1.0.0 (registry+x)", Dependencies: []string{}},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: map[string]string{}}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	var a *entities.Dependency
	for i := range cf.dependencies {
		if cf.dependencies[i].Id == "a-1.0.0.crate" {
			a = &cf.dependencies[i]
		}
	}
	if a == nil {
		t.Fatal("missing a-1.0.0.crate")
	}
	// Even though root's edge list contains a TWICE, dedup collapses to a single chain entry.
	want := [][]string{{"root:0.1.0"}}
	if !reflect.DeepEqual(a.RequestedBy, want) {
		t.Errorf("a.RequestedBy = %v, want %v (dedup should collapse duplicate chains)", a.RequestedBy, want)
	}
}
