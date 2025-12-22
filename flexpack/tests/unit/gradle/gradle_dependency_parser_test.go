package unit

import (
	"testing"

	gradleflexpack "github.com/jfrog/build-info-go/flexpack/gradle"
	"github.com/stretchr/testify/assert"
)

// TestMergeScopes tests the MergeScopes helper function
func TestMergeScopes(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		new      []string
		expected []string
	}{
		{
			name:     "merge into empty",
			existing: []string{},
			new:      []string{"compile", "runtime"},
			expected: []string{"compile", "runtime"},
		},
		{
			name:     "merge new into existing",
			existing: []string{"compile"},
			new:      []string{"runtime", "test"},
			expected: []string{"compile", "runtime", "test"},
		},
		{
			name:     "no duplicates",
			existing: []string{"compile", "runtime"},
			new:      []string{"compile", "test"},
			expected: []string{"compile", "runtime", "test"},
		},
		{
			name:     "all duplicates",
			existing: []string{"compile", "runtime"},
			new:      []string{"compile", "runtime"},
			expected: []string{"compile", "runtime"},
		},
		{
			name:     "empty new",
			existing: []string{"compile", "runtime"},
			new:      []string{},
			expected: []string{"compile", "runtime"},
		},
		{
			name:     "both empty",
			existing: []string{},
			new:      []string{},
			expected: []string{},
		},
		{
			name:     "result is sorted",
			existing: []string{"test", "compile"},
			new:      []string{"runtime"},
			expected: []string{"compile", "runtime", "test"},
		},
		{
			name:     "unsorted input gets sorted",
			existing: []string{"runtime"},
			new:      []string{"compile"},
			expected: []string{"compile", "runtime"},
		},
		{
			name:     "single scope each",
			existing: []string{"compile"},
			new:      []string{"runtime"},
			expected: []string{"compile", "runtime"},
		},
		{
			name:     "nil existing treated as empty",
			existing: nil,
			new:      []string{"compile"},
			expected: []string{"compile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gradleflexpack.MergeScopes(tt.existing, tt.new)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMergeScopesIdempotent tests that calling MergeScopes multiple times produces same result
func TestMergeScopesIdempotent(t *testing.T) {
	existing := []string{"compile"}
	new := []string{"runtime", "test"}

	result1 := gradleflexpack.MergeScopes(existing, new)
	result2 := gradleflexpack.MergeScopes(result1, new)

	assert.Equal(t, result1, result2, "merging same scopes twice should be idempotent")
}

