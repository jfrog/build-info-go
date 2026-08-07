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
	if dep.Checksum.Sha256 == "" {
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

	// RequestedBy records only the direct parent, one single-element path per parent — same
	// convention as pnpm. No chains to root; the module id does not appear at the end.
	//   a  <- [[root:0.1.0]]        (root is a's direct parent, so it appears once)
	//   b  <- [[a-1.0.0.crate]]     (a is b's direct parent; root does NOT appear)
	wantA := [][]string{{"root:0.1.0"}}
	if !reflect.DeepEqual(a.RequestedBy, wantA) {
		t.Errorf("a.RequestedBy = %v, want %v", a.RequestedBy, wantA)
	}
	wantB := [][]string{{"a-1.0.0.crate"}}
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
	// Two direct-parent paths, one per direct requester (a and b). No root at the end.
	want := [][]string{{"a-1.0.0.crate"}, {"b-1.0.0.crate"}}
	if !reflect.DeepEqual(d.RequestedBy, want) {
		t.Errorf("d.RequestedBy = %v, want %v", d.RequestedBy, want)
	}
}
