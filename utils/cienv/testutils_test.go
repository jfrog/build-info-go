package cienv

import (
	"os"
	"testing"
)

// setEnvForTest sets an environment variable and registers cleanup to restore the original value.
// This is a wrapper that handles error checking for os.Setenv.
func setEnvForTest(t *testing.T, key, value string) {
	t.Helper()
	origVal, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Failed to set env var %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			if err := os.Setenv(key, origVal); err != nil {
				t.Errorf("Failed to restore env var %s: %v", key, err)
			}
		} else {
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("Failed to unset env var %s: %v", key, err)
			}
		}
	})
}

// unsetEnvForTest unsets an environment variable and registers cleanup to restore the original value.
// This is a wrapper that handles error checking for os.Unsetenv.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	origVal, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Failed to unset env var %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			if err := os.Setenv(key, origVal); err != nil {
				t.Errorf("Failed to restore env var %s: %v", key, err)
			}
		}
	})
}
