package utils

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

const (
	configPropertiesPathTempPrefix = "extractorProperties"
	buildInfoPathKey               = "buildInfo.generated.build.info"
	buildNameKey                   = "buildInfo.build.name"
	buildNumberKey                 = "buildInfo.build.number"
	projectKey                     = "buildInfo.build.project"
)

// Download the relevant build-info-extractor jar, if it does not already exist locally.
// By default, the jar is downloaded directly from https://releases.jfrog.io/artifactory/oss-release-local.
//
// downloadPath: download path in the remote server.
// filename: The local file name.
// targetPath: The local download path (without the file name).
func downloadExtractorIfNeeded(downloadTo, filename, downloadPath string, downloadExtractorFunc func(downloadTo, downloadPath string) error, logger Log) error {
	// If the file exists locally, we're done.
	absFileName := filepath.Join(downloadTo, filename)
	exists, err := IsFileExists(absFileName, true)
	if exists || err != nil {
		return err
	}
	if err := os.MkdirAll(downloadTo, 0777); err != nil {
		return err
	}
	if downloadExtractorFunc != nil {
		// Override default download.
		return downloadExtractorFunc(absFileName, downloadPath)
	}
	extractorUrl := "https://releases.jfrog.io/artifactory/oss-release-local/" + downloadPath
	logger.Info("Downloading build-info-extractor from", extractorUrl, " to ", downloadTo)
	return DownloadFile(absFileName, extractorUrl)
}

// Save all the extractor's properties into a local file.
// extractorConfPath - Path to a file to which all the extractor's properties will be written.
// buildInfoPath - Path to a file to which the build-info data will be written.
// buildName - Build name of the current build.
// buildNumber - Build number of the current build.
// project - JFrog Project key of the current build
// configProperties - Data of the actual extractor's properties.
// Returns the extractor Config file path.
func CreateExtractorPropsFile(extractorConfPath, buildInfoPath, buildName, buildNumber, project string, configProperties map[string]string) (string, error) {
	if err := os.MkdirAll(extractorConfPath, 0777); err != nil {
		return "", err
	}
	propertiesFile, err := ioutil.TempFile(extractorConfPath, configPropertiesPathTempPrefix)
	if err != nil {
		return "", err
	}
	defer func() {
		deferErr := propertiesFile.Close()
		if err == nil {
			err = deferErr
		}
	}()
	var buildProperties = map[string]string{
		buildInfoPathKey: buildInfoPath,
		buildNameKey:     buildName,
		buildNumberKey:   buildNumber,
		projectKey:       project,
	}
	return propertiesFile.Name(), writeProps(propertiesFile, configProperties, buildProperties)
}

func DownloadDependencies(downloadTo, filename, relativefilePath string, downloadExtractorFunc func(downloadTo, downloadPath string) error, logger Log) error {
	downloadPath := path.Join(relativefilePath, filename)
	return downloadExtractorIfNeeded(downloadTo, filename, downloadPath, downloadExtractorFunc, logger)
}

func writeProps(propertiesFile *os.File, maps ...map[string]string) (err error) {
	for _, props := range maps {
		for key, value := range props {
			if value != "" {
				if _, err = propertiesFile.WriteString(key + "=" + value + "\n"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
