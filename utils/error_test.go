package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsForbiddenOutputForChocolatey(t *testing.T) {
	assert.True(t, IsForbiddenOutput(Choco, "The remote server returned an error: (403) Forbidden."))
	assert.True(t, IsForbiddenOutput("nuget", "Response status code does not indicate success: 403 (Forbidden)."))
	assert.False(t, IsForbiddenOutput(Choco, "package was not found"))
}

// TestIsForbiddenOutputPSResource closes a real test-coverage gap: the "psresource" branch of
// IsForbiddenOutput - the only branch in this function with a broad, bare substring match ("403" or
// "forbidden" anywhere in the output) rather than a fixed multi-word phrase like every other package
// manager uses - had no test at all.
func TestIsForbiddenOutputPSResource(t *testing.T) {
	tests := []struct {
		name      string
		cmdOutput string
		want      bool
	}{
		{
			name:      "a real Artifactory 403 response is recognized",
			cmdOutput: "Invoke-WebRequest: The remote server returned an error: (403) Forbidden.",
			want:      true,
		},
		{
			name:      "case-insensitive match",
			cmdOutput: "REMOTE SERVER RETURNED (403) FORBIDDEN",
			want:      true,
		},
		{
			name:      "bare 403 with no other context still matches (this is the broad-match tradeoff)",
			cmdOutput: "unexpected response, code 403",
			want:      true,
		},
		{
			name:      "an unrelated success message must not match",
			cmdOutput: "Successfully installed module Foo, version 2.5.1",
			want:      false,
		},
		{
			name:      "empty output does not match",
			cmdOutput: "",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsForbiddenOutput(PSResource, tc.cmdOutput); got != tc.want {
				t.Errorf("IsForbiddenOutput(PSResource, %q) = %v, want %v", tc.cmdOutput, got, tc.want)
			}
		})
	}
}
