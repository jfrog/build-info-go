package build

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	buildutil "github.com/jfrog/build-info-go/build/utils"
	"github.com/jfrog/jfrog-client-go/utils/io/fileutils"
	"github.com/jfrog/jfrog-client-go/utils/log"
)

const (
	MavenHome                       = "M2_HOME"
	buildInfoPathKey                = "buildInfo.generated.build.info"
	dependenciesDirName             = ".build-info"
	MavenExtractorFileName          = "build-info-extractor-maven3-%s-uber.jar"
	classworldsConfFileName         = "classworlds.conf"
	PropertiesTempfolderName        = "properties"
	mavenExtractorRemotePath        = "org/jfrog/buildinfo/build-info-extractor-maven3/%s"
	GeneratedBuildInfoTempPrefix    = "generatedBuildInfo"
	MavenExtractorDependencyVersion = "2.30.2"

	ClassworldsConf = `main is org.apache.maven.cli.MavenCli from plexus.core

	set maven.home default ${user.home}/m2

	[plexus.core]
	load ${maven.home}/lib/*.jar
	load ${m3plugin.lib}/*.jar
	`
)

type MavenModule struct {
	// The build which contains the maven module.
	containingBuild *Build
	// Project path in the file system.
	srcPath string
	// The Maven extractor (dependency) which calculates the build-info.
	extractorDetails *extractorDetails
}

// Maven extractor is the engine for calculating the project dependencies.
type extractorDetails struct {
	// Extractor local path.
	localPath string
	// Download the extracor from remote server.
	downloadExtractorFunc func(downloadTo, downloadFrom string) error
	// mvn goals to build the project.
	goals []string
	// Additionals JVM option to build the project.
	mavenOpts []string
	// Map of configurations for the extractor.
	props map[string]string
	// Local path to the configuration file.
	propsDir string
}

// Add a new Maven module to a given build.
func newMavenModule(containingBuild *Build, srcPath string) (*MavenModule, error) {
	log.SetLogger(containingBuild.logger)
	extractorProps := map[string]string{
		"org.jfrog.build.extractor.maven.recorder.activate": "true",
		"publish.artifacts": "false",
		"publish.buildInfo": "false",
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	extractorLocalPath := filepath.Join(home, dependenciesDirName, "maven")
	return &MavenModule{
		srcPath:         srcPath,
		containingBuild: containingBuild,
		extractorDetails: &extractorDetails{
			goals:     []string{"compile"},
			props:     extractorProps,
			localPath: extractorLocalPath,
			propsDir:  filepath.Join(containingBuild.tempDirPath, PropertiesTempfolderName),
		},
	}, err
}

func (mm *MavenModule) SetExtractorDetails(localdExtractorPath, extractorPropsdir string, goals []string, downloadExtractorFunc func(downloadTo, downloadFrom string) error, configProps map[string]string) *MavenModule {
	mm.extractorDetails.localPath = localdExtractorPath
	mm.extractorDetails.propsDir = extractorPropsdir
	mm.extractorDetails.downloadExtractorFunc = downloadExtractorFunc
	mm.extractorDetails.goals = goals
	if configProps != nil {
		configProps[buildInfoPathKey] = mm.extractorDetails.props[buildInfoPathKey]
		mm.extractorDetails.props = configProps
	}
	return mm
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
	buildInfoPath, err := generateEmptyBIFile(mm.containingBuild)
	mm.extractorDetails.props[buildInfoPathKey] = buildInfoPath
	extractorProps, err := buildutil.CreateExtractorPropsFile(mm.extractorDetails.propsDir, mm.extractorDetails.props)
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
	}, nil
}

// Generates Maven build-info.
func (mm *MavenModule) CalcDependencies() error {
	if mm.srcPath == "" {
		var err error
		if mm.srcPath, err = os.Getwd(); err != nil {
			return err
		}
	}

	err := downloadMavenExtractor(mm.extractorDetails.localPath, mm.extractorDetails.downloadExtractorFunc)
	if err != nil {
		return err
	}
	mvnRunConfig, err := mm.createMvnRunConfig()
	if err != nil {
		return err
	}

	defer os.Remove(mvnRunConfig.buildInfoProperties)
	log.Info("Running Mvn...")
	return mvnRunConfig.runCmd()
}

func (mm *MavenModule) loadMavenHome() (mavenHome string, err error) {
	log.Debug("Searching for Maven home.")
	mavenHome = os.Getenv(MavenHome)
	if mavenHome == "" {
		// The 'mavenHome' is not defined.
		// Since Maven installation can be located in different locations,
		// Depending on the installation type and the OS (for example: For Mac with brew install: /usr/local/Cellar/maven/{version}/libexec or Ubuntu with debian: /usr/share/maven),
		// We need to grab the location using the mvn --version command

		// First we will try lo look for 'mvn' in PATH.
		mvnPath, err := exec.LookPath("mvn")
		if err != nil || mvnPath == "" {
			return "", errors.New(err.Error() + "Hint: The mvn command may not be included in the PATH. Either add it to the path, or set the M2_HOME environment variable value to the maven installation directory, which is the directory which includes the bin and lib directories.")
		}
		log.Debug(MavenHome, " is not defined. Retrieving Maven home using 'mvn --version' command.")
		cmd := exec.Command("mvn", "--version")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err = cmd.Run()
		if err != nil {
			return "", err
		}
		output := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		// Finding the relevant "Maven home" line in command response.
		for _, line := range output {
			if strings.HasPrefix(line, "Maven home:") {
				mavenHome = strings.Split(line, " ")[2]
				break
			}
		}
		if mavenHome == "" {
			return "", errors.New("Could not find the location of the maven home directory, by running 'mvn --version' command. The command output is:\n" + stdout.String() + "\nYou also have the option of setting the M2_HOME environment variable value to the maven installation directory, which is the directory which includes the bin and lib directories.")
		}
	}
	log.Debug("Maven home location: ", mavenHome)
	return
}

func downloadMavenExtractor(downloadTo string, downloadExtractorFunc func(downloadTo, downloadPath string) error) error {
	filename := fmt.Sprintf(MavenExtractorFileName, GradleExtractorDependencyVersion)
	filePath := fmt.Sprintf(mavenExtractorRemotePath, GradleExtractorDependencyVersion)
	if err := buildutil.DownloadDependencies(downloadTo, filename, filePath, downloadExtractorFunc); err != nil {
		return err
	}
	return createClassworldsConfig(downloadTo)
}

func createClassworldsConfig(dependenciesPath string) error {
	classworldsPath := filepath.Join(dependenciesPath, classworldsConfFileName)
	if fileutils.IsPathExists(classworldsPath, false) {
		return nil
	}
	return ioutil.WriteFile(classworldsPath, []byte(ClassworldsConf), 0644)
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
}

func (config *mvnRunConfig) runCmd() error {
	command := config.GetCmd()
	command.Stderr = os.Stderr
	command.Stdout = os.Stderr
	return command.Run()
}

func CreateBuildInfoService() *BuildInfoService {
	buildInfoService := NewBuildInfoService()
	buildInfoService.SetLogger(log.Logger)
	return buildInfoService
}
