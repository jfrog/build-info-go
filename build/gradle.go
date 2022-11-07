package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jfrog/build-info-go/utils"
)

const (
	extractorPropsDir                = "BUILDINFO_PROPFILE"
	GradleExtractorFileName          = "build-info-extractor-gradle-%s-uber.jar"
	gradleInitScriptTemplate         = "gradle.init"
	GradleExtractorRemotePath        = "org/jfrog/buildinfo/build-info-extractor-gradle/%s"
	GradleExtractorDependencyVersion = "4.29.2"
)

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
	// Download the extracor from remote server.
	downloadExtractorFunc func(downloadTo, downloadFrom string) error
	// Map of configurations for the extractor.
	props map[string]string
	// Local path to the configuration file.
	propsDir string
}

// Add a new Gradle module to a given build.
func newGradleModule(containingBuild *Build, srcPath string) (*GradleModule, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	extractorLocalPath := filepath.Join(home, dependenciesDirName, "gradle", GradleExtractorDependencyVersion)
	return &GradleModule{
		srcPath:         srcPath,
		containingBuild: containingBuild,
		gradleExtractorDetails: &gradleExtractorDetails{
			localPath: extractorLocalPath,
			tasks:     []string{"artifactoryPublish"},
			propsDir:  filepath.Join(containingBuild.tempDirPath, PropertiesTempfolderName),
			props:     map[string]string{},
		},
	}, err
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
	if gm.srcPath == "" {
		if gm.srcPath, err = os.Getwd(); err != nil {
			return
		}
	}

	if err = downloadGradleDependencies(gm.gradleExtractorDetails.localPath, gm.gradleExtractorDetails.downloadExtractorFunc, gm.containingBuild.logger); err != nil {
		return err
	}
	if !gm.gradleExtractorDetails.usePlugin {
		gradlePluginFilename := fmt.Sprintf(GradleExtractorFileName, GradleExtractorDependencyVersion)

		gm.gradleExtractorDetails.initScript, err = getInitScript(gm.gradleExtractorDetails.localPath, gradlePluginFilename)
		if err != nil {
			return err
		}
	}
	gradleRunConfig, err := gm.createGradleRunConfig()
	if err != nil {
		return err
	}
	return gradleRunConfig.runCmd()
}

func (gm *GradleModule) createGradleRunConfig() (*gradleRunConfig, error) {
	gradleExecPath, err := getGradleExecPath(gm.gradleExtractorDetails.useWrapper)
	if err != nil {
		return nil, err
	}
	buildInfoPath, err := createEmptyBuildInfoFile(gm.containingBuild)
	if err != nil {
		return nil, err
	}
	extractorPropsFile, err := utils.CreateExtractorPropsFile(gm.gradleExtractorDetails.propsDir, buildInfoPath, gm.containingBuild.buildName, gm.containingBuild.buildNumber, gm.containingBuild.projectKey, gm.gradleExtractorDetails.props)
	if err != nil {
		return nil, err
	}
	return &gradleRunConfig{
		env:                gm.gradleExtractorDetails.props,
		gradle:             gradleExecPath,
		extractorPropsFile: extractorPropsFile,
		tasks:              strings.Join(gm.gradleExtractorDetails.tasks, " "),
		initScript:         gm.gradleExtractorDetails.initScript,
		logger:             gm.containingBuild.logger,
	}, nil
}

func downloadGradleDependencies(downloadTo string, downloadExtractorFunc func(downloadTo, downloadPath string) error, logger utils.Log) error {
	filename := fmt.Sprintf(GradleExtractorFileName, GradleExtractorDependencyVersion)
	filePath := fmt.Sprintf(GradleExtractorRemotePath, GradleExtractorDependencyVersion)
	return utils.DownloadDependencies(downloadTo, filename, filePath, downloadExtractorFunc, logger)
}

func getInitScript(gradleDependenciesDir, gradlePluginFilename string) (string, error) {
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
	gradlePluginPath = strings.Replace(gradlePluginPath, "\\", "\\\\", -1)
	initScriptContent := strings.Replace(GradleInitScript, "${pluginLibDir}", gradlePluginPath, -1)
	if !utils.IsPathExists(gradleDependenciesDir) {
		err = os.MkdirAll(gradleDependenciesDir, 0777)
		if err != nil {
			return "", err
		}
	}

	return initScriptPath, os.WriteFile(initScriptPath, []byte(initScriptContent), 0644)
}

func getGradleExecPath(useWrapper bool) (string, error) {
	if useWrapper {
		execName := "gradlew"
		if runtime.GOOS == "windows" {
			execName = execName + ".bat"
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
	tasks              string
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
	cmd = append(cmd, strings.Split(config.tasks, " ")...)
	config.logger.Info("Running gradle command:", strings.Join(cmd, " "))
	return exec.Command(cmd[0], cmd[1:]...)
}

func (config *gradleRunConfig) runCmd() error {
	command := config.GetCmd()
	command.Env = os.Environ()
	for k, v := range config.env {
		command.Env = append(command.Env, k+"="+v)
	}
	command.Env = append(command.Env, extractorPropsDir+"="+config.extractorPropsFile)
	command.Stderr = os.Stderr
	command.Stdout = os.Stderr
	return command.Run()
}

const GradleInitScript = `import org.jfrog.gradle.plugin.artifactory.ArtifactoryPlugin
import org.jfrog.gradle.plugin.artifactory.task.ArtifactoryTask

initscript {
    dependencies {
        classpath fileTree('${pluginLibDir}')
    }
}

addListener(new BuildInfoPluginListener())
class BuildInfoPluginListener extends BuildAdapter {

    def void projectsLoaded(Gradle gradle) {
        Map<String, String> projectProperties = new HashMap<String, String>(gradle.startParameter.getProjectProperties())
        projectProperties.put("build.start", Long.toString(System.currentTimeMillis()))
        gradle.startParameter.setProjectProperties(projectProperties)

        Project root = gradle.getRootProject()
        root.logger.debug("Artifactory plugin: projectsEvaluated: ${root.name}")
        if (!"buildSrc".equals(root.name)) {
            root.allprojects {
                apply {
                    apply plugin: ArtifactoryPlugin
                }
            }
        }

        // Set the "mavenJava" and "ivyJava" publications or
        // "archives" configuration to all Artifactory tasks.
        for (Project p : root.getAllprojects()) {
            Task t = p.getTasks().findByName(ArtifactoryTask.ARTIFACTORY_PUBLISH_TASK_NAME)
            if (t != null) {
                ArtifactoryTask task = (ArtifactoryTask) t
                task.setCiServerBuild()
            }
        }
    }
}
`
