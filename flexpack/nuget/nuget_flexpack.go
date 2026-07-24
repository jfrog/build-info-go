package nuget

import (
	"fmt"

	buildinfosolution "github.com/jfrog/build-info-go/build/utils/dotnet/solution"
	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/build-info-go/utils"
)

// NuGetFlexPack collects build-info for NuGet projects using the FlexPack native approach.
// It reuses the existing solution/project parsers (project.assets.json, packages.config).
type NuGetFlexPack struct {
	config buildinfoflex.NuGetConfig
	log    utils.Log
}

// NewNuGetFlexPack creates a new NuGetFlexPack with the given configuration.
func NewNuGetFlexPack(config buildinfoflex.NuGetConfig, log utils.Log) (*NuGetFlexPack, error) {
	if config.WorkingDirectory == "" {
		return nil, fmt.Errorf("NuGetConfig.WorkingDirectory must not be empty")
	}
	if log == nil {
		log = utils.NewDefaultLogger(utils.INFO)
	}
	return &NuGetFlexPack{config: config, log: log}, nil
}

// CollectBuildInfo parses the solution/project in WorkingDirectory and returns a populated BuildInfo.
func (n *NuGetFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	sol, err := buildinfosolution.Load(n.config.WorkingDirectory, "", "", n.log)
	if err != nil {
		return nil, fmt.Errorf("load NuGet solution: %w", err)
	}
	bi, err := sol.BuildInfo("", n.log)
	if err != nil {
		return nil, fmt.Errorf("collect NuGet build info: %w", err)
	}
	bi.Name = buildName
	bi.Number = buildNumber
	return bi, nil
}

// GetProjectDependencies returns the flat list of dependencies for the project.
func (n *NuGetFlexPack) GetProjectDependencies() ([]buildinfoflex.DependencyInfo, error) {
	bi, err := n.CollectBuildInfo("", "")
	if err != nil {
		return nil, err
	}
	var deps []buildinfoflex.DependencyInfo
	for _, mod := range bi.Modules {
		for _, d := range mod.Dependencies {
			deps = append(deps, buildinfoflex.DependencyInfo{
				ID:          d.Id,
				Type:        d.Type,
				SHA1:        d.Sha1,
				SHA256:      d.Sha256,
				MD5:         d.Md5,
				Scopes:      d.Scopes,
				RequestedBy: flattenRequestedBy(d.RequestedBy),
			})
		}
	}
	return deps, nil
}

// GetDependencyGraph returns a parent→children map derived from RequestedBy chains.
func (n *NuGetFlexPack) GetDependencyGraph() (map[string][]string, error) {
	bi, err := n.CollectBuildInfo("", "")
	if err != nil {
		return nil, err
	}
	graph := make(map[string][]string)
	for _, mod := range bi.Modules {
		for _, d := range mod.Dependencies {
			for _, chain := range d.RequestedBy {
				if len(chain) > 0 && chain[0] != "" {
					graph[chain[0]] = append(graph[chain[0]], d.Id)
				}
			}
		}
	}
	return graph, nil
}

// flattenRequestedBy converts [][]string requestedBy chains into []string (first element of each chain).
func flattenRequestedBy(chains [][]string) []string {
	result := make([]string, 0, len(chains))
	seen := make(map[string]bool, len(chains))
	for _, chain := range chains {
		if len(chain) > 0 && !seen[chain[0]] {
			result = append(result, chain[0])
			seen[chain[0]] = true
		}
	}
	return result
}
