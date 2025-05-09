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

	log.Debug(fmt.Sprintf("Running twine command: '%s %s %s' with build info collection in dir '%s'",
		_twineExeName, _twineUploadCmdName, strings.Join(commandArgs, " "), srcPath))

	rawStdout, execErr := gofrogcmd.RunCmdOutput(uploadCmd)

	if execErr != nil {
		return nil, fmt.Errorf("failed running '%s %s %s': %w. Raw stdout (if any): %s",
			_twineExeName, _twineUploadCmdName, strings.Join(commandArgs, " "), execErr, rawStdout)
	}

	if rawStdout != "" {
		rawLinesForDebug := strings.Split(rawStdout, "\n")
		for i, line := range rawLinesForDebug {
			log.Debug(fmt.Sprintf("Raw STDOUT [%04d]: %s", i, line))
		}
	} else {
		log.Debug("Twine command raw stdout was empty.")
	}

	lines := strings.Split(rawStdout, "\n")
	mergedLines := mergeTwineWrappedLines(lines)

	if len(mergedLines) > 0 {
		for i, line := range mergedLines {
			log.Debug(fmt.Sprintf("Merged Line [%04d]: %s", i, line))
		}
	} else {
		log.Debug("No lines after merging (or raw stdout was empty).")
	}

	// Regexp to catch the paths in lines such as "INFO     dist/jfrog_python_example-1.0-py3-none-any.whl (1.6 KB)"
	// First part ".+\s" is the line prefix.
	// Second part "([^ \t]+)" is the artifact path as a group.
	// Third part "\s+\([\d.]+\s+[A-Za-z]{2}\)" is the size and unit, surrounded by parentheses.
	Regexp := regexp.MustCompile(`^.+\s([^ \t]+)\s+\([\d.]+\s+[A-Za-z]{2}\)`)

	for _, line := range mergedLines {
		matches := Regexp.FindStringSubmatch(line)
		if len(matches) >= 2 {
			path := strings.TrimSpace(matches[1])
			if path != "" {
				log.Debug(fmt.Sprintf("Extracted artifact path: '%s' from merged line: '%s'", path, line))
				artifactsPaths = append(artifactsPaths, path)
			} else {
				log.Debug(fmt.Sprintf("Regex matched, but captured path is empty after trim from merged line: '%s'", line))
			}
		}
	}

	if len(artifactsPaths) == 0 && len(strings.TrimSpace(rawStdout)) > 0 {
		log.Debug("No artifact paths extracted from Twine output. This might be expected or indicate a parsing issue with current logic/regex.")
	}

	return artifactsPaths, nil
}

func mergeTwineWrappedLines(lines []string) []string {
	if len(lines) == 0 {
		return []string{}
	}

	var mergedLines []string
	var currentLineBuffer strings.Builder

	currentLineBuffer.WriteString(lines[0])

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, " ") && currentLineBuffer.Len() > 0 {
			currentLineBuffer.WriteString(" ")
			currentLineBuffer.WriteString(strings.TrimSpace(line))
		} else {
			if currentLineBuffer.Len() > 0 {
				mergedLines = append(mergedLines, currentLineBuffer.String())
			}
			currentLineBuffer.Reset()
			currentLineBuffer.WriteString(line)
		}
	}

	if currentLineBuffer.Len() > 0 {
		mergedLines = append(mergedLines, currentLineBuffer.String())
	}

	return mergedLines
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
