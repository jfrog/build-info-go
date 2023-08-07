package pythonutils

import (
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestGetUsingCacheParser(t *testing.T) {
	var testData = []struct {
		text        string
		expectedMap map[string]entities.Dependency
	}{
		{"Collecting package\n\n Using cached \n https://link\n to\ncache (16 kB)", getExpectedMap()},
		{"Collecting package\nUsing cached \n https://link \n to \ncache \n (16 kB)", getExpectedMap()},
		{"Collecting package\n Using cached \n\n https://link \n\n to \n\ncache (16 kB)\n\n", getExpectedMap()},
		{"Collecting package\n Using cached \n\n https://link \n\n to \n\ncache \n\n(16 kB)\n\n", getExpectedMap()},
	}
	dependenciesMap := map[string]entities.Dependency{}
	dependencyNameParser, downloadedFileParser, pipEnvCachedParser, installedPackagesParser := GetLogParsers(dependenciesMap, &utils.NullLog{})
	regExpStruct := append([]*gofrogcmd.CmdOutputPattern{}, &dependencyNameParser, &downloadedFileParser, &pipEnvCachedParser, &installedPackagesParser)

	for _, test := range testData {
		for _, line := range strings.Split(test.text, "\n") {
			for _, regExp := range regExpStruct {
				matched := regExp.RegExp.Match([]byte(line))
				if matched {
					regExp.MatchedResults = regExp.RegExp.FindStringSubmatch(line)
					regExp.Line = line
					_, err := regExp.ExecFunc(regExp)
					assert.NoError(t, err)
				}
			}
		}
		assert.Equal(t, dependenciesMap, test.expectedMap)
	}
}

func getExpectedMap() map[string]entities.Dependency {
	expected := map[string]entities.Dependency{}
	expected["package"] = entities.Dependency{Id: "linktocache"}
	return expected
}
