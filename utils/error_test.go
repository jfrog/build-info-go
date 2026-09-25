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
