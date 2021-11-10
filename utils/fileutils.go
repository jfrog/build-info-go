package utils

import (
	"io"
	"io/ioutil"
	"net/http"
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

func DownloadFile(downloadTo string, fromUrl string) (err error) {
	// Get the data
	resp, err := http.Get(fromUrl)
	if err != nil {
		return err
	}
	defer func() {
		if deferErr := resp.Body.Close(); err == nil {
			err = deferErr
		}
	}()
	// Create the file
	out, err := os.Create(downloadTo)
	if err != nil {
		return err
	}
	defer func() {
		if deferErr := out.Close(); err == nil {
			err = deferErr
		}
	}()
	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return
}

func DoubleWinPathSeparator(filePath string) string {
	return strings.Replace(filePath, "\\", "\\\\", -1)
}

// Check if path exists.
func IsPathExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
