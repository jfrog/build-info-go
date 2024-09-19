package entities

import (
	"errors"
	"github.com/jfrog/build-info-go/utils/compareutils"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"regexp"
	"sort"
	"strings"
	"time"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/jfrog/gofrog/stringutils"
)

type ModuleType string

const (
	TimeFormat           = "2006-01-02T15:04:05.000-0700"
	BuildInfoEnvPrefix   = "buildInfo.env."
	RequestedByMaxLength = 15

	// Build type
	Build ModuleType = "build"

	// Package managers types
	Generic   ModuleType = "generic"
	Maven     ModuleType = "maven"
	Gradle    ModuleType = "gradle"
	Docker    ModuleType = "docker"
	Npm       ModuleType = "npm"
	Nuget     ModuleType = "nuget"
	Go        ModuleType = "go"
	Python    ModuleType = "python"
	Terraform ModuleType = "terraform"
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
	for i, newModule := range buildInfo.Modules {
		exists := false
		for j := range targetBuildInfo.Modules {
			if newModule.Id == targetBuildInfo.Modules[j].Id {
				mergeModules(&buildInfo.Modules[i], &targetBuildInfo.Modules[j])
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

func (targetBuildInfo *BuildInfo) ToCycloneDxBom() (*cdx.BOM, error) {
	var biDependencies []Dependency
	moduleIds := make(map[string]bool)

	for _, module := range targetBuildInfo.Modules {
		// Aggregated builds are not supported
		if module.Type == Build {
			continue
		}

		moduleDep := Dependency{Id: module.Id}
		moduleIds[module.Id] = true

		dependenciesToAdd := []Dependency{moduleDep}
		dependenciesToAdd = append(dependenciesToAdd, module.Dependencies...)
		mergeDependenciesLists(&dependenciesToAdd, &biDependencies)
	}

	var components []cdx.Component
	depMap := make(map[string]map[string]bool)
	for _, biDep := range biDependencies {
		newComp, err := packageIdToCycloneDxComponent(biDep.Id)
		if err != nil {
			return nil, err
		}
		newComp.BOMRef = biDep.Id
		if _, exist := moduleIds[biDep.Id]; exist {
			newComp.Type = cdx.ComponentTypeApplication
		} else {
			newComp.Type = cdx.ComponentTypeLibrary
		}

		if !biDep.Checksum.IsEmpty() {
			hashes := []cdx.Hash{
				{
					Algorithm: cdx.HashAlgoSHA256,
					Value:     biDep.Sha256,
				},
				{
					Algorithm: cdx.HashAlgoSHA1,
					Value:     biDep.Sha1,
				},
				{
					Algorithm: cdx.HashAlgoMD5,
					Value:     biDep.Md5,
				},
			}
			newComp.Hashes = &hashes
		}
		components = append(components, *newComp)

		for _, reqByPath := range biDep.RequestedBy {
			if depMap[reqByPath[0]] == nil {
				depMap[reqByPath[0]] = make(map[string]bool)
			}
			depMap[reqByPath[0]][biDep.Id] = true
		}
	}

	sort.Slice(components, func(i, j int) bool {
		return components[i].BOMRef < components[j].BOMRef
	})

	// Convert the map of dependencies to CycloneDX dependency objects
	var dependencies []cdx.Dependency
	for compRef, deps := range depMap {
		depsSlice := maps.Keys(deps)
		sort.Slice(depsSlice, func(i, j int) bool {
			return depsSlice[i] < depsSlice[j]
		})
		dependencies = append(dependencies, cdx.Dependency{Ref: compRef, Dependencies: &depsSlice})
	}

	sort.Slice(dependencies, func(i, j int) bool {
		return dependencies[i].Ref < dependencies[j].Ref
	})

	bom := cdx.NewBOM()
	bom.Components = &components
	bom.Dependencies = &dependencies
	return bom, nil
}

func packageIdToCycloneDxComponent(packageId string) (*cdx.Component, error) {
	comp := &cdx.Component{}
	packageIdParts := strings.Split(packageId, ":")
	switch len(packageIdParts) {
	case 1:
		comp.Name = packageIdParts[0]
	case 2:
		comp.Name = packageIdParts[0]
		comp.Version = packageIdParts[1]
	case 3:
		comp.Group = packageIdParts[0]
		comp.Name = packageIdParts[1]
		comp.Version = packageIdParts[2]
	default:
		return nil, errors.New("invalid package identifier: " + packageId)
	}
	return comp, nil
}

// Merge the first module into the second module.
func mergeModules(merge *Module, into *Module) {
	mergeArtifacts(&merge.Artifacts, &into.Artifacts)
	mergeArtifacts(&merge.ExcludedArtifacts, &into.ExcludedArtifacts)
	mergeDependenciesLists(&merge.Dependencies, &into.Dependencies)
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

func mergeDependenciesLists(dependenciesToAdd, intoDependencies *[]Dependency) {
	for i, dependencyToAdd := range *dependenciesToAdd {
		exists := false
		for _, dependency := range *intoDependencies {
			if dependencyToAdd.Sha1 == dependency.Sha1 && dependencyToAdd.Id == dependency.Id {
				exists = true
				(*dependenciesToAdd)[i] = mergeDependencies(dependency, dependencyToAdd)
				break
			}
		}
		if !exists {
			*intoDependencies = append(*intoDependencies, dependencyToAdd)
		}
	}
}

func mergeDependencies(dep1, dep2 Dependency) Dependency {
	return Dependency{
		Id:          dep1.Id,
		Type:        dep1.Type,
		Scopes:      mergeStringSlices(dep1.Scopes, dep2.Scopes),
		RequestedBy: mergeRequestedBySlices(dep1.RequestedBy, dep2.RequestedBy),
		Checksum:    dep1.Checksum,
	}
}

func mergeStringSlices(slice1, slice2 []string) []string {
	for _, item2 := range slice2 {
		exists := false
		for _, item1 := range slice1 {
			if item1 == item2 {
				exists = true
				break
			}
		}
		if !exists {
			slice1 = append(slice1, item2)
		}
	}
	return slice1
}

// mergeRequestedBySlices gets two slices of dependencies' paths in a build (RequestedBy) and merges them together, without duplicates.
func mergeRequestedBySlices(requestedBy1, requestedBy2 [][]string) [][]string {
	for _, item2 := range requestedBy2 {
		exists := false
		for _, item1 := range requestedBy1 {
			if equalStringSlices(item1, item2) {
				exists = true
				break
			}
		}
		if !exists {
			requestedBy1 = append(requestedBy1, item2)
		}
	}
	return requestedBy1
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	// Parent is used to store the parent module id, for multi-module projects.
	// This field is not recognized by Artifactory, and is used for internal purposes only.
	Parent string `json:"parent,omitempty"`
	// Used in aggregated builds - this field stores the checksums of the referenced build-info JSON.
	Checksum
}

// If the 'other' Module matches the current one, return true.
// 'other' Module may contain regex values for Id, Artifacts, ExcludedArtifacts, Dependencies and Checksum.
func (m *Module) isEqual(other Module) (bool, error) {
	match, err := m.Checksum.IsEqual(other.Checksum)
	if !match || err != nil {
		return false, err
	}
	match, err = isEqualArtifactSlices(m.Artifacts, other.Artifacts)
	if !match || err != nil {
		return false, err
	}
	match, err = isEqualArtifactSlices(m.ExcludedArtifacts, other.ExcludedArtifacts)
	if !match || err != nil {
		return false, err
	}
	match, err = isEqualDependencySlices(m.Dependencies, other.Dependencies)
	if !match || err != nil {
		return false, err
	}
	match, err = regexp.MatchString(m.Id, other.Id)
	if !match || err != nil {
		return false, err
	}
	return m.Type == other.Type, nil
}

func IsEqualModuleSlices(actual, other []Module) (bool, error) {
	return isEqualModuleSlices(actual, other)
}

func isEqualModuleSlices(actual, other []Module) (bool, error) {
	if len(actual) != len(other) {
		return false, nil
	}
	visitedIndexes := make(map[int]bool)
	for _, aEl := range actual {
		found := false
		for index, eEl := range other {
			if _, ok := visitedIndexes[index]; !ok {
				match, err := aEl.isEqual(eEl)
				if err != nil {
					return false, err
				}
				if match {
					found = true
					visitedIndexes[index] = true
					break
				}
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

type Artifact struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	Path string `json:"path,omitempty"`
	// The target repository to which the artifact was deployed to.
	// Named 'original' because the repository might change throughout the lifecycle of the build.
	// This field is not recognized by Artifactory, and is used for internal purposes only.
	OriginalDeploymentRepo string `json:"originalDeploymentRepo,omitempty"`
	Checksum
}

// If the 'other' Artifact matches the current one, return true.
// 'other' Artifacts may contain regex values for Name, Path, and Checksum.
func (a *Artifact) isEqual(other Artifact) (bool, error) {
	match, err := regexp.MatchString(other.Name, a.Name)
	if !match || err != nil {
		return false, err
	}
	match, err = regexp.MatchString(other.Path, a.Path)
	if !match || err != nil {
		return false, err
	}
	match, err = a.Checksum.IsEqual(other.Checksum)
	if !match || err != nil {
		return false, err
	}

	return a.Type == other.Type, nil
}

func isEqualArtifactSlices(actual, other []Artifact) (bool, error) {
	if len(actual) != len(other) {
		return false, nil
	}
	visitedIndexes := make(map[int]bool)
	for _, aEl := range actual {
		found := false
		for index, eEl := range other {
			if _, ok := visitedIndexes[index]; !ok {
				match, err := aEl.isEqual(eEl)
				if err != nil {
					return false, err
				}
				if match {
					found = true
					visitedIndexes[index] = true
					break
				}
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

type Dependency struct {
	Id          string     `json:"id,omitempty"`
	Type        string     `json:"type,omitempty"`
	Scopes      []string   `json:"scopes,omitempty"`
	RequestedBy [][]string `json:"requestedBy,omitempty"`
	Checksum
}

// If the 'other' Dependency matches the current one, return true.
// 'other' Dependency may contain regex values for Id and Checksum.
func (d *Dependency) IsEqual(other Dependency) (bool, error) {
	match, err := d.Checksum.IsEqual(other.Checksum)
	if !match || err != nil {
		return false, err
	}
	match, err = regexp.MatchString(other.Id, d.Id)
	if !match || err != nil {
		return false, err
	}
	return d.Type == other.Type && compareutils.IsEqualSlices(d.Scopes, other.Scopes) && compareutils.IsEqual2DSlices(d.RequestedBy, other.RequestedBy), nil
}

func IsEqualDependencySlices(actual, other []Dependency) (bool, error) {
	return IsEqualModuleSlices([]Module{{Dependencies: actual}}, []Module{{Dependencies: other}})
}

func isEqualDependencySlices(actual, other []Dependency) (bool, error) {
	if len(actual) != len(other) {
		return false, nil
	}
	visitedIndexes := make(map[int]bool)
	for _, aEl := range actual {
		found := false
		for index, eEl := range other {
			if _, ok := visitedIndexes[index]; !ok {
				match, err := aEl.IsEqual(eEl)
				if err != nil {
					return false, err
				}
				if match {
					found = true
					visitedIndexes[index] = true
					break
				}
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}
func (d *Dependency) UpdateRequestedBy(parentId string, parentRequestedBy [][]string) {
	// Filter all existing paths from parent
	var filteredChildRequestedBy [][]string
	for _, childRequestedBy := range d.RequestedBy {
		if childRequestedBy[0] != parentId {
			filteredChildRequestedBy = append(filteredChildRequestedBy, childRequestedBy)
		}
	}
	// Add all updated paths from parent
	for _, requestedBy := range parentRequestedBy {
		newRequestedBy := append([]string{parentId}, requestedBy...)
		filteredChildRequestedBy = append(filteredChildRequestedBy, newRequestedBy)
	}
	d.RequestedBy = filteredChildRequestedBy
}

func (d *Dependency) NodeHasLoop() bool {
	for _, requestedBy := range d.RequestedBy {
		if slices.Contains(requestedBy, d.Id) {
			return true
		}
	}
	return false
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
	Sha1   string `json:"sha1,omitempty"`
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

func (c *Checksum) IsEmpty() bool {
	return c.Md5 == "" && c.Sha1 == "" && c.Sha256 == ""
}

// If the 'other' checksum matches the current one, return true.
// 'other' checksum may contain regex values for sha1, sha256 and md5.
func (c *Checksum) IsEqual(other Checksum) (bool, error) {
	match, err := regexp.MatchString(other.Md5, c.Md5)
	if !match || err != nil {
		return false, err
	}
	match, err = regexp.MatchString(other.Sha1, c.Sha1)
	if !match || err != nil {
		return false, err
	}
	match, err = regexp.MatchString(other.Sha256, c.Sha256)
	if !match || err != nil {
		return false, err
	}

	return true, nil
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
	Checksum
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
