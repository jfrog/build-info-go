package entities

import (
	"strings"
	"time"

	"github.com/jfrog/gofrog/stringutils"
)

type ModuleType string

const (
	TimeFormat         = "2006-01-02T15:04:05.000-0700"
	BuildInfoEnvPrefix = "buildInfo.env."

	// Build type
	Build ModuleType = "build"

	// Package managers types
	Generic ModuleType = "generic"
	Maven   ModuleType = "maven"
	Gradle  ModuleType = "gradle"
	Docker  ModuleType = "docker"
	Npm     ModuleType = "npm"
	Nuget   ModuleType = "nuget"
	Go      ModuleType = "go"
	Python  ModuleType = "python"
)

type BuildInfo struct {
	Name          string   `json:"name,omitempty"`
	Number        string   `json:"number,omitempty"`
	Agent         *Agent   `json:"agent,omitempty"`
	BuildAgent    *Agent   `json:"buildAgent,omitempty"`
	Modules       []Module `json:"modules,omitempty"`
	Started       string   `json:"started,omitempty"`
	Properties    Env      `json:"properties,omitempty"`
	Principal     string   `json:"artifactoryPrincipal,omitempty"`
	BuildUrl      string   `json:"url,omitempty"`
	Issues        *Issues  `json:"issues,omitempty"`
	PluginVersion string   `json:"artifactoryPluginVersion,omitempty"`
	VcsList       []Vcs    `json:"vcs,omitempty"`
}

func New() *BuildInfo {
	return &BuildInfo{
		Agent:      &Agent{},
		BuildAgent: &Agent{Name: "GENERIC"},
		Modules:    make([]Module, 0),
		VcsList:    make([]Vcs, 0),
	}
}

func (targetBuildInfo *BuildInfo) SetBuildAgentVersion(buildAgentVersion string) {
	targetBuildInfo.BuildAgent.Version = buildAgentVersion
}

func (targetBuildInfo *BuildInfo) SetAgentName(agentName string) {
	targetBuildInfo.Agent.Name = agentName
}

func (targetBuildInfo *BuildInfo) SetAgentVersion(agentVersion string) {
	targetBuildInfo.Agent.Version = agentVersion
}

func (targetBuildInfo *BuildInfo) SetPluginVersion(pluginVersion string) {
	targetBuildInfo.PluginVersion = pluginVersion
}

// Append the modules of the received build info to this build info.
// If the two build info instances contain modules with identical names, these modules are merged.
// When merging the modules, the artifacts and dependencies remain unique according to their checksum.
func (targetBuildInfo *BuildInfo) Append(buildInfo *BuildInfo) {
	for _, newModule := range buildInfo.Modules {
		exists := false
		for i := range targetBuildInfo.Modules {
			if newModule.Id == targetBuildInfo.Modules[i].Id {
				mergeModules(&newModule, &targetBuildInfo.Modules[i])
				exists = true
				break
			}
		}
		if !exists {
			targetBuildInfo.Modules = append(targetBuildInfo.Modules, newModule)
		}
	}
}

// IncludeEnv gets one or more wildcard patterns and filters out environment variables that don't match any of them.
func (targetBuildInfo *BuildInfo) IncludeEnv(patterns ...string) error {
	var err error
	for key := range targetBuildInfo.Properties {
		if !strings.HasPrefix(key, BuildInfoEnvPrefix) {
			continue
		}
		envKey := strings.TrimPrefix(key, BuildInfoEnvPrefix)
		include := false
		for _, filterPattern := range patterns {
			include, err = stringutils.MatchWildcardPattern(strings.ToLower(filterPattern), strings.ToLower(envKey))
			if err != nil {
				return err
			}
			if include {
				break
			}
		}

		if !include {
			delete(targetBuildInfo.Properties, key)
		}
	}
	return nil
}

// ExcludeEnv gets one or more wildcard patterns and filters out environment variables that match at least one of them.
func (targetBuildInfo *BuildInfo) ExcludeEnv(patterns ...string) error {
	for key := range targetBuildInfo.Properties {
		if !strings.HasPrefix(key, BuildInfoEnvPrefix) {
			continue
		}
		envKey := strings.TrimPrefix(key, BuildInfoEnvPrefix)
		for _, filterPattern := range patterns {
			match, err := stringutils.MatchWildcardPattern(strings.ToLower(filterPattern), strings.ToLower(envKey))
			if err != nil {
				return err
			}
			if match {
				delete(targetBuildInfo.Properties, key)
				break
			}
		}
	}
	return nil
}

// Merge the first module into the second module.
func mergeModules(merge *Module, into *Module) {
	mergeArtifacts(&merge.Artifacts, &into.Artifacts)
	mergeArtifacts(&merge.ExcludedArtifacts, &into.ExcludedArtifacts)
	mergeDependencies(&merge.Dependencies, &into.Dependencies)
}

func mergeArtifacts(mergeArtifacts *[]Artifact, intoArtifacts *[]Artifact) {
	for _, mergeArtifact := range *mergeArtifacts {
		exists := false
		for _, artifact := range *intoArtifacts {
			if mergeArtifact.Sha1 == artifact.Sha1 {
				exists = true
				break
			}
		}
		if !exists {
			*intoArtifacts = append(*intoArtifacts, mergeArtifact)
		}
	}
}

func mergeDependencies(mergeDependencies *[]Dependency, intoDependencies *[]Dependency) {
	for _, mergeDependency := range *mergeDependencies {
		exists := false
		for _, dependency := range *intoDependencies {
			if mergeDependency.Sha1 == dependency.Sha1 {
				exists = true
				break
			}
		}
		if !exists {
			*intoDependencies = append(*intoDependencies, mergeDependency)
		}
	}
}

// PublishedBuildInfo represents the response structure returned from Artifactory when getting a build-info.
type PublishedBuildInfo struct {
	Uri       string    `json:"uri,omitempty"`
	BuildInfo BuildInfo `json:"buildInfo,omitempty"`
}

type Agent struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type Module struct {
	Type              ModuleType   `json:"type,omitempty"`
	Properties        interface{}  `json:"properties,omitempty"`
	Id                string       `json:"id,omitempty"`
	Artifacts         []Artifact   `json:"artifacts,omitempty"`
	ExcludedArtifacts []Artifact   `json:"excludedArtifacts,omitempty"`
	Dependencies      []Dependency `json:"dependencies,omitempty"`
	*Checksum
}

func (m *Module) isEqual(b Module) bool {
	return m.Id == b.Id && m.Type == b.Type && isEqualArtifactSlices(m.Artifacts, b.Artifacts) && isEqualDependencySlices(m.Dependencies, b.Dependencies)
}

func IsEqualModuleSlices(a, b []Module) bool {
	return isEqualModuleSlices(a, b) && isEqualModuleSlices(b, a)
}

func isEqualModuleSlices(a, b []Module) bool {
	visitedIndexes := make(map[int]bool)
	for _, aEl := range a {
		found := false
		for index, bEl := range b {
			if _, ok := visitedIndexes[index]; !ok && aEl.isEqual(bEl) {
				found = true
				visitedIndexes[index] = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type Artifact struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	Path string `json:"path,omitempty"`
	*Checksum
}

func (a *Artifact) isEqual(b Artifact) bool {
	return a.Name == b.Name && a.Path == b.Path && a.Type == b.Type && a.Sha1 == b.Sha1 && a.Md5 == b.Md5
}

func isEqualArtifactSlices(a, b []Artifact) bool {
	visitedIndexes := make(map[int]bool)
	for _, aEl := range a {
		found := false
		for index, bEl := range b {
			if _, ok := visitedIndexes[index]; !ok && aEl.isEqual(bEl) {
				found = true
				visitedIndexes[index] = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type Dependency struct {
	Id          string     `json:"id,omitempty"`
	Type        string     `json:"type,omitempty"`
	Scopes      []string   `json:"scopes,omitempty"`
	RequestedBy [][]string `json:"requestedBy,omitempty"`
	*Checksum
}

func (d *Dependency) IsEqual(b Dependency) bool {
	return d.Id == b.Id && d.Type == b.Type && d.Sha1 == b.Sha1 && d.Md5 == b.Md5
}

func isEqualDependencySlices(a, b []Dependency) bool {
	visitedIndexes := make(map[int]bool)
	for _, aEl := range a {
		found := false
		for index, bEl := range b {
			if _, ok := visitedIndexes[index]; !ok && aEl.IsEqual(bEl) {
				found = true
				visitedIndexes[index] = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type Issues struct {
	Tracker                *Tracker        `json:"tracker,omitempty"`
	AggregateBuildIssues   bool            `json:"aggregateBuildIssues,omitempty"`
	AggregationBuildStatus string          `json:"aggregationBuildStatus,omitempty"`
	AffectedIssues         []AffectedIssue `json:"affectedIssues,omitempty"`
}

type Tracker struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type AffectedIssue struct {
	Key        string `json:"key,omitempty"`
	Url        string `json:"url,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Aggregated bool   `json:"aggregated,omitempty"`
}

type Checksum struct {
	Sha1 string `json:"sha1,omitempty"`
	Md5  string `json:"md5,omitempty"`
}

type Env map[string]string

type Vcs struct {
	Url      string `json:"url,omitempty"`
	Revision string `json:"revision,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Partials []*Partial

type Partial struct {
	ModuleType   ModuleType   `json:"Type,omitempty"`
	Artifacts    []Artifact   `json:"Artifacts,omitempty"`
	Dependencies []Dependency `json:"Dependencies,omitempty"`
	Env          Env          `json:"Env,omitempty"`
	Timestamp    int64        `json:"Timestamp,omitempty"`
	ModuleId     string       `json:"ModuleId,omitempty"`
	Issues       *Issues      `json:"Issues,omitempty"`
	VcsList      []Vcs        `json:"vcs,omitempty"`
	*Checksum
}

func (partials Partials) Len() int {
	return len(partials)
}

func (partials Partials) Less(i, j int) bool {
	return partials[i].Timestamp < partials[j].Timestamp
}

func (partials Partials) Swap(i, j int) {
	partials[i], partials[j] = partials[j], partials[i]
}

type General struct {
	Timestamp time.Time `json:"Timestamp,omitempty"`
}
