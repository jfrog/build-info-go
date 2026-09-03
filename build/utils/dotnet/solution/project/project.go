package project

import (
	"encoding/json"
	"encoding/xml"
	"github.com/jfrog/build-info-go/build/utils/dotnet/dependencies"
	"github.com/jfrog/build-info-go/build/utils/dotnet/dependenciestree"
	"github.com/jfrog/build-info-go/utils"
	"os"
	"path/filepath"
	"strings"
)

type Project interface {
	Name() string
	// PackageID returns the project's effective NuGet PackageId, resolved the same way
	// 'dotnet pack'/'nuget pack' resolve it: an explicit <PackageId>, then <AssemblyName>,
	// then the project file's base name. This is what pack/push always embed in the produced
	// .nupkg's file name, so build-info modules produced by 'restore' and by 'pack'/'push' share
	// the same module identity instead of splitting into two disconnected modules whenever
	// <PackageId> differs from the project file's name.
	PackageID() string
	RootPath() string
	MarshalJSON() ([]byte, error)
	Extractor() dependencies.Extractor
	CreateDependencyTree(log utils.Log) error
	Load(dependenciesSource string, log utils.Log) (Project, error)
}

// CreateProject creates a Project for the given project file (.csproj/.fsproj/.vbproj/...).
func CreateProject(projectFilePath string) Project {
	name := strings.TrimSuffix(filepath.Base(projectFilePath), filepath.Ext(projectFilePath))
	return &project{
		name:      name,
		rootPath:  filepath.Dir(projectFilePath),
		packageID: resolvePackageID(projectFilePath, name),
	}
}

// CreateProjectFromDir creates a Project for a standalone dependencies source (e.g. a bare
// packages.config) that has no enclosing *proj file. The directory's own base name stands in
// for both the project name and its NuGet package ID, since there's no project file to read
// PackageId/AssemblyName from.
func CreateProjectFromDir(dirPath string) Project {
	name := filepath.Base(dirPath)
	return &project{
		name:      name,
		rootPath:  dirPath,
		packageID: name,
	}
}

// msbuildPropertyGroup captures the subset of a <PropertyGroup> element relevant to resolving
// a project's NuGet package identity. Condition attributes on PropertyGroup/PackageId (e.g.
// per-configuration overrides) are intentionally not evaluated; the first non-empty value found
// across the file's PropertyGroups is used, matching the common single-value case.
type msbuildPropertyGroup struct {
	PackageId    string `xml:"PackageId"`
	AssemblyName string `xml:"AssemblyName"`
}

type msbuildProject struct {
	PropertyGroups []msbuildPropertyGroup `xml:"PropertyGroup"`
}

// resolvePackageID reads projectFilePath and returns its effective PackageId, following
// MSBuild's own default resolution order (PackageId, then AssemblyName), falling back to
// fallbackName when the file can't be read/parsed or neither property is set.
//
// Limitation: only the project file itself is parsed; Directory.Build.props files in parent
// directories are not walked. If PackageId is declared exclusively in Directory.Build.props,
// the fallback name (derived from the filename) is used instead.
func resolvePackageID(projectFilePath, fallbackName string) string {
	content, err := os.ReadFile(projectFilePath)
	if err != nil {
		return fallbackName
	}
	var parsed msbuildProject
	if err := xml.Unmarshal(content, &parsed); err != nil {
		return fallbackName
	}
	for _, group := range parsed.PropertyGroups {
		if group.PackageId != "" {
			return group.PackageId
		}
	}
	for _, group := range parsed.PropertyGroups {
		if group.AssemblyName != "" {
			return group.AssemblyName
		}
	}
	return fallbackName
}

func (project *project) getCompatibleExtractor(log utils.Log) (dependencies.Extractor, error) {
	extractor, err := dependencies.CreateCompatibleExtractor(project.name, project.dependenciesSource, log)
	return extractor, err
}

func (project *project) CreateDependencyTree(log utils.Log) error {
	var err error
	project.dependencyTree, err = dependencies.CreateDependencyTree(project.extractor, log)
	return err
}

type project struct {
	name               string
	packageID          string
	rootPath           string
	dependenciesSource string
	dependencyTree     dependenciestree.Tree
	extractor          dependencies.Extractor
}

func (project *project) Name() string {
	return project.name
}

func (project *project) PackageID() string {
	return project.packageID
}

func (project *project) RootPath() string {
	return project.rootPath
}

func (project *project) Extractor() dependencies.Extractor {
	return project.extractor
}

func (project *project) Load(dependenciesSource string, log utils.Log) (Project, error) {
	var err error
	project.dependenciesSource = dependenciesSource
	project.extractor, err = project.getCompatibleExtractor(log)
	return project, err
}

func (project *project) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name         string                `json:"name,omitempty"`
		Dependencies dependenciestree.Tree `json:"dependencies,omitempty"`
	}{
		Name:         project.name,
		Dependencies: project.dependencyTree,
	})
}
