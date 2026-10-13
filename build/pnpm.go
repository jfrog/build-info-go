package build

import (
	"errors"
	"os"
	"strings"

	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
)

const minSupportedPnpmVersion = "6.0.0"

type PnpmModule struct {
	containingBuild  *Build
	name             string
	srcPath          string
	executablePath   string
	pnpmArgs         []string
	collectBuildInfo bool
}

// Pass an empty string for srcPath to find the pnpm project in the working directory.
func newPnpmModule(srcPath string, containingBuild *Build) (*PnpmModule, error) {
	pnpmVersion, executablePath, err := buildutils.GetPnpmVersionAndExecPath(containingBuild.logger)
	if err != nil {
		return nil, err
	}
	if pnpmVersion.Compare(minSupportedPnpmVersion) > 0 {
		return nil, errors.New("pnpm CLI must have version " + minSupportedPnpmVersion + " or higher. The current version is: " + pnpmVersion.GetVersion())
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

	// Read module name - pnpm uses the same package.json format as npm
	packageInfo, err := buildutils.ReadPackageInfoFromPackageJsonIfExists(srcPath, pnpmVersion)
	if err != nil {
		return nil, err
	}
	name := packageInfo.BuildInfoModuleId()

	return &PnpmModule{name: name, srcPath: srcPath, containingBuild: containingBuild, executablePath: executablePath}, nil
}

func (pm *PnpmModule) Build() error {
	if len(pm.pnpmArgs) > 0 {
		output, _, err := buildutils.RunPnpmCmd(pm.executablePath, pm.srcPath, pm.pnpmArgs, &utils.NullLog{})
		if len(output) > 0 {
			pm.containingBuild.logger.Output(strings.TrimSpace(string(output)))
		}
		if err != nil {
			return err
		}
		// After executing the user-provided command, cleaning pnpmArgs is needed.
		pm.filterPnpmArgsFlags()
	}
	if !pm.collectBuildInfo {
		return nil
	}
	return pm.CalcDependencies()
}

func (pm *PnpmModule) CalcDependencies() error {
	if !pm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to collect the project's dependencies")
	}
	buildInfoDependencies, err := buildutils.CalculatePnpmDependenciesList(pm.executablePath, pm.srcPath, pm.name,
		buildutils.PnpmTreeDepListParam{Args: pm.pnpmArgs}, true, pm.containingBuild.logger)
	if err != nil {
		return err
	}
	buildInfoModule := entities.Module{Id: pm.name, Type: entities.Npm, Dependencies: buildInfoDependencies}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}
	return pm.containingBuild.SaveBuildInfo(buildInfo)
}

func (pm *PnpmModule) SetName(name string) {
	pm.name = name
}

func (pm *PnpmModule) SetPnpmArgs(pnpmArgs []string) {
	pm.pnpmArgs = pnpmArgs
}

func (pm *PnpmModule) SetCollectBuildInfo(collectBuildInfo bool) {
	pm.collectBuildInfo = collectBuildInfo
}

func (pm *PnpmModule) AddArtifacts(artifacts ...entities.Artifact) error {
	return pm.containingBuild.AddArtifacts(pm.name, entities.Npm, artifacts...)
}

// This function discards the pnpm command in pnpmArgs and keeps only the command flags.
// It is necessary for the pnpm command's name to come before the pnpm command's flags in pnpmArgs for the function to work correctly.
func (pm *PnpmModule) filterPnpmArgsFlags() {
	if len(pm.pnpmArgs) == 1 && !strings.HasPrefix(pm.pnpmArgs[0], "-") {
		pm.pnpmArgs = []string{}
	}
	for argIndex := 0; argIndex < len(pm.pnpmArgs); argIndex++ {
		if strings.HasPrefix(pm.pnpmArgs[argIndex], "-") {
			pm.pnpmArgs = pm.pnpmArgs[argIndex:]
		}
	}
}
