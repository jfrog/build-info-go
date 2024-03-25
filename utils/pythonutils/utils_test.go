package pythonutils

import (
	"fmt"
	"strings"
	"testing"

	gofrogcmd "github.com/jfrog/gofrog/io"
	"github.com/stretchr/testify/assert"
)

func TestGetMultilineCaptureOutputPattern(t *testing.T) {
	tests := []struct {
		name                string
		text                string
		startCapturePattern string
		captureGroupPattern string
		endCapturePattern   string
		expectedCapture     string
	}{
		{
			name:                "Using cached - single line captures",
			startCapturePattern: startUsingCachedPattern,
			captureGroupPattern: usingCacheCaptureGroup,
			endCapturePattern:   endPattern,
			text: `
Looking in indexes: 
***localhost:8081/artifactory/api/pypi/cli-pipenv-pypi-virtual-1698829624/simple
			
Collecting pexpect==4.8.0 (from -r /tmp/pipenv-qzun2hd3-requirements/pipenv-o_899oue-hashed-reqs.txt (line 1))
			
  Using cached http://localhost:8081/artifactory/api/pypi/cli-pipenv-pypi-virtual-1698829624/packages/packages/39/7b/88dbb785881c28a102619d46423cb853b46dbccc70d3ac362d99773a78ce/pexpect-4.8.0-py2.py3-none-any.whl (59 kB)`,
			expectedCapture: `pexpect-4.8.0-py2.py3-none-any.whl`,
		},
		{
			name:                "Using cached - multi line captures",
			startCapturePattern: startUsingCachedPattern,
			captureGroupPattern: usingCacheCaptureGroup,
			endCapturePattern:   endPattern,
			text: `
Looking in indexes: 
***localhost:8081/artifactory/api/pypi/cli-pipenv-pypi-virtual-16
98829624/simple
			
Collecting pexpect==4.8.0 (from -r 
/tmp/pipenv-qzun2hd3-requirements/pipenv-o_899oue-hashed-reqs.txt (line 1))
			
  Using cached 
http://localhost:8081/artifactory/api/pypi/cli-pipenv-pypi-virtual-1698829624/pa
ckages/packages/39/7b/88dbb785881c28a102619d46423cb853b46dbccc70d3ac362d99773a78
ce/pexpect-4.8.0-py2.py3-none-any.whl (59 kB)`,
			expectedCapture: `pexpect-4.8.0-py2.py3-none-any.whl`,
		},
		{
			name:                "Downloading - single line captures",
			startCapturePattern: startDownloadingPattern,
			captureGroupPattern: downloadingCaptureGroup,
			endCapturePattern:   endPattern,
			text: `  Preparing metadata (pyproject.toml): finished with status 'done'
Collecting PyYAML==5.1.2 (from jfrog-python-example==1.0)
  Downloading http://localhost:8081/artifactory/api/pypi/cli-pypi-virtual-1698829558/packages/packages/e3/e8/b3212641ee2718d556df0f23f78de8303f068fe29cdaa7a91018849582fe/PyYAML-5.1.2.tar.gz (265 kB)
	 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 265.0/265.0 kB 364.4 MB/s eta 0:00:00
Installing build dependencies: started`,
			expectedCapture: `PyYAML-5.1.2.tar.gz`,
		},
		{
			name:                "Downloading - multi line captures",
			startCapturePattern: startDownloadingPattern,
			captureGroupPattern: downloadingCaptureGroup,
			endCapturePattern:   endPattern,
			text: `  Preparing metadata (pyproject.toml): finished with status 'done'
Collecting PyYAML==5.1.2 (from jfrog-python-example==1.0)
  Downloading http://localhost:8081/artifactory/api/pypi/cli-pypi-virtual-1698
829558/packages/packages/e3/e8/b3212641ee2718d556df0f23f78de8303f068fe29cdaa7a91018849
582fe/PyYAML-5.1.2.tar.gz (265 kB)
	 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 265.0/265.0 kB 364.4 MB/s eta 0:00:00
			  Installing build dependencies: started`,
			expectedCapture: `PyYAML-5.1.2.tar.gz`,
		},
		{
			name:                "Downloading - multi line captures",
			startCapturePattern: startDownloadingPattern,
			captureGroupPattern: downloadingCaptureGroup,
			endCapturePattern:   endPattern,
			text: `  Preparing metadata (pyproject.toml): finished with status 'done'
Collecting PyYAML==5.1.2 (from jfrog-python-example==1.0)
  Downloading http://localhost:8081/artifactory/api/pypi/cli-pypi-virtual-1698
829558/packages/packages/e3/e8/b3212641ee2718d556df0f23f78de8303f068fe29cdaa7a91018849
582fe/PyYAML-5.1.2%2Bsp1.tar.gz (265 kB)
	 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 265.0/265.0 kB 364.4 MB/s eta 0:00:00
			  Installing build dependencies: started`,
			expectedCapture: `PyYAML-5.1.2+sp1.tar.gz`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			aggFunc, captures := getCapturesFromTest(testCase.expectedCapture)
			runDummyTextStream(t, testCase.text, getMultilineSplitCaptureOutputPattern(
				testCase.startCapturePattern,
				testCase.captureGroupPattern,
				testCase.endCapturePattern,
				aggFunc,
			))
			assert.Len(t, (*captures), 1, fmt.Sprintf("Expected 1 captured group, got size: %d", len(*captures)))
			assert.Equal(t, testCase.expectedCapture, (*captures)[0], fmt.Sprintf("Expected capture group: %s, got: %s", testCase.expectedCapture, (*captures)[0]))
		})
	}
}

func getCapturesFromTest(expectedCaptures ...string) (func(pattern *gofrogcmd.CmdOutputPattern) (string, error), *[]string) {
	captures := []string{}
	aggFunc := func(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
		captured := extractFileNameFromRegexCaptureGroup(pattern)
		for _, expectedCapture := range expectedCaptures {
			if expectedCapture == captured {
				captures = append(captures, expectedCapture)
			}
		}
		return pattern.Line, nil
	}
	return aggFunc, &captures
}

func runDummyTextStream(t *testing.T, txt string, parsers []*gofrogcmd.CmdOutputPattern) {
	// tokenize the text to be represented line by line to simulate expected cmd log output
	lines := strings.Split(txt, "\n")
	// iterate over the lines to simulate line text stream
	for _, line := range lines {
		for _, parser := range parsers {
			// check if the line matches the regexp of the parser
			if parser.RegExp.MatchString(line) {
				parser.MatchedResults = parser.RegExp.FindStringSubmatch(line)
				parser.Line = line
				// execute the parser function
				_, scannerError := parser.ExecFunc(parser)
				assert.NoError(t, scannerError)
			}
		}
	}
}
