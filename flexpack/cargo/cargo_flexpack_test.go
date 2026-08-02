package cargo

import (
	"os"
	"testing"

	"github.com/jfrog/build-info-go/entities"
)

func TestCollectBuildInfoProducesCargoModule(t *testing.T) {
	data, _ := os.ReadFile("testdata/metadata.json")
	meta, _ := parseMetadata(data)
	lock, _ := parseLockfile("testdata/Cargo.lock")
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: lock, initialized: true}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	bi, err := cf.buildInfoFromState("my-build", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bi.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(bi.Modules))
	}
	if bi.Modules[0].Type != entities.Cargo {
		t.Errorf("module type = %q, want cargo", bi.Modules[0].Type)
	}
	if bi.Modules[0].Id != "root:0.1.0" {
		t.Errorf("module id = %q, want root:0.1.0", bi.Modules[0].Id)
	}
}

// TestBuildModulesPerWorkspaceMember verifies a virtual workspace with two members produces one
// module per member, each carrying only the dependencies that member pulls in (Maven/Gradle
// multi-module layout). app depends on serde (prod) + cc (build) + the lib member (path);
// lib depends on serde (prod). Modules are ordered by id.
func TestBuildModulesPerWorkspaceMember(t *testing.T) {
	app := "app 0.1.0 (path+file:///ws/app)"
	lib := "lib 0.1.0 (path+file:///ws/lib)"
	serde := "serde 1.0.0 (registry+x)"
	cc := "cc 1.0.0 (registry+x)"
	dep := func(name, pkg, kind string) CargoNodeDep {
		return CargoNodeDep{Name: name, Pkg: pkg, DepKinds: []CargoDepKind{{Kind: kind}}}
	}
	meta := &CargoMetadata{
		WorkspaceMembers: []string{app, lib},
		Resolve: CargoResolve{
			Root: "", // virtual workspace: no single root package
			Nodes: []CargoNode{
				{Id: app, Dependencies: []string{serde, cc, lib},
					Deps: []CargoNodeDep{dep("serde", serde, ""), dep("cc", cc, "build"), dep("lib", lib, "")}},
				{Id: lib, Dependencies: []string{serde}, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: serde, Dependencies: []string{}},
				{Id: cc, Dependencies: []string{}},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: map[string]string{}}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	bi, err := cf.buildInfoFromState("ws-build", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bi.Modules) != 2 {
		t.Fatalf("expected 2 modules (one per member), got %d: %+v", len(bi.Modules), bi.Modules)
	}
	// Sorted by id: app:0.1.0 then lib:0.1.0.
	if bi.Modules[0].Id != "app:0.1.0" || bi.Modules[1].Id != "lib:0.1.0" {
		t.Fatalf("module ids = [%q, %q], want [app:0.1.0, lib:0.1.0]", bi.Modules[0].Id, bi.Modules[1].Id)
	}
	scopesById := func(m entities.Module) map[string]string {
		out := map[string]string{}
		for _, d := range m.Dependencies {
			if len(d.Scopes) > 0 {
				out[d.Id] = d.Scopes[0]
			}
		}
		return out
	}
	appDeps := scopesById(bi.Modules[0])
	if len(appDeps) != 2 || appDeps["serde-1.0.0.crate"] != "normal" || appDeps["cc-1.0.0.crate"] != "build" {
		t.Errorf("app module deps = %v, want serde=normal, cc=build (2 total)", appDeps)
	}
	libDeps := scopesById(bi.Modules[1])
	if len(libDeps) != 1 || libDeps["serde-1.0.0.crate"] != "normal" {
		t.Errorf("lib module deps = %v, want serde=normal (1 total)", libDeps)
	}
	if bi.Modules[0].Type != entities.Cargo || bi.Modules[1].Type != entities.Cargo {
		t.Errorf("module types must be cargo")
	}
}

// TestBuildModulesRespectsSelectedPackages verifies that when the CLI narrows the compilation
// to specific -p packages, only those workspace members produce build-info modules — sibling
// members that were not compiled must not appear. This is the fix for the ripgrep/grep-index
// bug where `cargo build -p grep-pcre2 --features pcre2` listed grep-index (never compiled).
func TestBuildModulesRespectsSelectedPackages(t *testing.T) {
	app := "app 0.1.0 (path+file:///ws/app)"
	lib := "lib 0.1.0 (path+file:///ws/lib)"
	unused := "unused 0.1.0 (path+file:///ws/unused)"
	serde := "serde 1.0.0 (registry+x)"
	dep := func(name, pkg, kind string) CargoNodeDep {
		return CargoNodeDep{Name: name, Pkg: pkg, DepKinds: []CargoDepKind{{Kind: kind}}}
	}
	meta := &CargoMetadata{
		WorkspaceMembers: []string{app, lib, unused},
		Resolve: CargoResolve{
			Nodes: []CargoNode{
				{Id: app, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: lib, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: unused, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: serde},
			},
		},
	}
	cf := &CargoFlexPack{
		config:        CargoConfig{SelectedPackages: []string{"app"}},
		meta:          meta,
		lockChecksums: map[string]string{},
	}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	bi, err := cf.buildInfoFromState("ws-build", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bi.Modules) != 1 {
		t.Fatalf("expected 1 module (app only), got %d: %+v", len(bi.Modules), bi.Modules)
	}
	if bi.Modules[0].Id != "app:0.1.0" {
		t.Errorf("module id = %q, want app:0.1.0", bi.Modules[0].Id)
	}
}

// TestBuildModulesUsesWorkspaceDefaultMembers verifies that when the CLI doesn't specify -p and
// cargo (>=1.71) exposes workspace_default_members, only default members produce modules —
// respecting [workspace.default-members] in Cargo.toml. Older cargo lacks the field, in which
// case buildModules falls back to all workspace members (covered by
// TestBuildModulesPerWorkspaceMember above).
func TestBuildModulesUsesWorkspaceDefaultMembers(t *testing.T) {
	app := "app 0.1.0 (path+file:///ws/app)"
	lib := "lib 0.1.0 (path+file:///ws/lib)"
	skipped := "skipped 0.1.0 (path+file:///ws/skipped)"
	serde := "serde 1.0.0 (registry+x)"
	dep := func(name, pkg, kind string) CargoNodeDep {
		return CargoNodeDep{Name: name, Pkg: pkg, DepKinds: []CargoDepKind{{Kind: kind}}}
	}
	meta := &CargoMetadata{
		WorkspaceMembers:        []string{app, lib, skipped},
		WorkspaceDefaultMembers: []string{app, lib}, // skipped not in default set
		Resolve: CargoResolve{
			Nodes: []CargoNode{
				{Id: app, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: lib, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: skipped, Deps: []CargoNodeDep{dep("serde", serde, "")}},
				{Id: serde},
			},
		},
	}
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: map[string]string{}}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	bi, err := cf.buildInfoFromState("ws-build", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bi.Modules) != 2 {
		t.Fatalf("expected 2 modules (app + lib, skipped omitted), got %d: %+v", len(bi.Modules), bi.Modules)
	}
	ids := []string{bi.Modules[0].Id, bi.Modules[1].Id}
	if ids[0] != "app:0.1.0" || ids[1] != "lib:0.1.0" {
		t.Errorf("module ids = %v, want [app:0.1.0 lib:0.1.0]", ids)
	}
}
