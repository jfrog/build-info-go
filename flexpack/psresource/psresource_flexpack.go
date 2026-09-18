package psresource

import (
	"fmt"

	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/build-info-go/utils"
)

// nupkgType is the build-info dependency/artifact type used for PSResource (NuGet-based) packages.
const nupkgType = "nupkg"

// ResolvedPackage is a PSResource package whose identity and checksum have already been determined
// by the caller: name/version via Get-InstalledPSResource, checksum via a HEAD request to
// Artifactory. build-info-go performs neither of those - it only assembles the resulting data into
// entities.Dependency records, exactly like every other FlexPack collector in this repo.
type ResolvedPackage struct {
	// Name is the PSResource package name, as returned by Get-InstalledPSResource.
	Name string

	// Version is the resolved package version, as returned by Get-InstalledPSResource.
	Version string

	// Checksum is the package checksum, already fetched by the caller from Artifactory.
	Checksum entities.Checksum
}

// PSResourceFlexPack collects build-info for PowerShell PSResourceGet using the FlexPack native
// approach. It performs no I/O of its own: it turns pre-resolved package data supplied by the
// caller (jfrog-cli-artifactory) into entities.BuildInfo structures.
type PSResourceFlexPack struct {
	config buildinfoflex.PSResourceConfig
	log    utils.Log
}

// NewPSResourceFlexPack creates a new PSResourceFlexPack with the given configuration.
func NewPSResourceFlexPack(config buildinfoflex.PSResourceConfig, log utils.Log) (*PSResourceFlexPack, error) {
	if config.WorkingDirectory == "" {
		return nil, fmt.Errorf("PSResourceConfig.WorkingDirectory must not be empty")
	}
	if log == nil {
		log = utils.NewDefaultLogger(utils.INFO)
	}
	return &PSResourceFlexPack{config: config, log: log}, nil
}

// CollectBuildInfo assembles build information for PSResource Install/Save/Update commands from the
// already-resolved packages supplied in resolved. It performs no network or filesystem I/O.
func (p *PSResourceFlexPack) CollectBuildInfo(buildName, buildNumber string, resolved []ResolvedPackage) (*entities.BuildInfo, error) {
	dependencies, err := BuildDependencies(resolved)
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
		if pkg.Name == "" {
			return nil, fmt.Errorf("resolved PSResource package is missing a name")
		}
		if pkg.Version == "" {
			return nil, fmt.Errorf("resolved PSResource package %q is missing a version", pkg.Name)
		}
		dependencies = append(dependencies, entities.Dependency{
			Id:       fmt.Sprintf("%s.%s.nupkg", pkg.Name, pkg.Version),
			Type:     nupkgType,
			Scopes:   []string{"main"},
			Checksum: pkg.Checksum,
		})
	}
	return dependencies, nil
}

// GetProjectDependencies is retained to satisfy buildinfoflex.BuildInfoCollector. PSResource has no
// local project manifest to introspect independently of the resolved packages the caller supplies
// to CollectBuildInfo, so this always returns an empty result.
func (p *PSResourceFlexPack) GetProjectDependencies() ([]buildinfoflex.DependencyInfo, error) {
	return nil, nil
}

// GetDependencyGraph returns an empty graph since PSResource has no lock file
// and we only track direct dependencies.
func (p *PSResourceFlexPack) GetDependencyGraph() (map[string][]string, error) {
	return make(map[string][]string), nil
}
