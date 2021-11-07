package utils

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/jfrog-client-go/utils/log"
)

const configPropertiesPathTempPrefix = "extractorProperties"

// Download the relevant build-info-extractor jar, if it does not already exist locally.
// By default, the jar is downloaded directly from https://releases.jfrog.io/artifactory/oss-release-local.
//
// downloadPath: download path in the remote server.
// filename: The local file name.
// targetPath: The local download path (without the file name).
func DownloadExtractorIfNeeded(downloadTo, filename, downloadPath string, downloadExtractorFunc func(downloadTo, downloadPath string) error) error {
	// If the file exists locally, we're done.
	absFileName := filepath.Join(downloadTo, filename)
	exists, err := utils.IsFileExists(absFileName)
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
	log.Info("Downloading build-info-extractor from", extractorUrl, " to ", downloadTo)
	return utils.DownloadFile(absFileName, extractorUrl)
}

func CreateExtractorPropsFile(configPropertiesPath string, configProperties map[string]string) (string, error) {
	if err := os.MkdirAll(configPropertiesPath, 0777); err != nil {
		return "", err
	}
	propertiesFile, err := ioutil.TempFile(configPropertiesPath, configPropertiesPathTempPrefix)
	if err != nil {
		return "", err
	}
	defer func() {
		deferErr := propertiesFile.Close()
		if err == nil {
			err = deferErr
		}
	}()
	// Iterate over all the required properties keys according to the buildType and create properties file.
	// If a value is provided by the build config file write it,
	// otherwise use the default value from defaultPropertiesValues map.
	for key, value := range configProperties {
		if _, err = propertiesFile.WriteString(key + "=" + value + "\n"); err != nil {
			return "", err
		}
	}
	return propertiesFile.Name(), nil
}

func DownloadDependencies(downloadTo, filename, relativefilePath string, downloadExtractorFunc func(downloadTo, downloadPath string) error) error {
	downloadPath := path.Join(relativefilePath, filename)
	return DownloadExtractorIfNeeded(downloadTo, filename, downloadPath, downloadExtractorFunc)
}
