package cienv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCIRunning_CIEnvVar(t *testing.T) {
	// Clear all CI markers before each sub-test.
	clearAllCIMarkers(t)

	t.Run("CI=true returns true", func(t *testing.T) {
		setEnvForTest(t, CIEnvVar, "true")
		assert.True(t, IsCIRunning())
	})

	t.Run("CI=false returns false", func(t *testing.T) {
		setEnvForTest(t, CIEnvVar, "false")
		assert.False(t, IsCIRunning())
	})

	t.Run("CI unset returns false", func(t *testing.T) {
		unsetEnvForTest(t, CIEnvVar)
		assert.False(t, IsCIRunning())
	})
}

func TestIsCIRunning_ProviderIndicators(t *testing.T) {
	for _, envVar := range CIEnvIndicators {
		envVar := envVar
		t.Run(envVar, func(t *testing.T) {
			clearAllCIMarkers(t)
			setEnvForTest(t, envVar, "somevalue")
			assert.True(t, IsCIRunning(), "expected IsCIRunning()=true when %s is set", envVar)
		})
	}
}

func TestIsCIRunning_NoCIMarkersReturnsFalse(t *testing.T) {
	clearAllCIMarkers(t)
	assert.False(t, IsCIRunning())
}

// clearAllCIMarkers unsets CI and every provider-specific indicator for the duration of the test.
func clearAllCIMarkers(t *testing.T) {
	t.Helper()
	unsetEnvForTest(t, CIEnvVar)
	for _, envVar := range CIEnvIndicators {
		unsetEnvForTest(t, envVar)
	}
}
