package pythonutils

import (
	"fmt"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"github.com/jfrog/gofrog/log"
	"regexp"
	"slices"
	"strings"
)

const (
	_twineExeName        = "twine"
	_twineUploadCmdName  = "upload"
	_verboseFlag         = "--verbose"
	_disableProgressFlag = "--disable-progress-bar"
)

// Run a twine upload and parse artifacts paths from logs.
func TwineUploadWithLogParsing(commandArgs []string, srcPath string) (artifactsPaths []string, err error) {
	commandArgs = addRequiredFlags(commandArgs)
	uploadCmd := gofrogcmd.NewCommand(_twineExeName, _twineUploadCmdName, commandArgs)
	uploadCmd.Dir = srcPath
	log.Debug("Running twine command: '", _twineExeName, _twineUploadCmdName, strings.Join(commandArgs, " "), "'with build info collection")
	_, errorOut, _, err := gofrogcmd.RunCmdWithOutputParser(uploadCmd, true, getArtifactsParser(&artifactsPaths))
	if err != nil {
		return nil, fmt.Errorf("failed running '%s %s %s' command with error: '%s - %s'", _twineExeName, _twineUploadCmdName, strings.Join(commandArgs, " "), err.Error(), errorOut)
	}
	return
}

// Enabling verbose and disabling progress bar are required for log parsing.
func addRequiredFlags(commandArgs []string) []string {
	for _, flag := range []string{_verboseFlag, _disableProgressFlag} {
		if !slices.Contains(commandArgs, flag) {
			commandArgs = append(commandArgs, flag)
		}
	}
	return commandArgs
}

func getArtifactsParser(artifactsPaths *[]string) (parser *gofrogcmd.CmdOutputPattern) {
	return &gofrogcmd.CmdOutputPattern{
		// Regexp to catch the paths in lines such as "INFO     dist/jfrog_python_example-1.0-py3-none-any.whl (1.6 KB)"
		// First part ".+\s" is the line prefix.
		// Second part "([^ \t]+)" is the artifact path as a group.
		// Third part "\s+\([\d.]+\s+[A-Za-z]{2}\)" is the size and unit, surrounded by parentheses.
		RegExp: regexp.MustCompile(`^.+\s([^ \t]+)\s+\([\d.]+\s+[A-Za-z]{2}\)`),
		ExecFunc: func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
			// Check for out of bound results.
			if len(pattern.MatchedResults)-1 <= 0 {
				log.Debug(fmt.Sprintf("Failed extracting artifact name from line: %s", pattern.Line))
				return pattern.Line, nil
			}
			*artifactsPaths = append(*artifactsPaths, pattern.MatchedResults[1])
			return pattern.Line, nil
		},
	}
}
