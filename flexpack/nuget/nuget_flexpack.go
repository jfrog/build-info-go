package nuget

import (
	"fmt"
	"path/filepath"
	"strings"

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

// CollectBuildInfo parses the solution/project selected by TargetPath, or falls back to
// WorkingDirectory when the native command did not receive an explicit target.
func (n *NuGetFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	workingDirectory := n.config.WorkingDirectory
	targetPath := n.config.TargetPath
	if targetPath != "" && !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workingDirectory, targetPath)
	}

	var (
		sol buildinfosolution.Solution
		err error
	)
	if isProjectFile(targetPath) {
		sol, err = buildinfosolution.LoadProject(targetPath, n.log)
	} else {
		solutionFile := ""
		if targetPath != "" {
			ext := filepath.Ext(targetPath)
			if strings.EqualFold(ext, ".sln") || strings.EqualFold(ext, ".slnx") {
				workingDirectory = filepath.Dir(targetPath)
				solutionFile = filepath.Base(targetPath)
			} else {
				workingDirectory = targetPath
			}
		}
		sol, err = buildinfosolution.Load(workingDirectory, solutionFile, "", n.log)
	}
	if err != nil {
		return nil, fmt.Errorf("load NuGet solution: %w", err)
	}
	// Honor the user-supplied --module override; when empty, default each module ID to the
	// fixed "<Name>:<Version>" form (falling back to the project name when no version is
	// available). BuildInfo's legacy project-name default is intentionally not used here.
	bi, err := sol.BuildInfoWithNameVersionModuleId(n.config.Module, n.log)
	if err != nil {
		return nil, fmt.Errorf("collect NuGet build info: %w", err)
	}
	bi.Name = buildName
	bi.Number = buildNumber
	return bi, nil
}

// GetProjectDependencies returns the flat list of dependencies for the project.
// TODO: callers that also invoke GetDependencyGraph and CollectBuildInfo trigger three separate
// solution parses. Cache the parsed BuildInfo on the receiver to reduce this to one.
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
// Each child appears at most once per parent (duplicate edges are eliminated).
func (n *NuGetFlexPack) GetDependencyGraph() (map[string][]string, error) {
	bi, err := n.CollectBuildInfo("", "")
	if err != nil {
		return nil, err
	}
	// Use a set internally to dedup edges before building the final slice map.
	edgeSets := make(map[string]map[string]struct{})
	for _, mod := range bi.Modules {
		for _, d := range mod.Dependencies {
			for _, chain := range d.RequestedBy {
				if len(chain) > 0 && chain[0] != "" {
					parent := chain[0]
					if edgeSets[parent] == nil {
						edgeSets[parent] = make(map[string]struct{})
					}
					edgeSets[parent][d.Id] = struct{}{}
				}
			}
		}
	}
	graph := make(map[string][]string, len(edgeSets))
	for parent, children := range edgeSets {
		childSlice := make([]string, 0, len(children))
		for child := range children {
			childSlice = append(childSlice, child)
		}
		graph[parent] = childSlice
	}
	return graph, nil
}

// isProjectFile reports whether path is a .NET project file by its extension.
// The set is explicit to avoid matching unrelated formats (e.g. ".aproj", ".xproj").
func isProjectFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csproj", ".fsproj", ".vbproj", ".proj":
		return true
	}
	return false
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
