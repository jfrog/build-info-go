package build

import (
	"errors"
	"os"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const minSupportedNpmVersion = "5.4.0"

type NpmModule struct {
	containingBuild          *Build
	name                     string
	srcPath                  string
	executablePath           string
	npmArgs                  []string
}

// Pass an empty string for srcPath to find the npm project in the working directory.
func newNpmModule(srcPath string, containingBuild *Build) (*NpmModule, error) {
	npmVersion, executablePath, err := buildutils.GetNpmVersionAndExecPath(containingBuild.logger)
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
	packageInfo, err := buildutils.ReadPackageInfoFromPackageJson(srcPath, npmVersion)
	if err != nil {
		return nil, err
	}
	name := packageInfo.BuildInfoModuleId()

	return &NpmModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath}, nil
}

func (nm *NpmModule) CalcDependencies() error {
	if !nm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to collect the project's dependencies")
	}
	buildInfoDependencies, err := buildutils.CalculateDependenciesList(nm.executablePath, nm.srcPath, nm.name, nm.npmArgs, nm.containingBuild.logger)
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

func (nm *NpmModule) SetNpmArgs(npmArgs []string) {
	nm.npmArgs = npmArgs
}

func (nm *NpmModule) AddArtifacts(artifacts ...entities.Artifact) error {
	if !nm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to add artifacts")
	}
	partial := &entities.Partial{ModuleId: nm.name, ModuleType: entities.Npm, Artifacts: artifacts}
	return nm.containingBuild.SavePartialBuildInfo(partial)
}
