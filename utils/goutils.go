package utils

import (
	"errors"
	"fmt"
	"github.com/jfrog/gofrog/version"
	"regexp"
	"runtime"

	gofrogcmd "github.com/jfrog/gofrog/io"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const credentialsInUrlRegexp = `(http|https|git)://.+@`

// Minimum go version, which its output does not require masking passwords in URLs.
const minGoVersionForMasking = "go1.13"

// Max go version, which automatically modify go.mod and go.sum when executing build commands.
const maxGoVersionAutomaticallyModifyMod = "go1.15"

// Never use this value, use shouldMaskPassword().
var shouldMask *bool = nil

// Never use this value, use automaticallyModifyMod().
var autoModify *bool = nil

// Used for masking basic auth credentials as part of a URL.
var protocolRegExp *gofrogcmd.CmdOutputPattern

type Cmd struct {
	Go           string
	Command      []string
	CommandFlags []string
	Dir          string
	StrWriter    io.WriteCloser
	ErrWriter    io.WriteCloser
}

func newCmd() (*Cmd, error) {
	execPath, err := exec.LookPath("go")
	if err != nil {
		return nil, err
	}
	return &Cmd{Go: execPath}, nil
}

func (config *Cmd) GetCmd() (cmd *exec.Cmd) {
	var cmdStr []string
	cmdStr = append(cmdStr, config.Go)
	cmdStr = append(cmdStr, config.Command...)
	cmdStr = append(cmdStr, config.CommandFlags...)
	cmd = exec.Command(cmdStr[0], cmdStr[1:]...)
	cmd.Dir = config.Dir
	return
}

func (config *Cmd) GetEnv() map[string]string {
	return map[string]string{}
}

func (config *Cmd) GetStdWriter() io.WriteCloser {
	return config.StrWriter
}

func (config *Cmd) GetErrWriter() io.WriteCloser {
	return config.ErrWriter
}

func RunGo(goArg []string, repoUrl string) error {
	err := os.Setenv("GOPROXY", repoUrl)
	if err != nil {
		return err
	}

	goCmd, err := newCmd()
	if err != nil {
		return err
	}
	goCmd.Command = goArg
	err = prepareGlobalRegExp()
	if err != nil {
		return err
	}

	performPasswordMask, err := shouldMaskPassword()
	if err != nil {
		return err
	}
	if performPasswordMask {
		_, _, _, err = gofrogcmd.RunCmdWithOutputParser(goCmd, true, protocolRegExp)
	} else {
		_, _, _, err = gofrogcmd.RunCmdWithOutputParser(goCmd, true)
	}
	return err
}

// Runs 'go list -m' command and returns module name
func GetModuleNameByDir(projectDir string, log Log) (string, error) {
	if log == nil {
		log = &NullLog{}
	}

	cmdArgs, err := getListCmdArgs()
	if err != nil {
		return "", err
	}
	cmdArgs = append(cmdArgs, "-m")
	output, err := runDependenciesCmd(projectDir, cmdArgs, log)
	if err != nil {
		return "", err
	}
	lineOutput := strings.Split(output, "\n")
	return lineOutput[0], err
}

// Gets go list command args according to go version
func getListCmdArgs() (cmdArgs []string, err error) {
	isAutoModify, err := automaticallyModifyMod()
	if err != nil {
		return []string{}, err
	}
	// Since version go1.16 build commands (like go build and go list) no longer modify go.mod and go.sum by default.
	if isAutoModify {
		return []string{"list"}, nil
	}
	return []string{"list", "-mod=mod"}, nil
}

// Runs go list -f {{with .Module}}{{.Path}}:{{.Version}}{{end}} all command and returns map of the dependencies
func GetDependenciesList(projectDir string, log Log) (map[string]bool, error) {
	cmdArgs, err := getListCmdArgs()
	if err != nil {
		return nil, err
	}
	output, err := runDependenciesCmd(projectDir, append(cmdArgs, "-f", "{{with .Module}}{{.Path}}@{{.Version}}{{end}}", "all"), log)
	if err != nil {
		// Errors occurred while running "go list". Run again and this time ignore errors (with '-e')
		log.Warn("Errors occurred while building the Go dependency tree. The dependency tree may be incomplete:" + err.Error())
		output, err = runDependenciesCmd(projectDir, append(cmdArgs, "-e", "-f", "{{with .Module}}{{.Path}}@{{.Version}}{{end}}", "all"), log)
		if err != nil {
			return nil, err
		}
	}
	return listToMap(output), err
}

// Runs 'go mod graph' command and returns map that maps dependencies to their child dependencies slice
func GetDependenciesGraph(projectDir string, log Log) (map[string][]string, error) {
	output, err := runDependenciesCmd(projectDir, []string{"mod", "graph"}, log)
	if err != nil {
		return nil, err
	}
	return graphToMap(output), err
}

// Common function to run dependencies command for list or graph commands
func runDependenciesCmd(projectDir string, commandArgs []string, log Log) (output string, err error) {
	log.Info(fmt.Sprintf("Running 'go %s' in %s", strings.Join(commandArgs, " "), projectDir))
	if projectDir == "" {
		projectDir, err = GetProjectRoot()
		if err != nil {
			return "", err
		}
	}
	// Read and store the details of the go.mod and go.sum files,
	// because they may change by the 'go mod graph' or 'go list' commands.
	modFileContent, modFileStat, err := GetFileContentAndInfo(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		log.Info("Dependencies were not collected for this build, since go.mod could not be found in", projectDir)
		return "", nil
	}
	sumFileContent, sumFileStat, err := GetFileContentAndInfo(filepath.Join(projectDir, "go.sum"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		defer func() {
			e := ioutil.WriteFile(filepath.Join(projectDir, "go.sum"), sumFileContent, sumFileStat.Mode())
			if err == nil {
				err = e
			}
		}()
	}
	goCmd, err := newCmd()
	if err != nil {
		return "", err
	}
	goCmd.Command = commandArgs
	goCmd.Dir = projectDir

	err = prepareGlobalRegExp()
	if err != nil {
		return "", err
	}
	performPasswordMask, err := shouldMaskPassword()
	if err != nil {
		return "", err
	}
	var executionError error
	if performPasswordMask {
		output, _, _, executionError = gofrogcmd.RunCmdWithOutputParser(goCmd, true, protocolRegExp)
	} else {
		output, _, _, executionError = gofrogcmd.RunCmdWithOutputParser(goCmd, true)
	}
	if len(output) != 0 {
		log.Debug(output)
	}
	if executionError != nil {
		// If the command fails, the mod stays the same, therefore, don't need to be restored.
		errorString := fmt.Sprintf("Failed running Go command: 'go %s' in %s with error: '%s'", strings.Join(commandArgs, " "), projectDir, executionError.Error())
		return "", errors.New(errorString)
	}

	// Restore the go.mod and go.sum files, to make sure they stay the same as before
	// running the "go mod graph" command.
	err = ioutil.WriteFile(filepath.Join(projectDir, "go.mod"), modFileContent, modFileStat.Mode())
	if err != nil {
		return "", err
	}
	return output, err
}

// Returns the root dir where the go.mod located.
func GetProjectRoot() (string, error) {
	// Get the current directory.
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	return FindFileInDirAndParents(wd, "go.mod")
}

// Go performs password redaction from url since version 1.13.
// Only if go version before 1.13, should manually perform password masking.
func shouldMaskPassword() (bool, error) {
	return compareSpecificVersionToCurVersion(shouldMask, minGoVersionForMasking)
}

// Since version go1.16 build commands (like go build and go list) no longer modify go.mod and go.sum by default.
func automaticallyModifyMod() (bool, error) {
	return compareSpecificVersionToCurVersion(autoModify, maxGoVersionAutomaticallyModifyMod)
}

func compareSpecificVersionToCurVersion(result *bool, comparedVersion string) (bool, error) {
	if result == nil {
		goVersion, err := getParsedGoVersion()
		if err != nil {
			return false, err
		}
		autoModifyBool := !goVersion.AtLeast(comparedVersion)
		result = &autoModifyBool
	}

	return *result, nil
}

func getParsedGoVersion() (*version.Version, error) {
	output, err := getGoVersion()
	if err != nil {
		return nil, err
	}
	// Go version output pattern is: 'go version go1.14.1 darwin/amd64'
	// Thus should take the third element.
	splitOutput := strings.Split(output, " ")
	return version.NewVersion(splitOutput[2]), nil
}

func getGoVersion() (string, error) {
	goCmd, err := newCmd()
	if err != nil {
		return "", err
	}
	goCmd.Command = []string{"version"}
	output, err := gofrogcmd.RunCmdOutput(goCmd)
	return output, err
}

// Compiles all the regex once
func prepareGlobalRegExp() error {
	var err error
	if protocolRegExp == nil {
		protocolRegExp, err = initRegExp(credentialsInUrlRegexp, removeCredentials)
		if err != nil {
			return err
		}
	}

	return err
}

func initRegExp(regex string, execFunc func(pattern *gofrogcmd.CmdOutputPattern) (string, error)) (*gofrogcmd.CmdOutputPattern, error) {
	regExp, err := regexp.Compile(regex)
	if err != nil {
		return &gofrogcmd.CmdOutputPattern{}, err
	}

	outputPattern := &gofrogcmd.CmdOutputPattern{
		RegExp: regExp,
	}

	outputPattern.ExecFunc = execFunc
	return outputPattern, nil
}

// Remove the credentials information from the line.
func removeCredentials(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
	splitResult := strings.Split(pattern.MatchedResults[0], "//")
	return strings.Replace(pattern.Line, pattern.MatchedResults[0], splitResult[0]+"//", 1), nil
}

// GetCachePath returns the location of downloads dir inside the GOMODCACHE
func GetCachePath() (string, error) {
	goModCachePath, err := GetGoModCachePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(goModCachePath, "cache", "download"), nil
}

// GetGoModCachePath returns the location of the go module cache
func GetGoModCachePath() (string, error) {
	goPath, err := getGOPATH()
	if err != nil {
		return "", err
	}
	return filepath.Join(goPath, "pkg", "mod"), nil
}

// GetGOPATH returns the location of the GOPATH
func getGOPATH() (string, error) {
	goCmd, err := newCmd()
	if err != nil {
		return "", err
	}
	goCmd.Command = []string{"env", "GOPATH"}
	output, err := gofrogcmd.RunCmdOutput(goCmd)
	if err != nil {
		return "", fmt.Errorf("Could not find GOPATH env: %s", err.Error())
	}
	return strings.TrimSpace(parseGoPath(string(output))), nil
}

func parseGoPath(goPath string) string {
	if runtime.GOOS == "windows" {
		goPathSlice := strings.Split(goPath, ";")
		return goPathSlice[0]
	}
	goPathSlice := strings.Split(goPath, ":")
	return goPathSlice[0]
}

func getGoSum(rootProjectDir string, log Log) (sumFileContent []byte, sumFileStat os.FileInfo, err error) {
	sumFileExists, err := IsFileExists(filepath.Join(rootProjectDir, "go.sum"))
	if err == nil && sumFileExists {
		log.Debug("Sum file exists:", rootProjectDir)
		sumFileContent, sumFileStat, err = GetFileContentAndInfo(filepath.Join(rootProjectDir, "go.sum"))
	}
	return
}

func listToMap(output string) map[string]bool {
	lineOutput := strings.Split(output, "\n")
	mapOfDeps := map[string]bool{}
	for _, line := range lineOutput {
		// The expected syntax : github.com/name@v1.2.3
		if len(strings.Split(line, "@")) == 2 && mapOfDeps[line] == false {
			mapOfDeps[line] = true
			continue
		}
	}
	return mapOfDeps
}

func graphToMap(output string) map[string][]string {
	lineOutput := strings.Split(output, "\n")
	mapOfDeps := map[string][]string{}
	for _, line := range lineOutput {
		// The expected syntax : github.com/parentname@v1.2.3 github.com/childname@v1.2.3
		line = strings.ReplaceAll(line, "@v", ":")
		splitLine := strings.Split(line, " ")
		if len(splitLine) == 2 {
			parent := splitLine[0]
			child := splitLine[1]
			mapOfDeps[parent] = append(mapOfDeps[parent], child)
		}
	}
	return mapOfDeps
}
