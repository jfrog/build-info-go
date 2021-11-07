package utils

import (
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"
)

func GetBuildDir(buildName, buildNumber, projectKey, buildsDirPath string) (string, error) {
	encodedDirName := base64.StdEncoding.EncodeToString([]byte(buildName + "_" + buildNumber + "_" + projectKey))
	buildsDir := filepath.Join(buildsDirPath, encodedDirName)
	err := os.MkdirAll(buildsDir, 0777)
	if err != nil {
		return "", err
	}
	return buildsDir, nil
}

func GetPartialsBuildDir(buildName, buildNumber, projectKey, buildsDirPath string) (string, error) {
	buildDir, err := GetBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return "", err
	}
	buildDir = filepath.Join(buildDir, "partials")
	err = os.MkdirAll(buildDir, 0777)
	if err != nil {
		return "", err
	}
	return buildDir, nil
}

// Create a temp file of build-info.
func CreateTempBuildFile(buildName, buildNumber, projectKey, buildsDirPath string, logger Log) (*os.File, error) {
	dirPath, err := GetBuildDir(buildName, buildNumber, projectKey, buildsDirPath)
	if err != nil {
		return nil, err
	}
	logger.Debug("Creating temp build file at: " + dirPath)
	tempFile, err := ioutil.TempFile(dirPath, "temp")
	if err != nil {
		return nil, err
	}
	return tempFile, nil
}
