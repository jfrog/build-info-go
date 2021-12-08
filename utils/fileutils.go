package utils

import (
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	tempDirPrefix = "build-info-temp-"

	// Max temp file age in hours
	maxFileAge = 24.0
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

// IsPathExists checks if a path exists.
func IsPathExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func GetFileContentAndInfo(filePath string) (fileContent []byte, fileInfo os.FileInfo, err error) {
	fileInfo, err = os.Stat(filePath)
	if err != nil {
		return
	}
	fileContent, err = ioutil.ReadFile(filePath)
	return
}

// CreateTempDir creates a temporary directory and returns its path.
func CreateTempDir() (string, error) {
	tempDirBase := os.TempDir()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	return ioutil.TempDir(tempDirBase, tempDirPrefix+timestamp+"-")
}

func RemoveTempDir(dirPath string) error {
	exists, err := IsDirExists(dirPath)
	if err != nil {
		return err
	}
	if exists {
		return os.RemoveAll(dirPath)
	}
	return nil
}

// Old runs/tests may leave junk at temp dir.
// Each temp file/Dir is named with prefix+timestamp, search for all temp files/dirs that match the common prefix and validate their timestamp.
func CleanOldDirs() error {
	// Get all files at temp dir
	tempDirBase := os.TempDir()
	files, err := ioutil.ReadDir(tempDirBase)
	if err != nil {
		return err
	}
	now := time.Now()
	// Search for files/dirs that match the template.
	for _, file := range files {
		if strings.HasPrefix(file.Name(), tempDirPrefix) {
			timeStamp, err := extractTimestamp(file.Name())
			if err != nil {
				return err
			}
			// Delete old file/dirs.
			if now.Sub(timeStamp).Hours() > maxFileAge {
				if err := os.RemoveAll(path.Join(tempDirBase, file.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func extractTimestamp(item string) (time.Time, error) {
	// Get timestamp from file/dir.
	endTimestampIndex := strings.LastIndex(item, "-")
	beginningTimestampIndex := strings.LastIndex(item[:endTimestampIndex], "-")
	timestampStr := item[beginningTimestampIndex+1 : endTimestampIndex]
	// Convert to int.
	timestampInt, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	// Convert to time type.
	return time.Unix(timestampInt, 0), nil
}
