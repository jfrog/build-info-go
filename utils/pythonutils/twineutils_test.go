package pythonutils

import (
	"github.com/stretchr/testify/assert"
	"regexp"
	"strings"
	"testing"
)

func TestMergeTwineWrappedLines(t *testing.T) {
	tests := []struct {
		name              string
		rawOutput         string
		expectedArtifacts []string
	}{
		{
			name: "wrapped artifact lines",
			rawOutput: `
INFO     dist/jfrog_python_example-1.0-py3-none-any.whl (1.6 KB)
INFO     dist/jfrog_python_example-1.0.tar.gz
         (2.4 KB)
INFO     some other non-matching line
INFO     dist/another_package-2.0.whl (3.2 KB)`,
			expectedArtifacts: []string{
				"dist/jfrog_python_example-1.0-py3-none-any.whl",
				"dist/jfrog_python_example-1.0.tar.gz",
				"dist/another_package-2.0.whl",
			},
		},
		{
			name: "empty output",
			rawOutput: `
`,
			expectedArtifacts: []string{},
		},
		{
			name: "malformed output",
			rawOutput: `
INFO     dist/invalid_package
INFO     dist/valid_package-1.2.3.tar.gz (1.5 MB)`,
			expectedArtifacts: []string{
				"dist/valid_package-1.2.3.tar.gz",
			},
		},
	}

	artifactRegex := regexp.MustCompile(`^.+\s([^ \t]+)\s+\([\d.]+\s+[A-Za-z]{2}\)`)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(tc.rawOutput, "\n")
			merged := mergeTwineWrappedLines(lines)

			var artifacts []string
			for _, line := range merged {
				matches := artifactRegex.FindStringSubmatch(line)
				if len(matches) >= 2 {
					path := strings.TrimSpace(matches[1])
					if path != "" {
						artifacts = append(artifacts, path)
					}
				}
			}

			assert.ElementsMatch(t, artifacts, tc.expectedArtifacts)
		})
	}
}
