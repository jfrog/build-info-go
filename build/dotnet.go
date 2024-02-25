package build

import (
	"github.com/jfrog/build-info-go/build/utils/dotnet"
	"github.com/jfrog/build-info-go/build/utils/dotnet/solution"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/io"
	"os"
	"path/filepath"
	"strings"
)

type DotnetModule struct {
	containingBuild *Build
	name            string
	toolchainType   dotnet.ToolchainType
	subCommand      string
	argAndFlags     []string
	solutionPath    string
}

// Pass an empty string for srcPath to find the solutions/proj files in the working directory.
func newDotnetModule(srcPath string, containingBuild *Build) (module *DotnetModule, err error) {
	if srcPath == "" {
		srcPath, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	return &DotnetModule{solutionPath: srcPath, containingBuild: containingBuild, argAndFlags: []string{"restore"}}, nil
}

func (dm *DotnetModule) SetArgAndFlags(argAndFlags []string) {
	dm.argAndFlags = argAndFlags
}

func (dm *DotnetModule) SetName(name string) {
	dm.name = name
}

func (dm *DotnetModule) SetSubcommand(subCommand string) {
	dm.subCommand = subCommand
}

func (dm *DotnetModule) SetSolutionPath(solutionPath string) {
	dm.solutionPath = solutionPath
}

func (dm *DotnetModule) SetToolchainType(toolchainType dotnet.ToolchainType) {
	dm.toolchainType = toolchainType
}

func (dm *DotnetModule) GetArgAndFlags() []string {
	return dm.argAndFlags
}

func (dm *DotnetModule) GetName() string {
	return dm.name
}

func (dm *DotnetModule) GetSubcommand() string {
	return dm.subCommand
}

func (dm *DotnetModule) GetSolutionPath() string {
	return dm.solutionPath
}

func (dm *DotnetModule) GetToolchainType() dotnet.ToolchainType {
	return dm.toolchainType
}

// CalcDependencies exec all type of dotnet commands - install, update, add, restore.
// Collects the dotnet project's dependencies and saves them in the build-info module.
func (dm *DotnetModule) CalcDependencies() error {
	err := dm.runCmd()
	if err != nil {
		return err
	}
	if !dm.containingBuild.buildNameAndNumberProvided() {
		return nil
	}
	slnFile, err := dm.updateSolutionPathAndGetFileName()
	if err != nil {
		return err
	}
	sol, err := solution.Load(dm.solutionPath, slnFile, "", dm.containingBuild.logger)
	if err != nil {
		return err
	}
	buildInfo, err := sol.BuildInfo(dm.name, dm.containingBuild.logger)
	if err != nil {
		return err
	}
	return dm.containingBuild.SaveBuildInfo(buildInfo)
}

// Prepares the dotnet/nuget configuration file within the temp directory
// Runs nuget/dotnet itself with the arguments and flags provided.
func (dm *DotnetModule) runCmd() error {
	cmd, err := dm.createCmd()
	if err != nil {
		return err
	}
	// To prevent NuGet prompting for credentials
	err = os.Setenv("NUGET_EXE_NO_PROMPT", "true")
	if err != nil {
		return err
	}

	err = io.RunCmd(cmd)
	if err != nil {
		return err
	}

	return nil
}

func (dm *DotnetModule) createCmd() (*dotnet.Cmd, error) {
	c, err := dotnet.NewToolchainCmd(dm.toolchainType)
	if err != nil {
		return nil, err
	}
	if dm.subCommand != "" {
		c.Command = append(c.Command, strings.Split(dm.subCommand, " ")...)
	}
	c.CommandFlags = dm.argAndFlags
	return c, nil
}

func (dm *DotnetModule) updateSolutionPathAndGetFileName() (string, error) {
	// The path argument wasn't provided, sln file will be searched under working directory.
	if len(dm.argAndFlags) == 0 || strings.HasPrefix(dm.argAndFlags[0], "-") {
		return "", nil
	}
	cmdFirstArg := dm.argAndFlags[0]
	exist, err := utils.IsDirExists(cmdFirstArg, false)
	if err != nil {
		return "", err
	}
	// The path argument is a directory. sln/project file will be searched under this directory.
	if exist {
		dm.updateSolutionPath(cmdFirstArg)
		return "", err
	}
	exist, err = utils.IsFileExists(cmdFirstArg, false)
	if err != nil {
		return "", err
	}
	if exist {
		// The path argument is a .sln file.
		if strings.HasSuffix(cmdFirstArg, ".sln") {
			dm.updateSolutionPath(filepath.Dir(cmdFirstArg))
			return filepath.Base(cmdFirstArg), nil
		}
		// The path argument is a .*proj/packages.config file.
		if strings.HasSuffix(filepath.Ext(cmdFirstArg), "proj") || strings.HasSuffix(cmdFirstArg, "packages.config") {
			dm.updateSolutionPath(filepath.Dir(cmdFirstArg))
		}
	}
	return "", nil
}

func (dm *DotnetModule) updateSolutionPath(slnRootPath string) {
	if filepath.IsAbs(slnRootPath) {
		dm.solutionPath = slnRootPath
	} else {
		dm.solutionPath = filepath.Join(dm.solutionPath, slnRootPath)
	}
}
