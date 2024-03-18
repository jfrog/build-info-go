package solution

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
)

var logger = utils.NewDefaultLogger(utils.INFO)

func TestEmptySolution(t *testing.T) {
	solution, err := Load(".", "", "", logger)
	if err != nil {
		t.Error(err)
	}

	expected := &buildinfo.BuildInfo{}
	buildInfo, err := solution.BuildInfo("", logger)
	if err != nil {
		t.Error("An error occurred while creating the build info object", err.Error())
	}
	if !reflect.DeepEqual(buildInfo, expected) {
		expectedString, err := json.Marshal(expected)
		assert.NoError(t, err)
		buildInfoString, err := json.Marshal(buildInfo)
		assert.NoError(t, err)
		t.Errorf("Expecting: \n%s \nGot: \n%s", expectedString, buildInfoString)
	}
}

func TestParseSln(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Error(err)
	}

	testdataDir := filepath.Join(pwd, "testdata")

	tests := []struct {
		name     string
		slnPath  string
		expected []string
	}{
		{"oneproject", filepath.Join(testdataDir, "oneproject.sln"), []string{`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig.csproj`}},
		{"visualbasic", filepath.Join(testdataDir, "visualbasic.sln"), []string{`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig.vbproj`}},
		{"multiProjects", filepath.Join(testdataDir, "multiprojects.sln"), []string{`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfigmulti", "packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfiganothermulti", "test\packagesconfig.csproj`}},
		{"multiLinesProjectSln", filepath.Join(testdataDir, "multilinesproject.sln"), []string{`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfigmulti", "packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfiganothermulti", "test\packagesconfig.csproj`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := parseSlnFile(test.slnPath)
			if err != nil {
				t.Error(err)
			}

			replaceCarriageSign(results)

			if !reflect.DeepEqual(test.expected, results) {
				t.Errorf("Expected %s, got %s", test.expected, results)
			}
		})
	}
}

func TestParseProjectLine(t *testing.T) {
	tests := []struct {
		name                 string
		projectLine          string
		expectedProjFilePath string
		expectedProjectName  string
	}{
		{"packagename", `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagename", "packagesconfig.csproj", "{D1FFA0DC-0ACC-4108-ADC1-2A71122C09AF}"
EndProject`, filepath.Join("jfrog", "path", "test", "packagesconfig.csproj"), "packagename"},
		{"withpath", `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagename", "packagesconfig/packagesconfig.csproj", "{D1FFA0DC-0ACC-4108-ADC1-2A71122C09AF}"
EndProject`, filepath.Join("jfrog", "path", "test", "packagesconfig", "packagesconfig.csproj"), "packagename"},
		{"sameprojectname", `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig/packagesconfig.csproj", "{D1FFA0DC-0ACC-4108-ADC1-2A71122C09AF}"
EndProject`, filepath.Join("jfrog", "path", "test", "packagesconfig", "packagesconfig.csproj"), "packagesconfig"},
		{"vbproj", `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig/packagesconfig.vbproj", "{D1FFA0DC-0ACC-4108-ADC1-2A71122C09AF}"
EndProject`, filepath.Join("jfrog", "path", "test", "packagesconfig", "packagesconfig.vbproj"), "packagesconfig"},
	}

	path := filepath.Join("jfrog", "path", "test")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectName, projFilePath, err := parseProjectLine(test.projectLine, path)
			if err != nil {
				t.Error(err)
			}
			if projFilePath != test.expectedProjFilePath {
				t.Errorf("Expected %s, got %s", test.expectedProjFilePath, projFilePath)
			}
			if projectName != test.expectedProjectName {
				t.Errorf("Expected %s, got %s", test.expectedProjectName, projectName)
			}
		})
	}
}

func TestGetProjectsFromSlns(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Error(err)
	}

	testdataDir := filepath.Join(pwd, "testdata")
	tests := []struct {
		name             string
		solution         solution
		expectedProjects []string
	}{
		{"withoutSlnFile", solution{path: testdataDir, slnFile: "", projects: nil}, []string{`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfigmulti", "packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfiganothermulti", "test\packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfigmulti", "packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfiganothermulti", "test\packagesconfig.csproj`, `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig.vbproj`}},
		{"withSlnFile", solution{path: testdataDir, slnFile: "oneproject.sln", projects: nil}, []string{`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "packagesconfig", "packagesconfig.csproj`}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := test.solution.getProjectsFromSlns()
			if err != nil {
				t.Error(err)
			}
			replaceCarriageSign(results)
			assert.ElementsMatch(t, test.expectedProjects, results)
		})
	}
}

// If running on Windows, replace \r\n with \n.
func replaceCarriageSign(results []string) {
	if utils.IsWindows() {
		for i, result := range results {
			results[i] = strings.ReplaceAll(result, "\r\n", "\n")
		}
	}
}

func TestLoad(t *testing.T) {
	// Prepare
	log := utils.NewDefaultLogger(utils.INFO)
	wd, err := os.Getwd()
	if err != nil {
		t.Error(err)
	}
	// Run 'nuget restore' command before testing 'Load()' functionality.
	// The reason is that way the "global packages" directory (which is required for the loading process) will be created.
	assert.NoError(t, utils.CopyDir(filepath.Join(wd, "testdata", "nugetproj"), filepath.Join(wd, "tmp", "nugetproj"), true, nil))
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(filepath.Join(wd, "tmp")))
	}()
	nugetCmd := exec.Command("nuget", "restore", filepath.Join(wd, "tmp", "nugetproj", "solutions", "nugetproj.sln"))
	assert.NoError(t, nugetCmd.Run())

	testCases := []struct {
		name                 string
		excludePattern       string
		expectedProjectCount int
	}{
		{"noExcludePattern", "", 2},
		{"excludePattern", "proj1", 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// 'nugetproj' contains 2 'packages.config' files for 2 projects -
			// 1. located in the project's root directory.
			// 2. located in solutions directory.
			solution := solution{path: filepath.Join(wd, "testdata", "nugetproj", "solutions"), slnFile: "nugetproj.sln"}
			solutions, err := Load(solution.path, solution.slnFile, testCase.excludePattern, log)
			if err != nil {
				t.Error(err)
			}
			assert.Equal(t, testCase.expectedProjectCount, len(solutions.GetProjects()))
		})
	}
}
