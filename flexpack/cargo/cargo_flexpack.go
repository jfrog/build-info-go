package cargo

import (
	"fmt"
	"os/exec"
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
	name, version, _ := parsePackageId(cf.meta.Resolve.Root)
	if version == "" {
		return name
	}
	return name + ":" + version
}

// buildInfoFromState assembles BuildInfo from already-collected dependencies.
func (cf *CargoFlexPack) buildInfoFromState(buildName, buildNumber string) (*entities.BuildInfo, error) {
	bi := &entities.BuildInfo{
		Name:       buildName,
		Number:     buildNumber,
		Started:    time.Now().Format(entities.TimeFormat),
		Agent:      &entities.Agent{Name: "build-info-go", Version: "1.0.0"},
		BuildAgent: &entities.Agent{Name: "Cargo", Version: cf.cargoVersion()},
		Modules: []entities.Module{{
			Id:           cf.getProjectId(),
			Type:         entities.Cargo,
			Dependencies: cf.dependencies,
		}},
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
			SHA1:   d.Checksum.Sha1,
			SHA256: d.Checksum.Sha256,
			MD5:    d.Checksum.Md5,
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
			"id": d.Id, "sha1": d.Checksum.Sha1, "sha256": d.Checksum.Sha256, "md5": d.Checksum.Md5,
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
