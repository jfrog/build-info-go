package utils

import (
	"bytes"
	"errors"
	"github.com/jfrog/build-info-go/utils"
	"github.com/stretchr/testify/assert"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetYarnDependencyKeyFromLocator(t *testing.T) {
	testCases := []struct {
		yarnDepLocator string
		expectedDepKey string
	}{
		{"camelcase@npm:6.2.0", "camelcase@npm:6.2.0"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight@npm:7.14.0"},
		{"fsevents@patch:fsevents@npm%3A2.3.2#builtin<compat/fsevents>::version=2.3.2&hash=11e9ea", "fsevents@patch:fsevents@npm%3A2.3.2#builtin<compat/fsevents>::version=2.3.2&hash=11e9ea"},
		{"follow-redirects@virtual:c192f6b3b32cd5d11a443145a3883a70c04cbd7c813b53085dbaf50263735f1162f10fdbddd53c24e162ec3bc#npm:1.14.1", "follow-redirects@npm:1.14.1"},
	}

	for _, testCase := range testCases {
		assert.Equal(t, testCase.expectedDepKey, GetYarnDependencyKeyFromLocator(testCase.yarnDepLocator))
	}
}

func TestGetYarnV2Dependencies(t *testing.T) {
	checkGetYarnDependencies(t, "v2", []string{"json@npm:9.0.6", "react@npm:18.2.0", "xml@npm:1.0.1"})
}

func TestBuildYarnV1Dependencies(t *testing.T) {
	checkGetYarnDependencies(t, "v1", []string{"json@9.0.6", "react@18.2.0", "xml@1.0.1"})
}

func TestGetYarnDependenciesUninstalled(t *testing.T) {
	checkGetYarnDependenciesUninstalled(t, "2.4.0")
	checkGetYarnDependenciesUninstalled(t, "latest")
}

func checkGetYarnDependenciesUninstalled(t *testing.T, versionToSet string) {
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}()
	testDataSource := filepath.Join("..", "testdata", "yarn", "v2")
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)
	projectSrcPath := filepath.Join(testDataTarget, "project")

	err = updateDirYarnVersion(executablePath, projectSrcPath, versionToSet)
	assert.NoError(t, err, "could not set version '"+versionToSet+"' to the test suitcase")

	// Deleting yarn.lock content to make imitate the reverse action of 'yarn install'
	lockFilePath := filepath.Join(projectSrcPath, "yarn.lock")
	yarnLockFile, err := os.OpenFile(lockFilePath, os.O_WRONLY, 0666)
	assert.NoError(t, err, "Could not open yarn.lock file")
	defer func() {
		assert.NoError(t, yarnLockFile.Close())
	}()
	err = yarnLockFile.Truncate(0) // This line erases the file's content without deleting the file itself
	assert.NoError(t, err, "Could not erase yarn.lock file content")

	pacInfo := PackageInfo{Name: "build-info-go-tests"}
	_, _, err = GetYarnDependencies(executablePath, projectSrcPath, &pacInfo, &utils.NullLog{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetching dependencies failed since '"+pacInfo.Name+"' doesn't present in your lockfile\nPlease run 'yarn install' to update lockfile\n")
}

func updateDirYarnVersion(executablePath string, srcPath string, versionToSet string) (err error) {
	command := exec.Command(executablePath, "set", "version", versionToSet)

	command.Dir = srcPath
	outBuffer := bytes.NewBuffer([]byte{})
	command.Stdout = outBuffer
	errBuffer := bytes.NewBuffer([]byte{})
	command.Stderr = errBuffer
	err = command.Run()

	if err != nil {
		// urfave/cli (aka codegangsta) exits when an ExitError is returned, so if it's an ExitError we'll convert it to a regular error.
		if _, ok := err.(*exec.ExitError); ok {
			err = errors.New(err.Error())
		}
		return
	}
	return
}

func checkGetYarnDependencies(t *testing.T, versionDir string, expectedLocators []string) {
	// Copy the project directory to a temporary directory
	tempDirPath, err := utils.CreateTempDir()
	assert.NoError(t, err, "Couldn't create temp dir")
	defer func() {
		assert.NoError(t, utils.RemoveTempDir(tempDirPath), "Couldn't remove temp dir")
	}()
	testDataSource := filepath.Join("..", "testdata", "yarn", versionDir)
	testDataTarget := filepath.Join(tempDirPath, "yarn")
	assert.NoError(t, utils.CopyDir(testDataSource, testDataTarget, true, nil))

	// Collecting and creating arguments for the command
	executablePath, err := GetYarnExecutable()
	assert.NoError(t, err)
	projectSrcPath := filepath.Join(testDataTarget, "project")
	pacInfo := PackageInfo{
		Name:            "build-info-go-tests",
		Version:         "v1.0.0",
		Dependencies:    map[string]string{"react": "18.2.0", "xml": "1.0.1"},
		DevDependencies: map[string]string{"json": "9.0.6"},
	}
	dependenciesMap, root, err := GetYarnDependencies(executablePath, projectSrcPath, &pacInfo, &utils.NullLog{})
	assert.NoError(t, err)
	assert.NotNil(t, root)

	// Checking dependencyMap
	assert.Len(t, dependenciesMap, 6)
	for dependencyName, depInfo := range dependenciesMap {
		var packageCleanName, packageVersion string
		if dependencyName != root.Value {
			packageCleanName, packageVersion, err = splitNameAndVersion(dependencyName)
			assert.NoError(t, err)
			if packageCleanName == "" || packageVersion == "" {
				assert.NoError(t, errors.New("got an empty dependency name/version or in incorrect format (expected: package-name@version) "))
			}
		} else {
			packageCleanName = root.Value
		}

		switch packageCleanName {
		case "react":
			assert.Equal(t, "18.2.0", depInfo.Details.Version)
			assert.NotNil(t, depInfo.Details.Dependencies)
			subDependencies := []string{"loose-envify"}
			for _, depPointer := range depInfo.Details.Dependencies {
				packageName, _, err := splitNameAndVersion(depPointer.Locator)
				assert.NoError(t, err)
				assert.Contains(t, subDependencies, packageName)
			}
		case "xml":
			assert.Equal(t, "1.0.1", depInfo.Details.Version)
			assert.Nil(t, depInfo.Details.Dependencies)
		case "json":
			assert.Equal(t, "9.0.6", depInfo.Details.Version)
			assert.Nil(t, depInfo.Details.Dependencies)
		case "loose-envify":
			assert.NotNil(t, depInfo.Details.Dependencies)
			assert.Equal(t, len(depInfo.Details.Dependencies), 1)
		case "js-tokens":
			assert.Nil(t, depInfo.Details.Dependencies)
		case root.Value:
			assert.True(t, strings.HasPrefix(root.Value, "build-info-go-tests"))
			assert.Equal(t, "v1.0.0", root.Details.Version)
			for _, dependency := range root.Details.Dependencies {
				assert.Contains(t, expectedLocators, dependency.Locator)
			}
		default:
			assert.NoError(t, errors.New("package "+dependencyName+" should not be inside the dependencies map"))
		}
	}
}

func TestYarnDependency_Name(t *testing.T) {
	testCases := []struct {
		packageFullName     string
		packageExpectedName string
	}{
		{"json@1.2.3", "json"},
		{"@babel/highlight@7.14.0", "@babel/highlight"},
		{"json@npm:1.2.3", "json"},
		{"@babel/highlight@npm:7.14.0", "@babel/highlight"},
	}
	for _, testCase := range testCases {
		yarnDep := YarnDependency{Value: testCase.packageFullName}
		assert.Equal(t, testCase.packageExpectedName, yarnDep.Name())
	}
}
