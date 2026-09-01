package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripSurroundingQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "value", expected: "value"},
		{input: "test test", expected: "test test"},
		{input: "'test test'", expected: "test test"},
		{input: `"value with spaces"`, expected: "value with spaces"},
		// Mismatched quote pair: left as-is.
		{input: `'value"`, expected: `'value"`},
		{input: "'", expected: "'"},
		{input: "", expected: ""},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, StripSurroundingQuotes(test.input))
	}
}
