package solution

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	buildinfo "github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var logger = utils.NewDefaultLogger(utils.INFO)

func TestSortRequestedByPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    [][]string
		expected [][]string
	}{
		{
			name: "shorter paths first",
			input: [][]string{
				{"A:1", "B:1", "Module"},
				{"Module"},
				{"B:1", "Module"},
			},
			expected: [][]string{
				{"Module"},
				{"B:1", "Module"},
				{"A:1", "B:1", "Module"},
			},
		},
		{
			name: "same length lexicographic",
			input: [][]string{
				{"C:1", "Module"},
				{"A:1", "Module"},
				{"B:1", "Module"},
			},
			expected: [][]string{
				{"A:1", "Module"},
				{"B:1", "Module"},
				{"C:1", "Module"},
			},
		},
		{
			name: "mixed lengths and values",
			input: [][]string{
				{"Z:1", "Y:1", "Module"},
				{"Module"},
				{"A:1", "Module"},
				{"B:1", "C:1", "Module"},
			},
			expected: [][]string{
				{"Module"},
				{"A:1", "Module"},
				{"B:1", "C:1", "Module"},
				{"Z:1", "Y:1", "Module"},
			},
		},
		{
			name:     "empty input",
			input:    [][]string{},
			expected: [][]string{},
		},
		{
			name:     "single element",
			input:    [][]string{{"Module"}},
			expected: [][]string{{"Module"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortRequestedByPaths(tt.input)
			assert.Equal(t, tt.expected, tt.input)
		})
	}
}

func TestIsMatchingDependencySource(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name               string
		source             string
		projectRootPath    string
		projectObjPattern  string
		projectNamePattern string
		expected           bool
	}{
		{
			name:               "source in project root",
			source:             filepath.Join("solution", "myproject", "packages.config"),
			projectRootPath:    strings.ToLower(filepath.Join("solution", "myproject")),
			projectObjPattern:  strings.ToLower(filepath.Join("solution", "myproject", "obj") + sep),
			projectNamePattern: strings.ToLower(sep + "myproject" + sep),
			expected:           true,
		},
		{
			name:               "source in obj directory",
			source:             filepath.Join("solution", "myproject", "obj", "project.assets.json"),
			projectRootPath:    strings.ToLower(filepath.Join("solution", "myproject")),
			projectObjPattern:  strings.ToLower(filepath.Join("solution", "myproject", "obj") + sep),
			projectNamePattern: strings.ToLower(sep + "myproject" + sep),
			expected:           true,
		},
		{
			name:               "source in subdirectory with project name",
			source:             filepath.Join("other", "myproject", "obj", "project.assets.json"),
			projectRootPath:    strings.ToLower(filepath.Join("solution", "myproject")),
			projectObjPattern:  strings.ToLower(filepath.Join("solution", "myproject", "obj") + sep),
			projectNamePattern: strings.ToLower(sep + "myproject" + sep),
			expected:           true,
		},
		{
			name:               "source not matching - different project",
			source:             filepath.Join("solution", "otherproject", "packages.config"),
			projectRootPath:    strings.ToLower(filepath.Join("solution", "myproject")),
			projectObjPattern:  strings.ToLower(filepath.Join("solution", "myproject", "obj") + sep),
			projectNamePattern: strings.ToLower(sep + "myproject" + sep),
			expected:           false,
		},
		{
			name:               "partial project name should not match",
			source:             filepath.Join("solution", "myprojectextra", "packages.config"),
			projectRootPath:    strings.ToLower(filepath.Join("solution", "myproject")),
			projectObjPattern:  strings.ToLower(filepath.Join("solution", "myproject", "obj") + sep),
			projectNamePattern: strings.ToLower(sep + "myproject" + sep),
			expected:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMatchingDependencySource(tt.source, tt.projectRootPath, tt.projectObjPattern, tt.projectNamePattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPopulateRequestedByDeterministic(t *testing.T) {
	// Test that populateRequestedBy produces consistent results across multiple runs
	// by verifying the output is always the same regardless of map iteration order

	runTest := func() [][]string {
		dependencies := map[string]*buildinfo.Dependency{
			"depa": {Id: "depA:1"},
			"depb": {Id: "depB:1"},
			"depc": {Id: "depC:1"},
			"depd": {Id: "depD:1"},
		}

		// depA and depB are direct deps, both depend on depC
		// depC depends on depD
		directDependencies := []string{"depb", "depa"}
		sort.Strings(directDependencies)

		childrenMap := map[string][]string{
			"depa": {"depc"},
			"depb": {"depc"},
			"depc": {"depd"},
			"depd": {},
		}
		for key, children := range childrenMap {
			sort.Strings(children)
			childrenMap[key] = children
		}

		moduleId := "TestModule"
		for _, direct := range directDependencies {
			dependencies[direct].RequestedBy = append(dependencies[direct].RequestedBy, []string{moduleId})
		}
		for _, direct := range directDependencies {
			populateRequestedBy(*dependencies[direct], dependencies, childrenMap)
		}

		// Sort the results for comparison
		result := dependencies["depd"].RequestedBy
		sortRequestedByPaths(result)
		return result
	}

	// Run multiple times to ensure consistency
	firstResult := runTest()
	for i := 0; i < 10; i++ {
		result := runTest()
		assert.Equal(t, firstResult, result, "Run %d produced different results", i)
	}

	// Verify expected paths
	expected := [][]string{
		{"depC:1", "depA:1", "TestModule"},
		{"depC:1", "depB:1", "TestModule"},
	}
	assert.Equal(t, expected, firstResult)
}

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

func TestLoadNuget(t *testing.T) {
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

func TestLoadMixed(t *testing.T) {
	// Prepare
	log := utils.NewDefaultLogger(utils.INFO)
	wd, err := os.Getwd()
	require.NoError(t, err)
	testdataDir := filepath.Join(wd, "testdata")

	// Run 'nuget restore' (multi) / 'dotnet restore' (core) command before testing 'Load()' functionality.
	assert.NoError(t, utils.CopyDir(filepath.Join(testdataDir, "multi"), filepath.Join(wd, "tmp", "multi"), true, nil))
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(filepath.Join(wd, "tmp")))
	}()
	nugetCmd := exec.Command("nuget", "restore", filepath.Join(wd, "tmp", "multi", "multi.sln"))
	assert.NoError(t, nugetCmd.Run())

	solution := solution{path: filepath.Join(wd, "tmp", "multi"), slnFile: "multi.sln"}
	solutions, err := Load(solution.path, solution.slnFile, "", log)
	require.NoError(t, err)
	// The solution contains 2 projects:
	// 1. 'multi' - a .NET Framework project with 'packages.config'.
	// 2. 'core' - a .NET Core project with 'project.json'.
	assert.Equal(t, 2, len(solutions.GetProjects()))
	// Check source dependencies paths in solution.
	assert.ElementsMatch(t, solutions.GetDependenciesSources(), []string{
		filepath.Join(wd, "tmp", "multi", "multi", "packages.config"),
		filepath.Join(wd, "tmp", "multi", "core", "obj", "project.assets.json"),
	})

	for _, project := range solutions.GetProjects() {
		switch project.Name() {
		case "multi":
			assert.Equal(t, filepath.Join(wd, "tmp", "multi", "multi"), project.RootPath())
			direct, err := project.Extractor().DirectDependencies()
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"newtonsoft.json"}, direct)
		case "core":
			assert.Equal(t, filepath.Join(wd, "tmp", "multi", "core"), project.RootPath())
			direct, err := project.Extractor().DirectDependencies()
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"newtonsoft.json"}, direct)
		default:
			t.Errorf("Unexpected project name: %s", project.Name())
		}
	}
}
