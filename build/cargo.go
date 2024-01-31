package build

import (
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	gofrogcmd "github.com/jfrog/gofrog/io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var cargoModuleFile = "Cargo.toml"

type CargoModule struct {
	containingBuild *Build
	name            string
	version         string
	srcPath         string
}

func newCargoModule(srcPath string, containingBuild *Build) (*CargoModule, error) {
	var err error
	if srcPath == "" {
		srcPath, err = getProjectRoot()
		if err != nil {
			return nil, err
		}
	}

	// Read module name
	name, version, err := getModuleNameByDirWithVersion(srcPath, containingBuild.logger)
	if err != nil {
		return nil, err
	}
	return &CargoModule{name: name, version: version, srcPath: srcPath, containingBuild: containingBuild}, nil
}

func getProjectRoot() (string, error) {
	// Get the current directory.
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	return utils.FindFileInDirAndParents(wd, cargoModuleFile)
}

func getModuleNameByDirWithVersion(projectDir string, log utils.Log) (string, string, error) {

	var values map[string]map[string]string

	_, err := toml.DecodeFile(path.Join(projectDir, cargoModuleFile), &values)
	return values["package"]["name"], values["package"]["version"], err
}
func (gm *CargoModule) AddArtifacts(artifacts ...entities.Artifact) error {
	if !gm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to add artifacts")
	}
	partial := &entities.Partial{ModuleId: gm.name + ":" + gm.version, ModuleType: entities.Cargo, Artifacts: artifacts}
	return gm.containingBuild.SavePartialBuildInfo(partial)
}
func (cm *CargoModule) CalcDependencies() error {
	if !cm.containingBuild.buildNameAndNumberProvided() {
		return errors.New("a build name must be provided in order to collect the project's dependencies")
	}
	buildInfoDependencies, err := cm.loadDependencies()
	if err != nil {
		return err
	}

	buildInfoModule := entities.Module{Id: cm.name + ":" + cm.version, Type: entities.Cargo, Dependencies: buildInfoDependencies}
	buildInfo := &entities.BuildInfo{Modules: []entities.Module{buildInfoModule}}

	return cm.containingBuild.SaveBuildInfo(buildInfo)
}

func (cm *CargoModule) loadDependencies() ([]entities.Dependency, error) {
	cachePath, err := getCachePath(cm.containingBuild.logger)
	if err != nil {
		return nil, err
	}
	dependenciesGraph, err := getDependenciesGraph(cm.srcPath, cm.containingBuild.logger)
	if err != nil {
		return nil, err
	}
	mapOfDeps := map[string]bool{}
	for k, v := range dependenciesGraph {
		mapOfDeps[k] = true
		for _, v2 := range v {
			mapOfDeps[v2] = true
		}
	}
	dependenciesMap, err := cm.getCargoDependencies(cachePath, mapOfDeps)
	if err != nil {
		return nil, err
	}
	emptyRequestedBy := [][]string{{}}
	populateRequestedByField(cm.name+":"+cm.version, emptyRequestedBy, dependenciesMap, dependenciesGraph)
	return dependenciesMapToList(dependenciesMap), nil
}

func (cm *CargoModule) getCargoDependencies(cachePath string, depList map[string]bool) (map[string]entities.Dependency, error) {
	// Create a map from dependency to parents
	buildInfoDependencies := make(map[string]entities.Dependency)
	for moduleId := range depList {
		zipPath, err := getCrateLocation(cachePath, moduleId, cm.containingBuild.logger)
		if err != nil {
			return nil, err
		}
		if zipPath == "" {
			continue
		}
		zipDependency, err := populateCrate(moduleId, zipPath)
		if err != nil {
			return nil, err
		}
		buildInfoDependencies[moduleId] = zipDependency
	}
	return buildInfoDependencies, nil
}

func getDependenciesGraph(projectDir string, log utils.Log) (map[string][]string, error) {
	cmdArgs := []string{"tree", "--prefix=depth"}
	output, err := runDependenciesCmd(projectDir, cmdArgs, log)
	if err != nil {
		return nil, err
	}
	return graphToMap(output), err
}
func populateCrate(packageId, zipPath string) (zipDependency entities.Dependency, err error) {
	zipDependency = entities.Dependency{Id: packageId}
	md5, sha1, sha2, err := utils.GetFileChecksums(zipPath)
	if err != nil {
		return
	}
	zipDependency.Type = "cargo"
	zipDependency.Checksum = entities.Checksum{Sha1: sha1, Md5: md5, Sha256: sha2}
	return
}

var deplineRegex = regexp.MustCompile(`([0-9]+)(\S+) v(\S+)( \(.*\))?`)
var depNoDepthlineRegex = regexp.MustCompile(`(\S+) v(\S+)( \(.*\))?`)

func graphToMap(output string) map[string][]string {
	var stack Stack
	lineOutput := strings.Split(output, "\n")
	mapOfDeps := map[string][]string{}
	prevDepth := -1
	currParent := ""
	prevLineName := ""
	for _, line := range lineOutput {
		if line == "" {
			continue
		}
		// The expected syntax : {depth}name v{ver} (features)
		// e.g.: 4proc-macro-error-attr v1.0.4 (proc-macro)
		match := deplineRegex.FindStringSubmatch(line)
		//// parse
		depth, err := strconv.Atoi(match[1])
		if err != nil {
			panic(err)
		}
		nameAndVersion := match[2] + ":" + match[3]

		if depth == prevDepth {
			// no need to change parent or if 0, no need to store at all
		} else if depth > prevDepth {
			stack.Push(currParent)
			currParent = prevLineName
		} else {
			diff := prevDepth - depth // how many parents to pop
			for i := 0; i < diff; i++ {
				currParent, _ = stack.Pop()
			}
		}
		if depth > 0 {
			mapOfDeps[currParent] = append(mapOfDeps[currParent], nameAndVersion)
		} else {
			prevLineName = nameAndVersion
			prevDepth = depth
			continue
		}
		prevDepth = depth
		prevLineName = nameAndVersion
	}
	return mapOfDeps
}

func getCargoHome(log utils.Log) (cargoHome string, err error) {
	// check for env var CARGO_HOME treat both abs and relative values
	// if fails, check for location of binary
	log.Debug("Searching for Cargo home.")
	cargoHome = os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		cargo, err := exec.LookPath("cargo")
		if err != nil {
			return "", err
		}
		cargoHome = path.Dir(path.Dir(cargo))
	} else {
		if !path.IsAbs(cargoHome) {
			// for relative path, prepend current directory to it
			wd, err := os.Getwd()
			if err != nil {
				return wd, err
			}
			cargoHome = filepath.Join(wd, cargoHome)
		}
	}
	log.Debug("Cargo home location:", cargoHome)

	return
}

func getCachePath(log utils.Log) (string, error) {
	goModCachePath, err := getCargoHome(log)
	if err != nil {
		return "", err
	}
	return filepath.Join(goModCachePath, "registry", "cache"), nil
}
func getCrateLocation(cachePath, encodedDependencyId string, log utils.Log) (cratePath string, err error) {
	moduleInfo := strings.Split(encodedDependencyId, ":")
	if len(moduleInfo) != 2 {
		log.Debug("The encoded dependency Id syntax should be 'name:version' but instead got:", encodedDependencyId)
		return "", nil
	}
	dependencyName := moduleInfo[0]
	version := moduleInfo[1]
	entries, err := os.ReadDir(cachePath)
	if err != nil {
		return "", fmt.Errorf("could not read cache directory. %s", err)
	}
	fileExists := false
	for _, file := range entries {
		if file.IsDir() {
			cratePath = filepath.Join(cachePath, file.Name(), dependencyName+"-"+version+".crate")
			fileExists, err = utils.IsFileExists(cratePath, true)
			if err != nil {
				return "", fmt.Errorf("could not find zip binary for dependency '%s' at %s: %s", dependencyName, cratePath, err)
			}
			if fileExists {
				break
			}
		}
	}

	// Crate binary does not exist, so we skip it by returning a nil dependency.
	if !fileExists {
		log.Debug("Could not find crate")
		return "", nil
	}
	return cratePath, nil
}

type Stack []string

func (s *Stack) Push(str string) {
	*s = append(*s, str)
}
func (s *Stack) Pop() (string, bool) {
	if len(*s) == 0 {
		return "", false
	} else {
		index := len(*s) - 1
		element := (*s)[index]
		*s = (*s)[:index]
		return element, true
	}
}

func runDependenciesCmd(projectDir string, commandArgs []string, log utils.Log) (output string, err error) {
	log.Info(fmt.Sprintf("Running 'cargo %s' in %s", strings.Join(commandArgs, " "), projectDir))
	if projectDir == "" {
		projectDir, err = getProjectRoot()
		if err != nil {
			return "", err
		}
	}

	goCmd := gofrogcmd.NewCommand("cargo", "", commandArgs)
	goCmd.Dir = projectDir

	///	err = prepareGlobalRegExp()
	///	if err != nil {
	///		return "", err
	///	}
	///	performPasswordMask, err := shouldMaskPassword()
	///	if err != nil {
	///		return "", err
	///	}
	var executionError error
	var errorOut string
	///	if performPasswordMask {
	///		output, errorOut, _, executionError = gofrogcmd.RunCmdWithOutputParser(goCmd, false, protocolRegExp)
	///	} else {
	output, errorOut, _, executionError = gofrogcmd.RunCmdWithOutputParser(goCmd, false)
	///	}
	if len(output) != 0 {
		log.Debug(output)
	}
	if executionError != nil {
		// If the command fails, the mod stays the same, therefore, don't need to be restored.
		errorString := fmt.Sprintf("Failed running Cargo command: 'cargo %s' in %s with error: '%s - %s'", strings.Join(commandArgs, " "), projectDir, executionError.Error(), errorOut)
		return "", errors.New(errorString)
	}

	return output, err
}
