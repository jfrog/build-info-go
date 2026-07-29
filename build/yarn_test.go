package build

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/tests"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/parallel"
	"github.com/stretchr/testify/assert"
)

func TestYarnDependencyName(t *testing.T) {
	testCases := []struct {
		dependencyValue string
		expectedName    string
	}{
		{"yargs-unparser@npm:2.0.0", "yargs-unparser"},
		{"typescript@patch:typescript@npm%3A3.9.9#builtin<compat/typescript>::version=3.9.9&hash=a45b0e", "typescript"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight"},
		{"@types/tmp@patch:@types/tmp@npm%3A0.1.0#builtin<compat/typescript>::version=0.1.0&hash=a45b0e", "@types/tmp"},
	}

	for _, testCase := range testCases {
		dependency := &buildutils.YarnDependency{Value: testCase.dependencyValue}
		depName, err := dependency.Name()
		assert.NoError(t, err)
		assert.Equal(t, testCase.expectedName, depName)
	}
}

func TestAppendDependencyRecursively(t *testing.T) {
	dependenciesMap := map[string]*buildutils.YarnDependency{
		// For test 1:
		"pack1@npm:1.0.0": {Value: "pack1@npm:1.0.0", Details: buildutils.YarnDepDetails{Version: "1.0.0"}},
		"pack2@npm:1.0.0": {Value: "pack2@npm:1.0.0", Details: buildutils.YarnDepDetails{Version: "1.0.0"}},
		"pack3@npm:1.0.0": {Value: "pack3@npm:1.0.0", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "pack1@virtual:c192f6b3b32cd5d11a443144e162ec3bc#npm:1.0.0"}, {Locator: "pack2@npm:1.0.0"}}}},
		// For test 2:
		"pack4@npm:1.0.0": {Value: "pack4@npm:1.0.0", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "pack5@npm:1.0.0"}}}},
		"pack5@npm:1.0.0": {Value: "pack5@npm:1.0.0", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "pack6@npm:1.0.0"}}}},
		"pack6@npm:1.0.0": {Value: "pack6@npm:1.0.0", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "pack4@npm:1.0.0"}}}},
	}
	yarnModule := &YarnModule{}

	testCases := []struct {
		dependency           *buildutils.YarnDependency
		expectedDependencies map[string]*entities.Dependency
	}{
		{
			dependenciesMap["pack3@npm:1.0.0"],
			map[string]*entities.Dependency{
				"pack1:1.0.0": {Id: "pack1:1.0.0", RequestedBy: [][]string{{"pack3:1.0.0", "rootpack:1.0.0"}}},
				"pack2:1.0.0": {Id: "pack2:1.0.0", RequestedBy: [][]string{{"pack3:1.0.0", "rootpack:1.0.0"}}},
				"pack3:1.0.0": {Id: "pack3:1.0.0", RequestedBy: [][]string{{"rootpack:1.0.0"}}},
			},
		}, {
			dependenciesMap["pack6@npm:1.0.0"],
			map[string]*entities.Dependency{
				"pack4:1.0.0": {Id: "pack4:1.0.0", RequestedBy: [][]string{{"pack6:1.0.0", "rootpack:1.0.0"}}},
				"pack5:1.0.0": {Id: "pack5:1.0.0", RequestedBy: [][]string{{"pack4:1.0.0", "pack6:1.0.0", "rootpack:1.0.0"}}},
				"pack6:1.0.0": {Id: "pack6:1.0.0", RequestedBy: [][]string{{"rootpack:1.0.0"}}},
			},
		},
	}

	for _, testCase := range testCases {
		producerConsumer := parallel.NewBounedRunner(1, false)
		biDependencies := make(map[string]*entities.Dependency)
		go func() {
			defer producerConsumer.Done()
			err := yarnModule.appendDependencyRecursively(testCase.dependency, []string{"rootpack:1.0.0"}, dependenciesMap, biDependencies)
			assert.NoError(t, err)
		}()
		producerConsumer.Run()
		assert.True(t, reflect.DeepEqual(testCase.expectedDependencies, biDependencies), "The result dependencies tree doesn't match the expected. expected: %s, actual: %s", testCase.expectedDependencies, biDependencies)
	}
}

// TestAppendDependencyRecursively_NonRegistryCollapse verifies that non-registry locators
// (workspace, link, file, portal, git) are excluded from build-info emission but their
// transitive registry deps are still captured. The non-registry node is also collapsed
// from the requestedBy chain (option B).
func TestAppendDependencyRecursively_NonRegistryCollapse(t *testing.T) {
	// Topology: root -> portal-dep (portal, non-registry) -> xml@npm (registry)
	//                -> link-dep (link, non-registry)  [no transitive]
	depMap := map[string]*buildutils.YarnDependency{
		"xml@npm:1.0.1":                   {Value: "xml@npm:1.0.1", Details: buildutils.YarnDepDetails{Version: "1.0.1"}},
		"portal-dep@portal:./portal-dep":  {Value: "portal-dep@portal:./portal-dep", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "xml@npm:1.0.1"}}}},
		"link-dep@link:./link-dep":        {Value: "link-dep@link:./link-dep", Details: buildutils.YarnDepDetails{Version: "1.0.0"}},
		"root@workspace:.":                {Value: "root@workspace:.", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "portal-dep@portal:./portal-dep"}, {Locator: "link-dep@link:./link-dep"}}}},
	}
	ym := &YarnModule{containingBuild: &Build{logger: &utils.NullLog{}}}
	biDeps := make(map[string]*entities.Dependency)

	// Walk from root (pathToRoot=[]) — simulates normal getDependenciesMap root walk.
	err := ym.appendDependencyRecursively(depMap["root@workspace:."], []string{}, depMap, biDeps)
	assert.NoError(t, err)

	// Only xml should be emitted — portal-dep and link-dep are non-registry and dropped.
	assert.Len(t, biDeps, 1)
	assert.Contains(t, biDeps, "xml:1.0.1")
	assert.NotContains(t, biDeps, "portal-dep:1.0.0")
	assert.NotContains(t, biDeps, "link-dep:1.0.0")

	// requestedBy for xml should be ["root:1.0.0"] — portal-dep collapsed from chain.
	assert.Equal(t, [][]string{{"root:1.0.0"}}, biDeps["xml:1.0.1"].RequestedBy)
}

// TestGetDependenciesMap_WorkspaceRoot verifies that when IsYarnWorkspaceProject returns true,
// getDependenciesMap seeds additional walks from each workspace member and captures their
// registry deps even though the root node has no direct dependency edges in yarn info output.
func TestGetDependenciesMap_WorkspaceRoot(t *testing.T) {
	// Simulates: s1-root (workspace root, no deps) with two workspace members.
	// pkg-a -> xml@npm:1.0.1
	// pkg-b -> json@npm:9.0.6
	depMap := map[string]*buildutils.YarnDependency{
		"xml@npm:1.0.1":                          {Value: "xml@npm:1.0.1", Details: buildutils.YarnDepDetails{Version: "1.0.1"}},
		"json@npm:9.0.6":                         {Value: "json@npm:9.0.6", Details: buildutils.YarnDepDetails{Version: "9.0.6"}},
		"pkg-a@workspace:packages/pkg-a":         {Value: "pkg-a@workspace:packages/pkg-a", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "xml@npm:1.0.1"}}}},
		"pkg-b@workspace:packages/pkg-b":         {Value: "pkg-b@workspace:packages/pkg-b", Details: buildutils.YarnDepDetails{Version: "1.0.0", Dependencies: []buildutils.YarnDependencyPointer{{Locator: "json@npm:9.0.6"}}}},
		"s1-root@workspace:.":                    {Value: "s1-root@workspace:.", Details: buildutils.YarnDepDetails{Version: "0.0.0"}},
	}
	root := depMap["s1-root@workspace:."]

	containingBuild := &Build{logger: &utils.NullLog{}}
	ym := &YarnModule{containingBuild: containingBuild}

	biDeps := make(map[string]*entities.Dependency)
	// Normal root walk first (produces nothing — root has no edges).
	err := ym.appendDependencyRecursively(root, []string{}, depMap, biDeps)
	assert.NoError(t, err)
	assert.Empty(t, biDeps)

	// Simulate workspace seed (as getDependenciesMap does).
	rootName, _ := root.Name()
	rootId := rootName + ":" + root.Details.Version
	for _, dep := range depMap {
		if dep.Value != root.Value && buildutils.IsWorkspaceLocator(dep.Value) {
			err = ym.appendDependencyRecursively(dep, []string{rootId}, depMap, biDeps)
			assert.NoError(t, err)
		}
	}

	assert.Len(t, biDeps, 2)
	assert.Contains(t, biDeps, "xml:1.0.1")
	assert.Contains(t, biDeps, "json:9.0.6")
	assert.NotContains(t, biDeps, "pkg-a:1.0.0")
	assert.NotContains(t, biDeps, "pkg-b:1.0.0")
	assert.Equal(t, [][]string{{"s1-root:0.0.0"}}, biDeps["xml:1.0.1"].RequestedBy)
	assert.Equal(t, [][]string{{"s1-root:0.0.0"}}, biDeps["json:9.0.6"].RequestedBy)
}

func TestGenerateBuildInfoForYarnProject(t *testing.T) {
	// Copy the project directory to a temporary directory
	tempDirPath, createTempDirCallback := tests.CreateTempDirWithCallbackAndAssert(t)
	defer createTempDirCallback()
	testDataSource := filepath.Join("testdata", "yarn", "v2")
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	service := NewBuildInfoService()
	yarnBuild, err := service.GetOrCreateBuild("build-info-go-test-yarn", "1")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, yarnBuild.Clean())
	}()
	yarnModule, err := yarnBuild.AddYarnModule(filepath.Join(testDataTarget, "project"))
	assert.NoError(t, err)
	err = yarnModule.Build()
	assert.NoError(t, err)
	err = yarnModule.AddArtifacts(entities.Artifact{Name: "artifactName", Type: "artifactType", Path: "artifactPath", Checksum: entities.Checksum{Sha1: "123", Md5: "456", Sha256: "789"}})
	assert.NoError(t, err)
	buildInfo, err := yarnBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 5, 1, "build-info-go-tests:v1.0.0", entities.Npm, false)
}

func TestCollectDepsForYarnProjectWithTraverse(t *testing.T) {
	// Copy the project directory to a temporary directory
	tempDirPath, createTempDirCallback := tests.CreateTempDirWithCallbackAndAssert(t)
	defer createTempDirCallback()
	testDataSource := filepath.Join("testdata", "yarn", "v2")
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	service := NewBuildInfoService()
	yarnBuild, err := service.GetOrCreateBuild("build-info-go-test-yarn", "2")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, yarnBuild.Clean())
	}()
	yarnModule, err := yarnBuild.AddYarnModule(filepath.Join(testDataTarget, "project"))
	assert.NoError(t, err)
	yarnModule.SetTraverseDependenciesFunc(func(dependency *entities.Dependency) (bool, error) {
		if dependency.Id == "xml:1.0.1" {
			return false, nil
		}
		dependency.Checksum = entities.Checksum{Sha1: "test123", Md5: "test456", Sha256: "test789"}
		return true, nil
	})
	err = yarnModule.Build()
	assert.NoError(t, err)
	buildInfo, err := yarnBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 4, 0, "build-info-go-tests:v1.0.0", entities.Npm, true)
	var dependencies []string
	for _, dep := range buildInfo.Modules[0].Dependencies {
		dependencies = append(dependencies, dep.Id)
	}

	expectedDependencies := []string{"js-tokens:4.0.0", "json:9.0.6", "loose-envify:1.4.0", "react:18.2.0"}

	for _, expectedDep := range expectedDependencies {
		assert.Contains(t, dependencies, expectedDep)
	}
}

func TestCollectDepsForYarnProjectWithErrorInTraverse(t *testing.T) {
	// Copy the project directory to a temporary directory
	tempDirPath, createTempDirCallback := tests.CreateTempDirWithCallbackAndAssert(t)
	defer createTempDirCallback()
	testDataSource := filepath.Join("testdata", "yarn", "v2")
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	service := NewBuildInfoService()
	yarnBuild, err := service.GetOrCreateBuild("build-info-go-test-yarn", "3")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, yarnBuild.Clean())
	}()
	yarnModule, err := yarnBuild.AddYarnModule(filepath.Join(testDataTarget, "project"))
	assert.NoError(t, err)
	yarnModule.SetTraverseDependenciesFunc(func(dependency *entities.Dependency) (bool, error) {
		return false, errors.New("test error")
	})
	err = yarnModule.Build()
	assert.Error(t, err)
}

func TestBuildYarnProjectWithArgs(t *testing.T) {
	// Copy the project directory to a temporary directory
	tempDirPath, createTempDirCallback := tests.CreateTempDirWithCallbackAndAssert(t)
	defer createTempDirCallback()
	testDataSource := filepath.Join("testdata", "yarn", "v2")
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	service := NewBuildInfoService()
	yarnBuild, err := service.GetOrCreateBuild("build-info-go-test-yarn", "4")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, yarnBuild.Clean())
	}()
	yarnModule, err := yarnBuild.AddYarnModule(filepath.Join(testDataTarget, "project"))
	assert.NoError(t, err)
	yarnModule.SetArgs([]string{"add", "statuses@1.5.0"})
	err = yarnModule.Build()
	assert.NoError(t, err)
	buildInfo, err := yarnBuild.ToBuildInfo()
	assert.NoError(t, err)
	assert.Len(t, buildInfo.Modules, 1)
	validateModule(t, buildInfo.Modules[0], 6, 0, "build-info-go-tests:v1.0.0", entities.Npm, false)
}
