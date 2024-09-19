package pythonutils

import (
	gofrogcmd "github.com/jfrog/gofrog/io"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTwineUploadCapture(t *testing.T) {
	tests := []struct {
		name             string
		text             string
		expectedCaptures []string
	}{
		{
			name: "verbose true",
			text: `
Uploading distributions to https://myplatform.jfrog.io/artifactory/api/pypi/twine-local/
INFO     dist/jfrog_python_example-1.0-py3-none-any.whl (1.6 KB)
INFO     dist/jfrog_python_example-1.0.tar.gz (2.4 KB)
INFO     username set by command options
INFO     password set by command options
INFO     username: user
INFO     password: <hidden>
Uploading jfrog_python_example-1.0-py3-none-any.whl
100% ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 4.5/4.5 kB • 00:00 • ?
INFO     Response from https://myplatform.jfrog.io/artifactory/api/pypi/twine-local/:
         200
Uploading jfrog_python_example-1.0.tar.gz
100% ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 5.3/5.3 kB • 00:00 • ?
INFO     Response from https://myplatform.jfrog.io/artifactory/api/pypi/twine-local/:
         200`,
			expectedCaptures: []string{"dist/jfrog_python_example-1.0-py3-none-any.whl",
				"dist/jfrog_python_example-1.0.tar.gz"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var artifacts []string
			runDummyTextStream(t, testCase.text, []*gofrogcmd.CmdOutputPattern{getArtifactsParser(&artifacts)})
			assert.ElementsMatch(t, artifacts, testCase.expectedCaptures)
		})
	}
}
