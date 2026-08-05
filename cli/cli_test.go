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

func TestExtractApkPackageNames(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "add with packages", args: []string{"add", "curl", "wget"}, want: []string{"curl", "wget"}},
		{name: "skips flags", args: []string{"add", "--no-cache", "curl"}, want: []string{"curl"}},
		{name: "upgrade", args: []string{"upgrade", "openssl"}, want: []string{"openssl"}},
		{name: "empty", args: nil, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractApkPackageNames(tc.args))
		})
	}
}
