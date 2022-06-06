package project

import (
	"encoding/json"
	"github.com/jfrog/build-info-go/build/utils/dotnet/dependencies"
	"github.com/jfrog/build-info-go/build/utils/dotnet/dependenciestree"
	"github.com/jfrog/build-info-go/utils"
)

type Project interface {
	Name() string
	RootPath() string
	MarshalJSON() ([]byte, error)
	Extractor() dependencies.Extractor
	CreateDependencyTree(log utils.Log) error
	Load(dependenciesSource string, log utils.Log) (Project, error)
}

func CreateProject(name, rootPath string) Project {
	return &project{name: name, rootPath: rootPath}
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
	rootPath           string
	dependenciesSource string
	dependencyTree     dependenciestree.Tree
	extractor          dependencies.Extractor
}

func (project *project) Name() string {
	return project.name
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
