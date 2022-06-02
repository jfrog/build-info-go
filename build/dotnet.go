package build

import (
	"errors"
	"fmt"
	buildutils "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/build-info-go/build/utils/dotnet"
	"github.com/jfrog/build-info-go/build/utils/dotnet/solution"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/io"

	"io/ioutil"

	"os"
	"path/filepath"
	"strings"
)

type DotnetModule struct {
	containingBuild *Build
	name            string
	srcPath         string
	//executablePath           string
	dotnetArgs               []string
	traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)
	threads                  int
	packageInfo              *buildutils.PackageInfo

	// from core
	toolchainType     dotnet.ToolchainType
	subCommand        string
	argAndFlags       []string
	repoName          string
	solutionPath      string
	useNugetAddSource bool
	useNugetV2        bool
}

// Pass an empty string for srcPath to find the solutions/proj files in the working directory.
func newDotnetModule(srcPath string, containingBuild *Build) (*DotnetModule, error) {
	if srcPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		srcPath = wd
	}

	return &DotnetModule{srcPath: srcPath, containingBuild: containingBuild, threads: 3, dotnetArgs: []string{"restore"}}, nil
}

func (ym *DotnetModule) SetName(name string) {
	ym.name = name
}

func (ym *DotnetModule) SetArgs(dotnetArgs []string) {
	ym.dotnetArgs = dotnetArgs
}

func (dc *DotnetModule) runDotnetCmd(log utils.Log) (solution.Solution, error) {
	// Use temp dir to save config file, so that config will be removed at the end.
	tempDirPath, err := utils.CreateTempDir()
	if err != nil {
		return nil, err
	}
	defer utils.RemoveTempDir(tempDirPath)

	dc.solutionPath, err = changeWorkingDir(dc.solutionPath)
	if err != nil {
		return nil, err
	}

	err = dc.prepareAndRunCmd(tempDirPath, log)
	if err != nil {
		return nil, err
	}
	if !dc.containingBuild.buildNameAndNumberProvided() {
		return nil, nil
	}

	slnFile, err := dc.updateSolutionPathAndGetFileName()
	if err != nil {
		return nil, err
	}
	return solution.Load(dc.solutionPath, slnFile, log)
}

// Exec all consume type nuget commands, install, update, add, restore.
func (dc *DotnetModule) Build() error {
	sol, err := dc.runDotnetCmd(dc.containingBuild.logger)
	if err != nil {
		return err
	}
	if !dc.containingBuild.buildNameAndNumberProvided() {
		return nil
	}
	buildInfo, err := sol.BuildInfo(dc.name, dc.containingBuild.logger)
	if err != nil {
		return err
	}
	return dc.containingBuild.SaveBuildInfo(buildInfo)
}

// Changes the working directory if provided.
// Returns the path to the solution
func changeWorkingDir(newWorkingDir string) (string, error) {
	var err error
	if newWorkingDir != "" {
		err = os.Chdir(newWorkingDir)
	} else {
		newWorkingDir, err = os.Getwd()
	}

	return newWorkingDir, err
}

// Prepares the nuget configuration file within the temp directory
// Runs NuGet itself with the arguments and flags provided.
func (dc *DotnetModule) prepareAndRunCmd(configDirPath string, log utils.Log) error {
	cmd, err := dc.createCmd()
	if err != nil {
		return err
	}
	// To prevent NuGet prompting for credentials
	err = os.Setenv("NUGET_EXE_NO_PROMPT", "true")
	if err != nil {
		return err
	}

	err = dc.prepareConfigFile(cmd, configDirPath, log)
	if err != nil {
		return err
	}
	err = io.RunCmd(cmd)
	if err != nil {
		return err
	}

	return nil
}

// Checks if the user provided input such as -configfile flag or -Source flag.
// If those flags were provided, NuGet will use the provided configs (default config file or the one with -configfile)
// If neither provided, we are initializing our own config.
func (dc *DotnetModule) prepareConfigFile(cmd *dotnet.Cmd, configDirPath string, log utils.Log) error {
	cmdFlag := cmd.GetToolchain().GetTypeFlagPrefix() + "configfile"
	currentConfigPath, err := getFlagValueIfExists(cmdFlag, cmd)
	if err != nil {
		return err
	}
	if currentConfigPath != "" {
		return nil
	}

	cmdFlag = cmd.GetToolchain().GetTypeFlagPrefix() + "source"
	sourceCommandValue, err := getFlagValueIfExists(cmdFlag, cmd)
	if err != nil {
		return err
	}
	if sourceCommandValue != "" {
		return nil
	}

	// TODO: take care' create config file using rt url, should be provided in core in setargs probably.
	configFile, err := dc.InitNewConfig(configDirPath, log)
	if err == nil {
		cmd.CommandFlags = append(cmd.CommandFlags, cmd.GetToolchain().GetTypeFlagPrefix()+"configfile", configFile.Name())
	}
	return err
}

// Got to here, means that neither of the flags provided and we need to init our own config.
func (dc *DotnetModule) InitNewConfig(configDirPath string, log utils.Log) (configFile *os.File, err error) {
	// Initializing a new NuGet config file that NuGet will use into a temp file
	configFile, err = ioutil.TempFile(configDirPath, "jfrog.cli.nuget.")
	if err != nil {
		return
	}
	log.Debug("Nuget config file created at:", configFile.Name())
	defer func() {
		e := configFile.Close()
		if err == nil {
			err = e
		}
	}()

	// We will prefer to write the NuGet configuration using the `nuget add source` command (addSourceToNugetConfig)
	// Currently the NuGet configuration utility doesn't allow setting protocolVersion.
	// Until that is supported, the templated method must be used.
	err = dc.addSourceToNugetTemplate(configFile)
	return
}

func (dc *DotnetModule) updateSolutionPathAndGetFileName() (string, error) {
	// The path argument wasn't provided, sln file will be searched under working directory.
	if len(dc.argAndFlags) == 0 || strings.HasPrefix(dc.argAndFlags[0], "-") {
		return "", nil
	}
	cmdFirstArg := dc.argAndFlags[0]
	exist, err := utils.IsDirExists(cmdFirstArg, false)
	if err != nil {
		return "", err
	}
	// The path argument is a directory. sln/project file will be searched under this directory.
	if exist {
		dc.updateSolutionPath(cmdFirstArg)
		return "", err
	}
	exist, err = utils.IsFileExists(cmdFirstArg, false)
	if err != nil {
		return "", err
	}
	if exist {
		// The path argument is a .sln file.
		if strings.HasSuffix(cmdFirstArg, ".sln") {
			dc.updateSolutionPath(filepath.Dir(cmdFirstArg))
			return filepath.Base(cmdFirstArg), nil
		}
		// The path argument is a .*proj/packages.config file.
		if strings.HasSuffix(filepath.Ext(cmdFirstArg), "proj") || strings.HasSuffix(cmdFirstArg, "packages.config") {
			dc.updateSolutionPath(filepath.Dir(cmdFirstArg))
		}
	}
	return "", nil
}

func (dc *DotnetModule) updateSolutionPath(slnRootPath string) {
	if filepath.IsAbs(slnRootPath) {
		dc.solutionPath = slnRootPath
	} else {
		dc.solutionPath = filepath.Join(dc.solutionPath, slnRootPath)
	}
}

// Returns the value of the flag if exists
func getFlagValueIfExists(cmdFlag string, cmd *dotnet.Cmd) (string, error) {
	for i := 0; i < len(cmd.CommandFlags); i++ {
		if !strings.EqualFold(cmd.CommandFlags[i], cmdFlag) {
			continue
		}
		if i+1 == len(cmd.CommandFlags) {
			return "", errors.New(fmt.Sprintf("%s flag was provided without value", cmdFlag))
		}
		return cmd.CommandFlags[i+1], nil
	}

	return "", nil
}

// Adds a source to the nuget config template
func (dc *DotnetModule) addSourceToNugetTemplate(configFile *os.File) error {
	sourceUrl, user, password, err := dc.getSourceDetails()
	if err != nil {
		return err
	}

	// Specify the protocolVersion
	protoVer := "3"
	if dc.useNugetV2 {
		protoVer = "2"
	}

	// Format the templates
	_, err = fmt.Fprintf(configFile, dotnet.ConfigFileFormat, sourceUrl, protoVer, user, password)
	return err
}

func (dc *DotnetModule) getSourceDetails() (sourceURL, user, password string, err error) {
	//var u *url.URL
	// TODO: check
	//u, err = url.Parse(dc.serverDetails.ArtifactoryUrl)
	//if errorutils.CheckError(err) != nil {
	//	return
	//}
	//nugetApi := "api/nuget/v3"
	//if dc.useNugetV2 {
	//	nugetApi = "api/nuget"
	//}
	//u.Path = path.Join(u.Path, nugetApi, dc.repoName)
	//sourceURL = u.String()
	//
	//user = dc.serverDetails.User
	//password = dc.serverDetails.Password
	//// If access-token is defined, extract user from it.
	//serverDetails, err := dc.ServerDetails()
	//if errorutils.CheckError(err) != nil {
	//	return
	//}
	//if serverDetails.AccessToken != "" {
	//	log.Debug("Using access-token details for nuget authentication.")
	//	user, err = auth.ExtractUsernameFromAccessToken(serverDetails.AccessToken)
	//	if err != nil {
	//		return
	//	}
	//	password = serverDetails.AccessToken
	//}
	return
}

func (dc *DotnetModule) createCmd() (*dotnet.Cmd, error) {
	c, err := dotnet.NewToolchainCmd(dc.toolchainType)
	if err != nil {
		return nil, err
	}
	if dc.subCommand != "" {
		c.Command = append(c.Command, strings.Split(dc.subCommand, " ")...)
	}
	c.CommandFlags = dc.argAndFlags
	return c, nil
}

//// Build builds the project, collects its dependencies and saves them in the build-info module.
//func (ym *DotnetModule) Build() error {
//	err := runYarnCommand(ym.executablePath, ym.srcPath, ym.yarnArgs...)
//	if err != nil {
//		return err
//	}
//	if !ym.containingBuild.buildNameAndNumberProvided() {
//		return nil
//	}
//	dependenciesMap, err := ym.getDependenciesMap()
//	if err != nil {
//		return err
//	}
//	buildInfoDependencies, err := buildutils.TraverseDependencies(dependenciesMap, ym.traverseDependenciesFunc, ym.threads)
//	if err != nil {
//		return err
//	}
//	buildInfoModule := entities.Module{Id: ym.name, Type: entities.Npm, Dependencies: buildInfoDependencies}
//	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}
//	return ym.containingBuild.SaveBuildInfo(buildInfo)
//}
//
//func (ym *DotnetModule) getDependenciesMap() (map[string]*entities.Dependency, error) {
//	// Run 'yarn info'
//	responseStr, errStr, err := runInfo(ym.executablePath, ym.srcPath)
//	// Some warnings and messages of Yarn are printed to stderr. They don't necessarily cause the command to fail, but we'd want to show them to the user.
//	if len(errStr) > 0 {
//		ym.containingBuild.logger.Warn("Some errors occurred while collecting dependencies info:\n" + errStr)
//	}
//	if err != nil {
//		ym.containingBuild.logger.Warn("An error was thrown while collecting dependencies info:", err.Error())
//		// A returned error doesn't necessarily mean that the operation totally failed. If, in addition, the response is empty, then it probably does.
//		if responseStr == "" {
//			return nil, err
//		}
//	}
//
//	dependenciesMap := make(map[string]*YarnDependency)
//	scanner := bufio.NewScanner(strings.NewReader(responseStr))
//	packageName := ym.packageInfo.FullName()
//	var root *YarnDependency
//
//	for scanner.Scan() {
//		var currDependency YarnDependency
//		currDepBytes := scanner.Bytes()
//		err = json.Unmarshal(currDepBytes, &currDependency)
//		if err != nil {
//			return nil, err
//		}
//		dependenciesMap[currDependency.Value] = &currDependency
//
//		// Check whether this dependency's name starts with the package name (which means this is the root)
//		if strings.HasPrefix(currDependency.Value, packageName+"@") {
//			root = &currDependency
//		}
//	}
//
//	buildInfoDependencies := make(map[string]*entities.Dependency)
//	err = ym.appendDependencyRecursively(root, []string{}, dependenciesMap, buildInfoDependencies)
//	return buildInfoDependencies, err
//}
//
//func (ym *DotnetModule) appendDependencyRecursively(yarnDependency *YarnDependency, pathToRoot []string, yarnDependenciesMap map[string]*YarnDependency,
//	buildInfoDependencies map[string]*entities.Dependency) error {
//	name := yarnDependency.Name()
//	var ver string
//	if len(pathToRoot) == 0 {
//		// The version of the local project returned from 'yarn info' is '0.0.0-use.local', but we need the version mentioned in package.json
//		ver = ym.packageInfo.Version
//	} else {
//		ver = yarnDependency.Details.Version
//	}
//	id := name + ":" + ver
//
//	// To avoid infinite loops in case of circular dependencies, the dependency won't be added if it's already in pathToRoot
//	if stringsSliceContains(pathToRoot, id) {
//		return nil
//	}
//
//	for _, dependencyPtr := range yarnDependency.Details.Dependencies {
//		innerDepKey := getYarnDependencyKeyFromLocator(dependencyPtr.Locator)
//		innerYarnDep, exist := yarnDependenciesMap[innerDepKey]
//		if !exist {
//			return fmt.Errorf("an error occurred while creating dependencies tree: dependency %s was not found", dependencyPtr.Locator)
//		}
//		err := ym.appendDependencyRecursively(innerYarnDep, append([]string{id}, pathToRoot...), yarnDependenciesMap,
//			buildInfoDependencies)
//		if err != nil {
//			return err
//		}
//	}
//
//	// The root project should not be added to the dependencies list
//	if len(pathToRoot) == 0 {
//		return nil
//	}
//
//	buildInfoDependency, exist := buildInfoDependencies[id]
//	if !exist {
//		buildInfoDependency = &entities.Dependency{Id: id}
//		buildInfoDependencies[id] = buildInfoDependency
//	}
//
//	buildInfoDependency.RequestedBy = append(buildInfoDependency.RequestedBy, pathToRoot)
//	return nil
//}
//

//

//
//func (ym *DotnetModule) SetThreads(threads int) {
//	ym.threads = threads
//}
//
//// SetTraverseDependenciesFunc gets a function to execute on all dependencies after their collection in Build(), before they're saved.
//// This function needs to return a boolean value indicating whether to save this dependency in the build-info or not.
//// This function might run asynchronously with different dependencies (if the threads amount setting is bigger than 1).
//// If more than one error are returned from this function in different threads, only the first of them will be returned from Build().
//func (ym *DotnetModule) SetTraverseDependenciesFunc(traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)) {
//	ym.traverseDependenciesFunc = traverseDependenciesFunc
//}
//
//func (ym *DotnetModule) AddArtifacts(artifacts ...entities.Artifact) error {
//	if !ym.containingBuild.buildNameAndNumberProvided() {
//		return errors.New("a build name must be provided in order to add artifacts")
//	}
//	partial := &entities.Partial{ModuleId: ym.name, ModuleType: entities.Npm, Artifacts: artifacts}
//	return ym.containingBuild.SavePartialBuildInfo(partial)
//}
//
//type DotnetDependency struct {
//	// The value is usually in this structure: @scope/package-name@npm:1.0.0
//	Value   string         `json:"value,omitempty"`
//	Details YarnDepDetails `json:"children,omitempty"`
//}
//
//func (yd *DotnetDependency) Name() string {
//	// Find the first index of '@', starting from position 1. In scoped dependencies (like '@jfrog/package-name@npm:1.2.3') we want to keep the first '@' as part of the name.
//	atSignIndex := strings.Index(yd.Value[1:], "@") + 1
//	return yd.Value[:atSignIndex]
//}
//
//type DotnetDepDetails struct {
//	Version      string                    `json:"Version,omitempty"`
//	Dependencies []DotnetDependencyPointer `json:"Dependencies,omitempty"`
//}
//
//type DotnetDependencyPointer struct {
//	Descriptor string `json:"descriptor,omitempty"`
//	Locator    string `json:"locator,omitempty"`
//}
//
//func getDotnetExecutable() (string, error) {
//	yarnExecPath, err := exec.LookPath("yarn")
//	if err != nil {
//		return "", err
//	}
//	return yarnExecPath, nil
//}
//
//func validateDotnetVersion(executablePath, srcPath string) error {
//	yarnVersionStr, err := getVersion(executablePath, srcPath)
//	if err != nil {
//		return err
//	}
//	yarnVersion := version.NewVersion(yarnVersionStr)
//	if yarnVersion.Compare(minSupportedYarnVersion) > 0 {
//		return errors.New("Yarn must have version " + minSupportedYarnVersion + " or higher. The current version is: " + yarnVersionStr)
//	}
//	return nil
//}
//
//func getDotnetVersion(executablePath, srcPath string) (string, error) {
//	command := exec.Command(executablePath, "--version")
//	command.Dir = srcPath
//	outBuffer := bytes.NewBuffer([]byte{})
//	command.Stdout = outBuffer
//	command.Stderr = os.Stderr
//	err := command.Run()
//	if _, ok := err.(*exec.ExitError); ok {
//		err = errors.New(err.Error())
//	}
//	return strings.TrimSpace(outBuffer.String()), err
//}
//
//// Yarn dependency locator usually looks like this: package-name@npm:1.2.3, which is used as the key in the dependencies map.
//// But sometimes it points to a virtual package, so it looks different: package-name@virtual:[ID of virtual package]#npm:1.2.3.
//// In this case we need to omit the part of the virtual package ID, to get the key as it is found in the dependencies map.
//func getYarnDependencyKeyFromLocator(yarnDepLocator string) string {
//	virtualIndex := strings.Index(yarnDepLocator, "@virtual:")
//	if virtualIndex == -1 {
//		return yarnDepLocator
//	}
//
//	hashSignIndex := strings.LastIndex(yarnDepLocator, "#")
//	return yarnDepLocator[:virtualIndex+1] + yarnDepLocator[hashSignIndex+1:]
//}
//
//func runInfo(executablePath, srcPath string) (outResult, errResult string, err error) {
//	command := exec.Command(executablePath, "info", "--all", "--recursive", "--json")
//	command.Dir = srcPath
//	outBuffer := bytes.NewBuffer([]byte{})
//	command.Stdout = outBuffer
//	errBuffer := bytes.NewBuffer([]byte{})
//	command.Stderr = errBuffer
//	err = command.Run()
//	errResult = errBuffer.String()
//	if err != nil {
//		if _, ok := err.(*exec.ExitError); ok {
//			err = errors.New(err.Error())
//		}
//		return
//	}
//	outResult = strings.TrimSpace(outBuffer.String())
//	return
//}
//
//func runDotnetCommand(executablePath, srcPath string, args ...string) error {
//	command := exec.Command(executablePath, args...)
//	command.Dir = srcPath
//	command.Stdout = os.Stderr
//	command.Stderr = os.Stderr
//	err := command.Run()
//	if _, ok := err.(*exec.ExitError); ok {
//		err = errors.New(err.Error())
//	}
//	return err
//}
//
//func stringsSliceContains(slice []string, str string) bool {
//	for _, element := range slice {
//		if element == str {
//			return true
//		}
//	}
//	return false
//}
