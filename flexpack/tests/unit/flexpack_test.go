package unit

import (
	"os"
	"testing"

	"github.com/jfrog/build-info-go/flexpack"
)

func TestIsFlexPackEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected bool
	}{
		// Valid true values
		{
			name:     "true lowercase returns true",
			envValue: "true",
			setEnv:   true,
			expected: true,
		},
		{
			name:     "TRUE uppercase returns true",
			envValue: "TRUE",
			setEnv:   true,
			expected: true,
		},
		{
			name:     "True mixed case returns true",
			envValue: "True",
			setEnv:   true,
			expected: true,
		},
		{
			name:     "1 returns true",
			envValue: "1",
			setEnv:   true,
			expected: true,
		},
		{
			name:     "t returns true",
			envValue: "t",
			setEnv:   true,
			expected: true,
		},
		{
			name:     "T returns true",
			envValue: "T",
			setEnv:   true,
			expected: true,
		},
		// Valid false values
		{
			name:     "false lowercase returns false",
			envValue: "false",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "FALSE uppercase returns false",
			envValue: "FALSE",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "False mixed case returns false",
			envValue: "False",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "0 returns false",
			envValue: "0",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "f returns false",
			envValue: "f",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "F returns false",
			envValue: "F",
			setEnv:   true,
			expected: false,
		},
		// Invalid values should default to false
		{
			name:     "invalid value returns false",
			envValue: "invalid",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "empty string returns false",
			envValue: "",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "yes returns false (not valid for ParseBool)",
			envValue: "yes",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "no returns false (not valid for ParseBool)",
			envValue: "no",
			setEnv:   true,
			expected: false,
		},
		{
			name:     "whitespace returns false",
			envValue: " ",
			setEnv:   true,
			expected: false,
		},
		// Env not set
		{
			name:     "env not set returns false",
			envValue: "",
			setEnv:   false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env before each test
			_ = os.Unsetenv("JFROG_RUN_NATIVE")

			if tt.setEnv {
				_ = os.Setenv("JFROG_RUN_NATIVE", tt.envValue)
			}

			result := flexpack.IsFlexPackEnabled()

			if result != tt.expected {
				t.Errorf("IsFlexPackEnabled() = %v, want %v for env value %q", result, tt.expected, tt.envValue)
			}

			// Clean up after test
			_ = os.Unsetenv("JFROG_RUN_NATIVE")
		})
	}
}

func TestIsFlexPackEnabledEnvPersistence(t *testing.T) {
	// Ensure clean state
	_ = os.Unsetenv("JFROG_RUN_NATIVE")

	// Test that the function doesn't modify env state
	t.Run("function does not modify environment", func(t *testing.T) {
		_ = os.Setenv("JFROG_RUN_NATIVE", "true")

		// Call function multiple times
		result1 := flexpack.IsFlexPackEnabled()
		result2 := flexpack.IsFlexPackEnabled()

		if result1 != result2 {
			t.Errorf("IsFlexPackEnabled() returned different results: %v vs %v", result1, result2)
		}

		// Verify env is unchanged
		envValue := os.Getenv("JFROG_RUN_NATIVE")
		if envValue != "true" {
			t.Errorf("Environment variable was modified: expected 'true', got %q", envValue)
		}

		_ = os.Unsetenv("JFROG_RUN_NATIVE")
	})
}
