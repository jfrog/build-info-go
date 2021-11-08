package build

import (
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gocmd/cmd"
	"github.com/jfrog/gocmd/executers"
	"github.com/jfrog/jfrog-client-go/utils/log"
)

type GoModule struct {
	containingBuild *Build
	dependencies    []executers.Package
	name            string
	srcPath         string
}

func newGoModule(srcPath string, containingBuild *Build) (*GoModule, error) {
	log.SetLogger(containingBuild.logger)

	var err error
	if srcPath == "" {
		srcPath, err = cmd.GetProjectRoot()
		if err != nil {
			return nil, err
		}
	}

	// Read module name
	name, err := cmd.GetModuleNameByDir(srcPath)
	if err != nil {
		return nil, err
	}

	return &GoModule{name: name, srcPath: srcPath, containingBuild: containingBuild}, nil
}

func (gm *GoModule) CalcDependencies() error {
	var err error
	err = gm.loadDependencies()
	if err != nil {
		return err
	}
	err = gm.createBuildInfoDependencies()
	if err != nil {
		return err
	}

	var buildInfoDependencies []entities.Dependency
	for _, dep := range gm.dependencies {
		buildInfoDependencies = append(buildInfoDependencies, dep.Dependencies()...)
	}

	buildInfoModule := entities.Module{Id: gm.name, Type: entities.Go, Dependencies: buildInfoDependencies}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}

	return gm.containingBuild.SaveBuildInfo(buildInfo)
}

func (gm *GoModule) SetName(name string) {
	gm.name = name
}

func (gm *GoModule) AddArtifacts(artifacts ...entities.Artifact) error {
	partial := &entities.Partial{ModuleId: gm.name, ModuleType: entities.Go, Artifacts: artifacts}
	return gm.containingBuild.SavePartialBuildInfo(partial)
}

// Get the go project dependencies.
func (gm *GoModule) createBuildInfoDependencies() error {
	for i, dep := range gm.dependencies {
		err := dep.PopulateZip()
		if err != nil {
			return err
		}
		gm.dependencies[i] = dep
	}
	return nil
}

func (gm *GoModule) loadDependencies() error {
	cachePath, err := cmd.GetCachePath()
	if err != nil {
		return err
	}
	modulesMap, err := cmd.GetDependenciesList(gm.srcPath)
	if err != nil {
		return err
	}
	if modulesMap == nil {
		return nil
	}
	gm.dependencies, err = executers.GetDependencies(cachePath, modulesMap)
	return err
}
