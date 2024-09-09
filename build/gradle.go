package build

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/version"
)

const (
	extractorPropsDir                 = "BUILDINFO_PROPFILE"
	gradleExtractorFileName           = "build-info-extractor-gradle-%s-uber.jar"
	gradleInitScriptTemplate          = "gradle.init"
	gradleExtractorRemotePath         = "org/jfrog/buildinfo/build-info-extractor-gradle/%s"
	gradleExtractor4DependencyVersion = "4.33.22"
	gradleExtractor5DependencyVersion = "5.2.5"
	projectPropertiesFlag             = "-P"
	systemPropertiesFlag              = "-D"
)

var versionRegex = regexp.MustCompile(`Gradle (\d+\.\d+(?:\.\d+|-\w+-\d+)?)`)

//go:embed init-gradle-extractor-4.gradle
var gradleInitScriptExtractor4 string

//go:embed init-gradle-extractor-5.gradle
var gradleInitScriptExtractor5 string

type GradleModule struct {
	// The build which contains the gradle module.
	containingBuild *Build
	// Project path in the file system.
	srcPath string
	// The Gradle extractor (dependency) which calculates the build-info.
	gradleExtractorDetails *gradleExtractorDetails
}

type gradleExtractorDetails struct {
	// Gradle init script to build the project.
	initScript string
	// Use the gradle wrapper to build the project.
	useWrapper bool
	usePlugin  bool
	// Extractor local path.
	localPath string
	// gradle tasks to build the project.
	tasks []string
	// Download the extractor from remote server.
	downloadExtractorFunc func(downloadTo, downloadFrom string) error
	// Map of configurations for the extractor.
	props map[string]string
	// Local path to the configuration file.
	propsDir string
}

// Add a new Gradle module to a given build.
func newGradleModule(containingBuild *Build, srcPath string) *GradleModule {
	return &GradleModule{
		srcPath:         srcPath,
		containingBuild: containingBuild,
		gradleExtractorDetails: &gradleExtractorDetails{
			tasks:    []string{"artifactoryPublish"},
			propsDir: filepath.Join(containingBuild.tempDirPath, PropertiesTempFolderName),
			props:    map[string]string{},
		},
	}
}

func (gm *GradleModule) SetExtractorDetails(localExtractorPath, extractorPropsDir string, tasks []string, useWrapper, usePlugin bool, downloadExtractorFunc func(downloadTo, downloadFrom string) error, props map[string]string) *GradleModule {
	gm.gradleExtractorDetails.tasks = tasks
	gm.gradleExtractorDetails.propsDir = extractorPropsDir
	gm.gradleExtractorDetails.useWrapper = useWrapper
	gm.gradleExtractorDetails.usePlugin = usePlugin
	gm.gradleExtractorDetails.localPath = localExtractorPath
	gm.gradleExtractorDetails.downloadExtractorFunc = downloadExtractorFunc
	gm.gradleExtractorDetails.props = props
	return gm
}

// Generates Gradle build-info.
func (gm *GradleModule) CalcDependencies() (err error) {
	gm.containingBuild.logger.Info("Running gradle...")
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if gm.srcPath != "" {
		if err = os.Chdir(gm.srcPath); err != nil {
			return
		}
		defer func() {
			err = errors.Join(err, os.Chdir(wd))
		}()
	}

	gradleExecPath, err := GetGradleExecPath(gm.gradleExtractorDetails.useWrapper)
	if err != nil {
		return
	}
	if !gm.gradleExtractorDetails.usePlugin {
		if err := gm.downloadGradleExtractor(gradleExecPath); err != nil {
			return err
		}
	}
	gradleRunConfig, err := gm.createGradleRunConfig(gradleExecPath)
	if err != nil {
		return err
	}
	return gradleRunConfig.runCmd(os.Stdout, os.Stderr)
}

func (gm *GradleModule) downloadGradleExtractor(gradleExecPath string) (err error) {
	gradleExtractorVersion, initScriptPattern, err := gm.getExtractorVersionAndInitScript(gradleExecPath)
	if err != nil {
		return err
	}
	gm.containingBuild.logger.Debug("Using Gradle build-info extractor", gradleExtractorVersion)

	dependencyLocalPath := filepath.Join(gm.gradleExtractorDetails.localPath, gradleExtractorVersion)
	if err = downloadGradleDependencies(dependencyLocalPath, gradleExtractorVersion, gm.gradleExtractorDetails.downloadExtractorFunc, gm.containingBuild.logger); err != nil {
		return err
	}
	gradlePluginFilename := fmt.Sprintf(gradleExtractorFileName, gradleExtractorVersion)

	gm.gradleExtractorDetails.initScript, err = getInitScript(initScriptPattern, dependencyLocalPath, gradlePluginFilename)
	return
}

// Return the Gradle extractor version and the relevant init script according to the Gradle version:
// For Gradle >= 6.8.1 use Gradle extractor 5
// For Gradle < 6.8.1 use Gradle extractor 4
// gradleExecPath - The Gradle binary path
func (gm *GradleModule) getExtractorVersionAndInitScript(gradleExecPath string) (string, string, error) {
	gradleRunConfig := &gradleRunConfig{
		gradle: gradleExecPath,
		tasks:  []string{"--version"},
		logger: gm.containingBuild.logger,
	}

	outBuffer := new(bytes.Buffer)
	errBuffer := new(bytes.Buffer)
	if err := gradleRunConfig.runCmd(outBuffer, errBuffer); err != nil {
		return "", "", err
	}

	gradleVersion, err := parseGradleVersion(outBuffer.String())
	if err != nil {
		if errBuffer.Len() > 0 {
			err = errors.Join(err, errors.New(errBuffer.String()))
		}
		return "", "", err
	}
	gm.containingBuild.logger.Info("Using Gradle version:", gradleVersion.GetVersion())
	if gradleVersion.AtLeast("6.8.1") {
		return gradleExtractor5DependencyVersion, gradleInitScriptExtractor5, nil
	}
	return gradleExtractor4DependencyVersion, gradleInitScriptExtractor4, nil
}

// Parse the 'gradle --version' output and return the Gradle version.
// versionOutput - The 'gradle --version' output
func parseGradleVersion(versionOutput string) (*version.Version, error) {
	match := versionRegex.FindStringSubmatch(versionOutput)
	if len(match) == 0 {
		return nil, errors.New("couldn't parse the Gradle version: " + versionOutput)
	}
	return version.NewVersion(match[1]), nil
}

func (gm *GradleModule) createGradleRunConfig(gradleExecPath string) (*gradleRunConfig, error) {
	buildInfoPath, err := createEmptyBuildInfoFile(gm.containingBuild)
	if err != nil {
		return nil, err
	}
	extractorPropsFile, err := utils.CreateExtractorPropsFile(gm.gradleExtractorDetails.propsDir, buildInfoPath, gm.containingBuild.buildName, gm.containingBuild.buildNumber, gm.containingBuild.buildTimestamp, gm.containingBuild.projectKey, gm.gradleExtractorDetails.props)
	if err != nil {
		return nil, err
	}
	return &gradleRunConfig{
		env:                gm.gradleExtractorDetails.props,
		gradle:             gradleExecPath,
		extractorPropsFile: extractorPropsFile,
		tasks:              gm.gradleExtractorDetails.tasks,
		initScript:         gm.gradleExtractorDetails.initScript,
		logger:             gm.containingBuild.logger,
	}, nil
}

func downloadGradleDependencies(downloadTo, gradleExtractorVersion string, downloadExtractorFunc func(downloadTo, downloadPath string) error, logger utils.Log) error {
	filename := fmt.Sprintf(gradleExtractorFileName, gradleExtractorVersion)
	filePath := fmt.Sprintf(gradleExtractorRemotePath, gradleExtractorVersion)
	return utils.DownloadDependencies(downloadTo, filename, filePath, downloadExtractorFunc, logger)
}

func getInitScript(initScriptPattern, gradleDependenciesDir, gradlePluginFilename string) (string, error) {
	gradleDependenciesDir, err := filepath.Abs(gradleDependenciesDir)
	if err != nil {
		return "", err
	}
	initScriptPath := filepath.Join(gradleDependenciesDir, gradleInitScriptTemplate)

	exists, err := utils.IsFileExists(initScriptPath, true)
	if exists || err != nil {
		return initScriptPath, err
	}

	gradlePluginPath := filepath.Join(gradleDependenciesDir, gradlePluginFilename)
	gradlePluginPath = strings.ReplaceAll(gradlePluginPath, "\\", "\\\\")
	initScriptContent := strings.ReplaceAll(initScriptPattern, "${pluginLibDir}", gradlePluginPath)
	if !utils.IsPathExists(gradleDependenciesDir) {
		err = os.MkdirAll(gradleDependenciesDir, 0777)
		if err != nil {
			return "", err
		}
	}

	return initScriptPath, os.WriteFile(initScriptPath, []byte(initScriptContent), 0644)
}

func GetGradleExecPath(useWrapper bool) (string, error) {
	if useWrapper {
		execName := "gradlew"
		if utils.IsWindows() {
			execName += ".bat"
		}
		// The Go1.19 update added the restriction that executables in the current directory are not resolved when the only executable name is provided.
		return "." + string(os.PathSeparator) + execName, nil
	}
	gradleExec, err := exec.LookPath("gradle")
	if err != nil {
		return "", err
	}
	return gradleExec, nil
}

type gradleRunConfig struct {
	gradle             string
	extractorPropsFile string
	tasks              []string
	initScript         string
	env                map[string]string
	logger             utils.Log
}

func (config *gradleRunConfig) GetCmd() *exec.Cmd {
	var cmd []string
	cmd = append(cmd, config.gradle)
	if config.initScript != "" {
		cmd = append(cmd, "--init-script", config.initScript)
	}
	cmd = append(cmd, formatCommandProperties(config.tasks)...)
	config.logger.Info("Running gradle command:", strings.Join(cmd, " "))
	return exec.Command(cmd[0], cmd[1:]...)
}

func formatCommandProperties(tasks []string) []string {
	var cmdArgs []string
	for _, task := range tasks {
		if isSystemOrProjectProperty(task) {
			task = quotePropertyIfNeeded(task)
		}
		cmdArgs = append(cmdArgs, task)
	}
	return cmdArgs
}

func isSystemOrProjectProperty(task string) bool {
	hasPropertiesFlag := strings.HasPrefix(task, systemPropertiesFlag) || strings.HasPrefix(task, projectPropertiesFlag)
	return hasPropertiesFlag && strings.Contains(task, "=")
}

// Wraps system or project property value in quotes if its value contain spaces, e.g., -Dkey=val ue => -Dkey='val ue'
func quotePropertyIfNeeded(task string) string {
	parts := strings.SplitN(task, "=", 2)
	if strings.Contains(parts[1], " ") {
		return fmt.Sprintf(`%s='%s'`, parts[0], parts[1])
	}

	return task
}

func (config *gradleRunConfig) runCmd(stdout, stderr io.Writer) error {
	command := config.GetCmd()
	command.Env = os.Environ()
	for k, v := range config.env {
		command.Env = append(command.Env, k+"="+v)
	}
	command.Env = append(command.Env, extractorPropsDir+"="+config.extractorPropsFile)
	command.Stderr = stderr
	command.Stdout = stdout
	return command.Run()
}
