package cargo

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

// CargoFlexPack implements FlexPack build-info collection for Cargo.
type CargoFlexPack struct {
	config        CargoConfig
	dependencies  []entities.Dependency
	meta          *CargoMetadata
	lockChecksums map[string]string // name|version -> sha256
	initialized   bool
}

func NewCargoFlexPack(config CargoConfig) (*CargoFlexPack, error) {
	cf := &CargoFlexPack{config: config, lockChecksums: map[string]string{}}
	if cf.config.CargoExecutable == "" {
		if p, err := exec.LookPath("cargo"); err == nil {
			cf.config.CargoExecutable = p
		} else {
			log.Warn("cargo executable not found in PATH, using 'cargo'")
			cf.config.CargoExecutable = "cargo"
		}
	}
	return cf, nil
}

func (cf *CargoFlexPack) ensureInitialized() error {
	if cf.initialized {
		return nil
	}
	if err := cf.collectDependencies(); err != nil {
		return err
	}
	cf.initialized = true
	return nil
}

func (cf *CargoFlexPack) getProjectId() string {
	if cf.meta == nil || cf.meta.Resolve.Root == "" {
		return "cargo-project"
	}
	return moduleIdForMember(cf.meta.Resolve.Root)
}

// moduleIdForMember formats a workspace-member package id as the build-info module id
// "name:version" (or just "name" when the version is unknown).
func moduleIdForMember(memberPkgId string) string {
	name, version, _ := parsePackageId(memberPkgId)
	if version == "" {
		return name
	}
	return name + ":" + version
}

// buildModules assembles one build-info Module per workspace member that is actually part of
// this build — each carrying only the dependencies that member pulls in (via depsForRoots) —
// mirroring the Maven/Gradle multi-module layout. Selection precedence:
//  1. cf.config.SelectedPackages (populated from the user's cargo -p/--package flags) — only
//     those members are emitted, so `cargo build -p X --features Y` no longer lists sibling
//     members that were not compiled.
//  2. cargo metadata's workspace_default_members (cargo >= 1.71), which respects the crate's
//     own [workspace.default-members] setting — same crates cargo would build by default.
//  3. All workspace members (older cargo / no signal available).
//
// When metadata exposes no members at all, it falls back to one module keyed by the resolve root
// (or "cargo-project").
func (cf *CargoFlexPack) buildModules() []entities.Module {
	if cf.meta == nil || len(cf.meta.WorkspaceMembers) == 0 {
		return []entities.Module{{
			Id:           cf.getProjectId(),
			Type:         entities.Cargo,
			Dependencies: cf.dependencies,
		}}
	}
	members := cf.compiledMembers()
	sort.Strings(members) // stable module ordering across runs
	modules := make([]entities.Module, 0, len(members))
	for _, memberId := range members {
		modules = append(modules, entities.Module{
			Id:           moduleIdForMember(memberId),
			Type:         entities.Cargo,
			Dependencies: cf.depsForRoots(map[string]bool{memberId: true}),
		})
	}
	return modules
}

// compiledMembers returns the workspace-member package ids the current build actually compiles.
// Precedence matches buildModules' docstring. If SelectedPackages names something that isn't a
// workspace member (e.g. user typo), it is silently dropped rather than returned as a phantom
// module.
func (cf *CargoFlexPack) compiledMembers() []string {
	memberById := make(map[string]string, len(cf.meta.WorkspaceMembers))
	for _, id := range cf.meta.WorkspaceMembers {
		name, _, _ := parsePackageId(id)
		memberById[name] = id
	}
	if len(cf.config.SelectedPackages) > 0 {
		out := make([]string, 0, len(cf.config.SelectedPackages))
		for _, name := range cf.config.SelectedPackages {
			if id, ok := memberById[name]; ok {
				out = append(out, id)
			} else {
				log.Debug("cargo: -p " + name + " does not match any workspace member; skipping")
			}
		}
		if len(out) > 0 {
			return out
		}
		// Fall through: none matched — better to emit all members than an empty build-info.
	}
	if len(cf.meta.WorkspaceDefaultMembers) > 0 {
		return append([]string(nil), cf.meta.WorkspaceDefaultMembers...)
	}
	return append([]string(nil), cf.meta.WorkspaceMembers...)
}

// buildInfoFromState assembles BuildInfo from already-collected metadata, one module per member.
func (cf *CargoFlexPack) buildInfoFromState(buildName, buildNumber string) (*entities.BuildInfo, error) {
	bi := &entities.BuildInfo{
		Name:       buildName,
		Number:     buildNumber,
		Started:    time.Now().Format(entities.TimeFormat),
		Agent:      &entities.Agent{Name: "build-info-go", Version: "1.0.0"},
		BuildAgent: &entities.Agent{Name: "Cargo", Version: cf.cargoVersion()},
		Modules:    cf.buildModules(),
	}
	return bi, nil
}

func (cf *CargoFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	if err := cf.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("cargo build-info: %w", err)
	}
	return cf.buildInfoFromState(buildName, buildNumber)
}

func (cf *CargoFlexPack) cargoVersion() string {
	out, err := exec.Command(cf.config.CargoExecutable, "--version").Output()
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(out)) // "cargo 1.78.0 (...)"
	if len(fields) >= 2 {
		return fields[1]
	}
	return "unknown"
}

// GetProjectDependencies returns dependencies as flexpack.DependencyInfo.
func (cf *CargoFlexPack) GetProjectDependencies() ([]flexpack.DependencyInfo, error) {
	if err := cf.ensureInitialized(); err != nil {
		return nil, err
	}
	out := make([]flexpack.DependencyInfo, 0, len(cf.dependencies))
	for _, d := range cf.dependencies {
		out = append(out, flexpack.DependencyInfo{
			ID:     d.Id,
			Type:   "crate",
			SHA1:   d.Sha1,
			SHA256: d.Sha256,
			MD5:    d.Md5,
			Scopes: d.Scopes,
		})
	}
	return out, nil
}

func (cf *CargoFlexPack) GetDependencyGraph() (map[string][]string, error) {
	if err := cf.ensureInitialized(); err != nil {
		return nil, err
	}
	if cf.meta == nil {
		return nil, nil
	}
	graph := make(map[string][]string)
	for _, node := range cf.meta.Resolve.Nodes {
		graph[node.Id] = append([]string(nil), node.Dependencies...)
	}
	return graph, nil
}

// FlexPackManager interface — minimal implementations (collection happens via CollectBuildInfo).
func (cf *CargoFlexPack) GetDependency() string { return cf.getProjectId() }

// Note: FlexPackManager methods return no error; an init failure yields empty results.
func (cf *CargoFlexPack) ParseDependencyToList() []string {
	_ = cf.ensureInitialized()
	out := make([]string, 0, len(cf.dependencies))
	for _, d := range cf.dependencies {
		out = append(out, d.Id)
	}
	return out
}

func (cf *CargoFlexPack) CalculateChecksum() []map[string]interface{} {
	_ = cf.ensureInitialized()
	out := make([]map[string]interface{}, 0, len(cf.dependencies))
	for _, d := range cf.dependencies {
		out = append(out, map[string]interface{}{
			"id": d.Id, "sha1": d.Sha1, "sha256": d.Sha256, "md5": d.Md5,
		})
	}
	return out
}

func (cf *CargoFlexPack) CalculateScopes() []string {
	_ = cf.ensureInitialized()
	seen := map[string]bool{}
	var out []string
	for _, d := range cf.dependencies {
		for _, s := range d.Scopes {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func (cf *CargoFlexPack) CalculateRequestedBy() map[string][]string {
	_ = cf.ensureInitialized()
	if cf.meta == nil {
		return nil
	}
	return buildRequestedBy(cf.meta)
}

// Compile-time interface assertions.
var _ flexpack.FlexPackManager = (*CargoFlexPack)(nil)
var _ flexpack.BuildInfoCollector = (*CargoFlexPack)(nil)
