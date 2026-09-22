package psresource

import (
	"fmt"
	"os"

	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
)

// nupkgType is the build-info dependency/artifact type used for PSResource (NuGet-based) packages.
const nupkgType = "nupkg"

// defaultModuleID is the build-info module ID used when the caller does not supply one via
// PSResourceConfig.Module (or --module). Mirrors the same "<tool>-project" fallback convention
// other FlexPack collectors in this repo use for their default module IDs.
const defaultModuleID = "psresource-project"

// defaultScope is the only dependency scope PSResourceGet produces. Get-InstalledPSResource
// returns a flat list of installed modules with no dev/optional/transitive classification (unlike,
// say, Poetry's or Maven's dependency groups), so every resolved dependency is recorded under this
// single scope.
const defaultScope = "main"

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
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		config.WorkingDirectory = wd
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
		moduleID = defaultModuleID
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
		if err := validateNameVersion(resolvedPackageKind, pkg.Name, pkg.Version); err != nil {
			return nil, err
		}
		dependencies = append(dependencies, entities.Dependency{
			Id:       nupkgFileName(pkg.Name, pkg.Version),
			Type:     nupkgType,
			Scopes:   []string{defaultScope},
			Checksum: pkg.Checksum,
		})
	}
	return dependencies, nil
}

// GetProjectDependencies and GetDependencyGraph below are required by the
// buildinfoflex.BuildInfoCollector interface (see the compile-time assertion above) that every
// FlexPack collector in this repo implements; callers such as jfrog-cli-artifactory invoke them
// polymorphically through that interface, not through the concrete *PSResourceFlexPack type, so
// within this repo they are only exercised directly from psresource_test.go. Removing them would
// break the interface conformance assertion and any caller that depends on it.

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
