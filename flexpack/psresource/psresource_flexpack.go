package psresource

import (
	"fmt"

	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
)

// nupkgType is the build-info dependency/artifact type used for PSResource (NuGet-based) packages.
const nupkgType = "nupkg"

// ResolvedPackage is a PSResource package whose identity and checksum have already been determined
// by the caller: name/version via Get-InstalledPSResource, checksum via a HEAD request to
// Artifactory. build-info-go performs neither of those - it only assembles the resulting data into
// entities.Dependency records, exactly like every other FlexPack collector in this repo.
//
// This is an alias for buildinfoflex.ResolvedPackage: the canonical definition lives in the parent
// flexpack package (which PSResourceConfig.ResolvedPackages needs), and this alias keeps existing
// references to psresource.ResolvedPackage working without a call-site change.
type ResolvedPackage = buildinfoflex.ResolvedPackage

// PSResourceFlexPack collects build-info for PowerShell PSResourceGet using the FlexPack native
// approach. It performs no I/O of its own: it turns the pre-resolved package data supplied via
// PSResourceConfig.ResolvedPackages (by the caller, jfrog-cli-artifactory) into entities.BuildInfo
// structures.
type PSResourceFlexPack struct {
	config buildinfoflex.PSResourceConfig
}

// compile-time assertion that PSResourceFlexPack satisfies the same interface every other FlexPack
// collector in this repo does.
var _ buildinfoflex.BuildInfoCollector = (*PSResourceFlexPack)(nil)

// NewPSResourceFlexPack creates a new PSResourceFlexPack with the given configuration.
func NewPSResourceFlexPack(config buildinfoflex.PSResourceConfig) (*PSResourceFlexPack, error) {
	if config.WorkingDirectory == "" {
		return nil, fmt.Errorf("PSResourceConfig.WorkingDirectory must not be empty")
	}
	return &PSResourceFlexPack{config: config}, nil
}

// CollectBuildInfo assembles build information for PSResource Install/Save/Update commands from the
// already-resolved packages in config.ResolvedPackages. It performs no network or filesystem I/O.
func (p *PSResourceFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	dependencies, err := BuildDependencies(p.config.ResolvedPackages)
	if err != nil {
		return nil, fmt.Errorf("collect PSResource dependencies: %w", err)
	}

	moduleID := p.config.Module
	if moduleID == "" {
		moduleID = "psresource-project"
	}

	return &entities.BuildInfo{
		Name:   buildName,
		Number: buildNumber,
		Modules: []entities.Module{
			{
				Id:           moduleID,
				Type:         entities.Nuget,
				Dependencies: dependencies,
			},
		},
	}, nil
}

// BuildDependencies turns already-resolved PSResource packages into entities.Dependency records.
// This is pure data assembly: the caller is responsible for resolving package identity (via
// Get-InstalledPSResource) and checksums (via a HEAD request to Artifactory) before calling this.
func BuildDependencies(resolved []ResolvedPackage) ([]entities.Dependency, error) {
	if len(resolved) == 0 {
		return nil, nil
	}
	dependencies := make([]entities.Dependency, 0, len(resolved))
	for _, pkg := range resolved {
		if err := validateNameVersion("resolved", pkg.Name, pkg.Version); err != nil {
			return nil, err
		}
		dependencies = append(dependencies, entities.Dependency{
			Id:       nupkgFileName(pkg.Name, pkg.Version),
			Type:     nupkgType,
			Scopes:   []string{"main"},
			Checksum: pkg.Checksum,
		})
	}
	return dependencies, nil
}

// GetProjectDependencies satisfies buildinfoflex.BuildInfoCollector. PSResource has no local project
// manifest to introspect independently of the resolved packages the caller supplies via
// PSResourceConfig.ResolvedPackages, so this always returns an empty result.
func (p *PSResourceFlexPack) GetProjectDependencies() ([]buildinfoflex.DependencyInfo, error) {
	return nil, nil
}

// GetDependencyGraph returns an empty graph since PSResource has no lock file
// and we only track direct dependencies.
func (p *PSResourceFlexPack) GetDependencyGraph() (map[string][]string, error) {
	return make(map[string][]string), nil
}
