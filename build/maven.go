package build

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/utils"
)

const (
	MavenHome                       = "M2_HOME"
	MavenExtractorFileName          = "build-info-extractor-maven3-%s-uber.jar"
	classworldsConfFileName         = "classworlds.conf"
	PropertiesTempFolderName        = "properties"
	MavenExtractorRemotePath        = "org/jfrog/buildinfo/build-info-extractor-maven3/%s"
	MavenExtractorDependencyVersion = "2.41.7"

	ClassworldsConf = `main is org.apache.maven.cli.MavenCli from plexus.core

	set maven.home default ${user.home}/m2

	[plexus.core]
	load ${maven.home}/lib/*.jar
	load ${m3plugin.lib}/*.jar
	`
)

var mavenHomeRegex = regexp.MustCompile(`^Maven\shome:\s(.+)`)

type MavenModule struct {
	// The build which contains the maven module.
	containingBuild *Build
	// Project path in the file system.
	srcPath string
	// The Maven extractor (dependency) which calculates the build-info.
	extractorDetails *extractorDetails
	// A pipe to write the maven extractor output to.
	outputWriter io.Writer
}

// Maven extractor is the engine for calculating the project dependencies.
type extractorDetails struct {
	// Extractor local path.
	localPath string
	// Download the extractor from remote server.
	downloadExtractorFunc func(downloadTo, downloadFrom string) error
	// mvn goals to build the project.
	goals []string
	// Additional JVM option to build the project.
	mavenOpts []string
	// Map of configurations for the extractor.
	props map[string]string
	// Local path to the configuration file.
	propsDir string
	// Use the maven wrapper to build the project.
	useWrapper bool
}

// Add a new Maven module to a given build.
func newMavenModule(containingBuild *Build, srcPath string) (*MavenModule, error) {
	extractorProps := map[string]string{
		"org.jfrog.build.extractor.maven.recorder.activate": "true",
		"publish.add.deployable.artifacts":                  "false",
		"publish.buildInfo":                                 "false",
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	extractorLocalPath := filepath.Join(home, dependenciesDirName, "maven", MavenExtractorDependencyVersion)
	return &MavenModule{
		srcPath:         srcPath,
		containingBuild: containingBuild,
		extractorDetails: &extractorDetails{
			goals:     []string{"compile"},
			props:     extractorProps,
			localPath: extractorLocalPath,
			propsDir:  filepath.Join(containingBuild.tempDirPath, PropertiesTempFolderName),
		},
	}, err
}

func (mm *MavenModule) SetExtractorDetails(localExtractorPath, extractorPropsdir string, goals []string, downloadExtractorFunc func(downloadTo, downloadFrom string) error, configProps map[string]string, useWrapper bool) *MavenModule {
	mm.extractorDetails.localPath = localExtractorPath
	mm.extractorDetails.propsDir = extractorPropsdir
	mm.extractorDetails.downloadExtractorFunc = downloadExtractorFunc
	mm.extractorDetails.goals = goals
	mm.extractorDetails.useWrapper = useWrapper
	if configProps != nil {
		mm.extractorDetails.props = configProps
	}
	return mm
}

func (mm *MavenModule) SetOutputWriter(outputWriter io.Writer) {
	mm.outputWriter = outputWriter
}

func (mm *MavenModule) SetMavenGoals(goals ...string) {
	mm.extractorDetails.goals = goals
}

func (mm *MavenModule) SetMavenOpts(mavenOpts ...string) {
	mm.extractorDetails.mavenOpts = mavenOpts
}

func (mm *MavenModule) createMvnRunConfig() (*mvnRunConfig, error) {
	var javaExecPath string
	mavenHome, err := mm.loadMavenHome()
	if err != nil {
		return nil, err
	}
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		javaExecPath = filepath.Join(javaHome, "bin", "java")
	} else {
		javaExecPath, err = exec.LookPath("java")
		if err != nil {
			return nil, err
		}
	}
	plexusClassworlds, err := filepath.Glob(filepath.Join(mavenHome, "boot", "plexus-classworlds*.jar"))
	if err != nil {
		return nil, err
	}
	if len(plexusClassworlds) != 1 {
		return nil, errors.New("couldn't find plexus-classworlds-x.x.x.jar in Maven installation path, please check M2_HOME environment variable")
	}
	buildInfoPath, err := createEmptyBuildInfoFile(mm.containingBuild)
	if err != nil {
		return nil, err
	}
	extractorProps, err := utils.CreateExtractorPropsFile(mm.extractorDetails.propsDir, buildInfoPath, mm.containingBuild.buildName, mm.containingBuild.buildNumber, mm.containingBuild.buildTimestamp, mm.containingBuild.projectKey, mm.extractorDetails.props)
	if err != nil {
		return nil, err
	}
	return &mvnRunConfig{
		java:                javaExecPath,
		pluginDependencies:  mm.extractorDetails.localPath,
		plexusClassworlds:   plexusClassworlds[0],
		cleassworldsConfig:  filepath.Join(mm.extractorDetails.localPath, classworldsConfFileName),
		mavenHome:           mavenHome,
		workspace:           mm.srcPath,
		goals:               mm.extractorDetails.goals,
		buildInfoProperties: extractorProps,
		mavenOpts:           mm.extractorDetails.mavenOpts,
		logger:              mm.containingBuild.logger,
	}, nil
}

// Generates Maven build-info.
func (mm *MavenModule) CalcDependencies() (err error) {
	if mm.srcPath == "" {
		if mm.srcPath, err = os.Getwd(); err != nil {
			return err
		}
	}

	if err = downloadMavenExtractor(mm.extractorDetails.localPath, mm.extractorDetails.downloadExtractorFunc, mm.containingBuild.logger); err != nil {
		return
	}
	mvnRunConfig, err := mm.createMvnRunConfig()
	if err != nil {
		return
	}
	defer func() {
		fileExist, e := utils.IsFileExists(mvnRunConfig.buildInfoProperties, false)
		if fileExist && e == nil {
			e = os.Remove(mvnRunConfig.buildInfoProperties)
		}
		if err == nil {
			err = e
		}
	}()
	mvnRunConfig.SetOutputWriter(mm.outputWriter)
	mm.containingBuild.logger.Info("Running Mvn...")
	return mvnRunConfig.runCmd()
}

func (mm *MavenModule) loadMavenHome() (mavenHome string, err error) {
	mm.containingBuild.logger.Debug("Searching for Maven home.")
	mavenHome = os.Getenv(MavenHome)
	if mavenHome == "" {
		// The 'mavenHome' is not defined.
		// Since Maven installation can be located in different locations,
		// depending on the installation type and the OS (for example: For Mac with brew install: /usr/local/Cellar/maven/{version}/libexec or Ubuntu with debian: /usr/share/maven),
		// we need to grab the location using the mvn --version command.
		// First, we will try to look for 'mvn' in PATH.
		maven, err := mm.getExecutableName()
		if err != nil {
			return maven, err
		}
		if !mm.extractorDetails.useWrapper {
			mvnPath, err := mm.lookPath()
			if err != nil {
				return mvnPath, err
			}
		}
		versionOutput, err := mm.execMavenVersion(maven)
		if err != nil {
			return "", err
		}
		// Finding the relevant "Maven home" line in command response.
		mavenHome, err = mm.extractMavenPath(versionOutput)
		if err != nil {
			return "", err
		}
	}
	mm.containingBuild.logger.Debug("Maven home location:", mavenHome)

	return
}

func (mm *MavenModule) lookPath() (mvnPath string, err error) {
	mvnPath, err = exec.LookPath("mvn")
	err = mm.determineError(mvnPath, "", err)
	return
}

// This function generates an error with a clear message, based on the arguments it gets.
func (mm *MavenModule) determineError(mvnPath, versionOutput string, err error) error {
	if err != nil {
		if versionOutput == "" {
			return errors.New(err.Error() + "\nHint: The mvn command may not be included in the PATH. Either add it to the path or set the M2_HOME environment variable value to the maven installation directory, which is the directory that includes the bin and lib directories.")
		}
		return errors.New(err.Error() + "Could not find the location of the maven home directory, by running 'mvn --version' command. The command versionOutput is:\n" + versionOutput + "\nYou also have the option of setting the M2_HOME environment variable value to the maven installation directory, which is the directory which includes the bin and lib directories.")
	}
	if mvnPath == "" {
		if versionOutput == "" {
			return errors.New("hint: The mvn command may not be included in the PATH. Either add it to the path or set the M2_HOME environment variable value to the maven installation directory, which is the directory that includes the bin and lib directories")
		}
		return errors.New("Could not find the location of the maven home directory, by running 'mvn --version' command. The command versionOutput is:\n" + versionOutput + "\nYou also have the option of setting the M2_HOME environment variable value to the maven installation directory, which is the directory which includes the bin and lib directories.")
	}
	return nil
}

func (mm *MavenModule) getExecutableName() (maven string, err error) {
	maven = "mvn"
	if mm.extractorDetails.useWrapper {
		if utils.IsWindows() {
			maven, err = filepath.Abs("mvnw.cmd")
		} else {
			maven = "./mvnw"
		}
	}
	return
}

func (mm *MavenModule) execMavenVersion(maven string) (stdout bytes.Buffer, err error) {
	mm.containingBuild.logger.Debug(MavenHome, "is not defined. Retrieving Maven home using 'mvn --version' command.")
	cmd := exec.Command(maven, "--version")
	cmd.Stdout = &stdout
	err = cmd.Run()
	err = mm.determineError("mvn", stdout.String(), err)
	if err != nil {
		return stdout, err
	}

	return stdout, nil
}

func (mm *MavenModule) extractMavenPath(mavenVersionOutput bytes.Buffer) (mavenHome string, err error) {
	mavenVersionResultInArray := strings.Split(strings.TrimSpace(mavenVersionOutput.String()), "\n")
	for _, line := range mavenVersionResultInArray {
		line = strings.TrimSpace(line)
		// Search for 'Maven home: /path/to/maven/home' line
		regexMatch := mavenHomeRegex.FindStringSubmatch(line)
		if regexMatch != nil {
			foundPath := regexMatch[1]
			mavenHome, err = filepath.Abs(foundPath)
			err = mm.determineError(foundPath, mavenVersionOutput.String(), err)
			break
		}
	}
	return
}

func downloadMavenExtractor(downloadTo string, downloadExtractorFunc func(downloadTo, downloadPath string) error, logger utils.Log) error {
	filename := fmt.Sprintf(MavenExtractorFileName, MavenExtractorDependencyVersion)
	filePath := fmt.Sprintf(MavenExtractorRemotePath, MavenExtractorDependencyVersion)
	if err := utils.DownloadDependencies(downloadTo, filename, filePath, downloadExtractorFunc, logger); err != nil {
		return err
	}
	return createClassworldsConfig(downloadTo)
}

func createClassworldsConfig(dependenciesPath string) error {
	classworldsPath := filepath.Join(dependenciesPath, classworldsConfFileName)
	if utils.IsPathExists(classworldsPath) {
		return nil
	}
	return os.WriteFile(classworldsPath, []byte(ClassworldsConf), 0644)
}

func (config *mvnRunConfig) GetCmd() *exec.Cmd {
	var cmd []string
	cmd = append(cmd, config.java)
	cmd = append(cmd, "-classpath", config.plexusClassworlds)
	cmd = append(cmd, "-Dmaven.home="+config.mavenHome)
	cmd = append(cmd, "-DbuildInfoConfig.propertiesFile="+config.buildInfoProperties)
	cmd = append(cmd, "-Dm3plugin.lib="+config.pluginDependencies)
	cmd = append(cmd, "-Dclassworlds.conf="+config.cleassworldsConfig)
	cmd = append(cmd, "-Dmaven.multiModuleProjectDirectory="+config.workspace)
	if config.mavenOpts != nil {
		cmd = append(cmd, config.mavenOpts...)
	}
	cmd = append(cmd, "org.codehaus.plexus.classworlds.launcher.Launcher")
	cmd = append(cmd, config.goals...)
	return exec.Command(cmd[0], cmd[1:]...)
}

type mvnRunConfig struct {
	java                string
	plexusClassworlds   string
	cleassworldsConfig  string
	mavenHome           string
	pluginDependencies  string
	workspace           string
	goals               []string
	buildInfoProperties string
	mavenOpts           []string
	logger              utils.Log
	outputWriter        io.Writer
}

func (config *mvnRunConfig) SetOutputWriter(outputWriter io.Writer) *mvnRunConfig {
	config.outputWriter = outputWriter
	return config
}

func (config *mvnRunConfig) runCmd() error {
	command := config.GetCmd()
	command.Stderr = os.Stderr
	if config.outputWriter == nil {
		command.Stdout = os.Stderr
	} else {
		command.Stdout = config.outputWriter
	}
	command.Dir = config.workspace
	config.logger.Info("Running mvn command:", strings.Join(command.Args, " "))
	return command.Run()
}
