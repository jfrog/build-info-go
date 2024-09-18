package pythonutils

import (
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"github.com/jfrog/gofrog/log"
	"path"
	"path/filepath"
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

// Create artifacts entities from the artifacts paths that were found during the upload.
func CreateArtifactsFromPaths(artifactsPaths []string) (artifacts []entities.Artifact, err error) {
	projectName, projectVersion, err := getPipProjectNameAndVersion("")
	if err != nil {
		return
	}
	var absPath string
	var fileDetails *crypto.FileDetails
	for _, artifactPath := range artifactsPaths {
		absPath, err = filepath.Abs(artifactPath)
		if err != nil {
			return nil, err
		}
		fileDetails, err = crypto.GetFileDetails(absPath, true)
		if err != nil {
			return nil, err
		}

		artifact := entities.Artifact{Name: filepath.Base(absPath), Path: path.Join(projectName, projectVersion, filepath.Base(absPath)),
			Type: strings.TrimPrefix(filepath.Ext(absPath), ".")}
		artifact.Checksum = entities.Checksum{Sha1: fileDetails.Checksum.Sha1, Md5: fileDetails.Checksum.Md5}
		artifacts = append(artifacts, artifact)
	}
	return
}
