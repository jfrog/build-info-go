package utils

import (
	"io/ioutil"
	"os"
	"strings"
)

func IsFileExists(path string) (bool, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) { // If doesn't exist, don't omit an error
			return false, nil
		}
		return false, err
	}
	return !fileInfo.IsDir(), nil
}

func IsDirExists(path string) (bool, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) { // If doesn't exist, don't omit an error
			return false, nil
		}
		return false, err
	}
	return fileInfo.IsDir(), nil
}

// ListFiles returns a list of files and directories in the specified path
func ListFiles(path string) ([]string, error) {
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(path, sep) {
		path += sep
	}
	var fileList []string
	files, _ := ioutil.ReadDir(path)
	path = strings.TrimPrefix(path, "."+sep)

	for _, f := range files {
		filePath := path + f.Name()
		exists, err := IsFileExists(filePath)
		if err != nil {
			return nil, err
		}
		if exists {
			fileList = append(fileList, filePath)
		}
	}
	return fileList, nil
}
