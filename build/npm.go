package build

import (
	"errors"
	"os"
	"strings"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const minSupportedNpmVersion = "5.4.0"

type NpmModule struct {
	containingBuild  *Build
	name             string
	srcPath          string
	executablePath   string
	npmArgs          []string
	collectBuildInfo bool
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
	packageInfo, err := buildutils.ReadPackageInfoFromPackageJsonIfExists(srcPath, npmVersion)
	if err != nil {
		return nil, err
	}
	name := packageInfo.BuildInfoModuleId()

	return &NpmModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath}, nil
}

func (nm *NpmModule) Build() error {
	if len(nm.npmArgs) > 0 {
		output, _, err := buildutils.RunNpmCmd(nm.executablePath, nm.srcPath, nm.npmArgs, &utils.NullLog{})
		if len(output) > 0 {
			nm.containingBuild.logger.Output(strings.TrimSpace(string(output)))
		}
		if err != nil {
			return err
		}
		// After executing the user-provided command, cleaning npmArgs is needed.
		nm.filterNpmArgsFlags()
	}
	if !nm.collectBuildInfo {
		return nil
	}
	return nm.CalcDependencies()
}

func (nm *NpmModule) CalcDependencies() error {
	if !nm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to collect the project's dependencies")
	}
	buildInfoDependencies, err := buildutils.CalculateNpmDependenciesList(nm.executablePath, nm.srcPath, nm.name,
		buildutils.NpmTreeDepListParam{Args: nm.npmArgs}, true, nm.containingBuild.logger)
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

func (nm *NpmModule) SetCollectBuildInfo(collectBuildInfo bool) {
	nm.collectBuildInfo = collectBuildInfo
}

func (nm *NpmModule) AddArtifacts(artifacts ...entities.Artifact) error {
	return nm.containingBuild.AddArtifacts(nm.name, entities.Npm, artifacts...)
}

// This function discards the npm command in npmArgs and keeps only the command flags.
// It is necessary for the npm command's name to come before the npm command's flags in npmArgs for the function to work correctly.
func (nm *NpmModule) filterNpmArgsFlags() {
	if len(nm.npmArgs) == 1 && !strings.HasPrefix(nm.npmArgs[0], "-") {
		nm.npmArgs = []string{}
	}
	for argIndex := 0; argIndex < len(nm.npmArgs); argIndex++ {
		if strings.HasPrefix(nm.npmArgs[argIndex], "-") {
			nm.npmArgs = nm.npmArgs[argIndex:]
		}
	}
}
