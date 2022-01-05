package build

import (
	"errors"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"os"
)

const minSupportedNpmVersion = "5.4.0"

type NpmModule struct {
	containingBuild          *Build
	name                     string
	srcPath                  string
	executablePath           string
	typeRestriction          utils.TypeRestriction
	npmArgs                  []string
	traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)
	threads                  int
}

// Pass an empty string for srcPath to find the npm project in the working directory.
func newNpmModule(srcPath string, containingBuild *Build) (*NpmModule, error) {
	npmVersion, executablePath, err := utils.GetNpmVersionAndExecPath(containingBuild.logger)
	if err != nil {
		return nil, err
	}
	if npmVersion.Compare(minSupportedNpmVersion) > 0 {
		return nil, errors.New("npm CLI must have version " + minSupportedNpmVersion + " or higher. The current version is: " + npmVersion.GetVersion())
	}

	if srcPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		srcPath, err = utils.FindFileInDirAndParents(wd, "package.json")
		if err != nil {
			return nil, err
		}
	}

	// Read module name
	packageInfo, err := utils.ReadPackageInfoFromPackageJson(srcPath, npmVersion)
	if err != nil {
		return nil, err
	}
	name := packageInfo.BuildInfoModuleId()

	return &NpmModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath, threads: 3}, nil
}

func (nm *NpmModule) CalcDependencies() error {
	buildInfoDependencies, err := utils.CalculateDependenciesList(nm.typeRestriction, nm.executablePath, nm.srcPath, nm.name, nm.npmArgs, nm.traverseDependenciesFunc, nm.threads, nm.containingBuild.logger)
	if err != nil {
		return err
	}
	buildInfoModule := entities.Module{Id: nm.name, Type: entities.Npm, Dependencies: buildInfoDependencies}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}
	return nm.containingBuild.SaveBuildInfo(buildInfo)
}

func (nm *NpmModule) SetName(name string) {
	nm.name = name
}

func (nm *NpmModule) SetTypeRestriction(typeRestriction utils.TypeRestriction) {
	nm.typeRestriction = typeRestriction
}

func (nm *NpmModule) SetNpmArgs(npmArgs []string) {
	nm.npmArgs = npmArgs
}

func (nm *NpmModule) SetThreads(threads int) {
	nm.threads = threads
}

// SetTraverseDependenciesFunc gets a function to execute on all dependencies after their collection in CalcDependencies(), before they're saved.
// This function needs to return a boolean value indicating whether to save this dependency in the build-info or not.
// This function might run asynchronously with different dependencies (if the threads amount setting is bigger than 1).
// If more than one error are returned from this function in different threads, only the first of them will be returned from CalcDependencies().
func (nm *NpmModule) SetTraverseDependenciesFunc(traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)) {
	nm.traverseDependenciesFunc = traverseDependenciesFunc
}

func (nm *NpmModule) AddArtifacts(artifacts ...entities.Artifact) error {
	partial := &entities.Partial{ModuleId: nm.name, ModuleType: entities.Npm, Artifacts: artifacts}
	return nm.containingBuild.SavePartialBuildInfo(partial)
}
