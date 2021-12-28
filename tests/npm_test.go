package tests

import (
	"errors"
	"github.com/jfrog/build-info-go/build"
	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"testing"
)

func TestGenerateBuildInfoForNpmProject(t *testing.T) {
	service := build.NewBuildInfoService()
	goBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, goBuild.Clean())
	}()
	npmModule, err := goBuild.AddNpmModule(filepath.Join("testdata", "npm", "project"))
	assert.NoError(t, err)
	err = npmModule.CalcDependencies()
	assert.NoError(t, err)
	err = npmModule.AddArtifacts(entities.Artifact{Name: "artifactName", Type: "artifactType", Path: "artifactPath", Checksum: &entities.Checksum{Sha1: "123", Md5: "456"}})
	assert.NoError(t, err)
	buildInfo, err := goBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 2, 1, "jfrog-cli-tests:v1.0.0", entities.Npm)
}

func TestCollectDepsForNpmProjectWithTraverse(t *testing.T) {
	service := build.NewBuildInfoService()
	goBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", "2")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, goBuild.Clean())
	}()
	npmModule, err := goBuild.AddNpmModule(filepath.Join("testdata", "npm", "project"))
	assert.NoError(t, err)
	npmModule.SetTraverseDependenciesFunc(func(dependency *entities.Dependency) (bool, error) {
		if dependency.Id == "xml:1.0.1" {
			return false, nil
		}
		dependency.Checksum = &entities.Checksum{Sha1: "test123"}
		return true, nil
	})
	err = npmModule.CalcDependencies()
	assert.NoError(t, err)
	buildInfo, err := goBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 1, 0, "jfrog-cli-tests:v1.0.0", entities.Npm)
	assert.Equal(t, "json:9.0.6", buildInfo.Modules[0].Dependencies[0].Id)
	assert.Equal(t, "test123", buildInfo.Modules[0].Dependencies[0].Sha1)
}

func TestCollectDepsForNpmProjectWithErrorInTraverse(t *testing.T) {
	service := build.NewBuildInfoService()
	goBuild, err := service.GetOrCreateBuild("build-info-go-test-npm", "2")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, goBuild.Clean())
	}()
	npmModule, err := goBuild.AddNpmModule(filepath.Join("testdata", "npm", "project"))
	assert.NoError(t, err)
	npmModule.SetTraverseDependenciesFunc(func(dependency *entities.Dependency) (bool, error) {
		return false, errors.New("test error")
	})
	err = npmModule.CalcDependencies()
	assert.Error(t, err)
}
