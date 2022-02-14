package cli

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestExtractStringFlag(t *testing.T) {
	testCases := []struct {
		args                 []string
		flagName             string
		expectedFlagValue    string
		expectedFilteredArgs []string
		expectedError        bool
	}{
		{args: []string{"a", "--b", "c", "d"}, flagName: "b", expectedFlagValue: "c", expectedFilteredArgs: []string{"a", "d"}, expectedError: false},
		{args: []string{"--a=b"}, flagName: "a", expectedFlagValue: "b", expectedFilteredArgs: []string{}, expectedError: false},
		{args: []string{"a", "--b=c"}, flagName: "a", expectedFlagValue: "", expectedFilteredArgs: []string{"a", "--b=c"}, expectedError: false},
		{args: []string{"a", "--b"}, flagName: "b", expectedFlagValue: "", expectedFilteredArgs: []string{}, expectedError: true},
		{args: []string{"a", "--b", "--c", "d"}, flagName: "b", expectedFlagValue: "", expectedFilteredArgs: []string{}, expectedError: true},
	}

	for _, testCase := range testCases {
		actualFlagValue, actualFilteredArgs, err := extractStringFlag(testCase.args, testCase.flagName)
		if testCase.expectedError {
			assert.Error(t, err)
			continue
		}
		assert.NoError(t, err)
		assert.Equal(t, testCase.expectedFlagValue, actualFlagValue)
		assert.Equal(t, testCase.expectedFilteredArgs, actualFilteredArgs)
	}
}
