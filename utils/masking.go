package utils

import (
	gofrogcmd "github.com/jfrog/gofrog/io"
	"strings"
)

// #nosec G101 -- False positive - no hardcoded credentials.
const CredentialsInUrlRegexp = `(?:http|https|git)://.+@`

// Remove the credentials information from the line.
func RemoveCredentials(pattern *gofrogcmd.CmdOutputPattern) (string, error) {
	splitResult := strings.Split(pattern.MatchedResults[0], "//")
	return strings.ReplaceAll(pattern.Line, pattern.MatchedResults[0], splitResult[0]+"//***@"), nil
}
