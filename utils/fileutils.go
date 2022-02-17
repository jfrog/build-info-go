package utils

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	tempDirPrefix = "build-info-temp-"

	// Max temp file age in hours
	maxFileAge = 24.0
)

// Check if path points at a file.
// If path points at a symlink and `followSymlink == false`,
// function will return `true` regardless of the symlink target
func IsFileExists(path string, followSymlink bool) (bool, error) {
	fileInfo, err := GetFileInfo(path, followSymlink)
	if err != nil {
		if os.IsNotExist(err) { // If doesn't exist, don't omit an error
			return false, nil
		}
		return false, err
	}
	return !fileInfo.IsDir(), nil
}

// Check if path points at a directory.
// If path points at a symlink and `followSymlink == false`,
// function will return `false` regardless of the symlink target
func IsDirExists(path string, followSymlink bool) (bool, error) {
	fileInfo, err := GetFileInfo(path, followSymlink)
	if err != nil {
		if os.IsNotExist(err) { // If doesn't exist, don't omit an error
			return false, nil
		}
		return false, err
	}
	return fileInfo.IsDir(), nil
}

// Get the file info of the file in path.
// If path points at a symlink and `followSymlink == false`, return the file info of the symlink instead
func GetFileInfo(path string, followSymlink bool) (fileInfo os.FileInfo, err error) {
	if followSymlink {
		fileInfo, err = os.Lstat(path)
	} else {
		fileInfo, err = os.Stat(path)
	}
	// We should not do CheckError here, because the error is checked by the calling functions.
	return fileInfo, err
}

// Move directory content from one path to another.
func MoveDir(fromPath, toPath string) error {
	err := CreateDirIfNotExist(toPath)
	if err != nil {
		return err
	}

	files, err := ListFiles(fromPath, true)
	if err != nil {
		return err
	}

	for _, v := range files {
		dir, err := IsDirExists(v, true)
		if err != nil {
			return err
		}

		if dir {
			toPath := toPath + GetFileSeparator() + filepath.Base(v)
			err := MoveDir(v, toPath)
			if err != nil {
				return err
			}
			continue
		}
		err = MoveFile(v, filepath.Join(toPath, filepath.Base(v)))
		if err != nil {
			return err
		}
	}
	return err
}

// GoLang: os.Rename() give error "invalid cross-device link" for Docker container with Volumes.
// MoveFile(source, destination) will work moving file between folders
// Therefore, we are using our own implementation (MoveFile) in order to rename files.
func MoveFile(sourcePath, destPath string) (err error) {
	inputFileOpen := true
	var inputFile *os.File
	inputFile, err = os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		if inputFileOpen {
			e := inputFile.Close()
			if err == nil {
				err = e
			}
		}
	}()
	inputFileInfo, err := inputFile.Stat()
	if err != nil {
		return err
	}

	var outputFile *os.File
	outputFile, err = os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() {
		e := outputFile.Close()
		if err == nil {
			err = e
		}
	}()

	_, err = io.Copy(outputFile, inputFile)
	if err != nil {
		return err
	}
	err = os.Chmod(destPath, inputFileInfo.Mode())
	if err != nil {
		return err
	}

	// The copy was successful, so now delete the original file
	err = inputFile.Close()
	if err != nil {
		return err
	}
	inputFileOpen = false
	err = os.Remove(sourcePath)
	return err
}

// Return the list of files and directories in the specified path
func ListFiles(path string, includeDirs bool) ([]string, error) {
	sep := GetFileSeparator()
	if !strings.HasSuffix(path, sep) {
		path += sep
	}
	fileList := []string{}
	files, _ := ioutil.ReadDir(path)
	path = strings.TrimPrefix(path, "."+sep)

	for _, f := range files {
		filePath := path + f.Name()
		exists, err := IsFileExists(filePath, false)
		if err != nil {
			return nil, err
		}
		if exists || IsPathSymlink(filePath) {
			fileList = append(fileList, filePath)
		} else if includeDirs {
			isDir, err := IsDirExists(filePath, false)
			if err != nil {
				return nil, err
			}
			if isDir {
				fileList = append(fileList, filePath)
			}
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
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("download failed. status code: %s", resp.Status)
		return
	}
	// Create the file
	var out *os.File
	out, err = os.Create(downloadTo)
	if err != nil {
		return
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
	exists, err := IsDirExists(dirPath, true)
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

// FindFileInDirAndParents looks for a file named fileName in dirPath and its parents, and returns the path of the directory where it was found.
// dirPath must be a full path.
func FindFileInDirAndParents(dirPath, fileName string) (string, error) {
	// Create a map to store all paths visited, to avoid running in circles.
	visitedPaths := make(map[string]bool)
	currDir := dirPath
	for {
		// If the file is found in the current directory, return the path.
		exists, err := IsFileExists(filepath.Join(currDir, fileName), true)
		if err != nil || exists {
			return currDir, err
		}

		// Save this path.
		visitedPaths[currDir] = true

		// CD to the parent directory.
		currDir = filepath.Dir(currDir)

		// If we already visited this directory, it means that there's a loop and we can stop.
		if visitedPaths[currDir] {
			return "", errors.New(fmt.Sprintf("could not find the %s file of the project", fileName))
		}
	}
}

// Copy directory content from one path to another.
// includeDirs means to copy also the dirs if presented in the src folder.
// excludeNames - Skip files/dirs in the src folder that match names in provided slice. ONLY excludes first layer (only in src folder).
func CopyDir(fromPath, toPath string, includeDirs bool, excludeNames []string) error {
	err := CreateDirIfNotExist(toPath)
	if err != nil {
		return err
	}

	files, err := ListFiles(fromPath, includeDirs)
	if err != nil {
		return err
	}

	for _, v := range files {
		// Skip if excluded
		if IsStringInSlice(filepath.Base(v), excludeNames) {
			continue
		}

		dir, err := IsDirExists(v, false)
		if err != nil {
			return err
		}

		if dir {
			toPath := toPath + GetFileSeparator() + filepath.Base(v)
			err := CopyDir(v, toPath, true, nil)
			if err != nil {
				return err
			}
			continue
		}
		err = CopyFile(toPath, v)
		if err != nil {
			return err
		}
	}
	return err
}

func CopyFile(dst, src string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	fileName, _ := GetFileAndDirFromPath(src)
	dstPath, err := CreateFilePath(dst, fileName)
	if err != nil {
		return err
	}
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	_, err = io.Copy(dstFile, srcFile)
	return err
}

func GetFileSeparator() string {
	return string(os.PathSeparator)
}

// Return the file's name and dir of a given path by finding the index of the last separator in the path.
// Support separators : "/" , "\\" and "\\\\"
func GetFileAndDirFromPath(path string) (fileName, dir string) {
	index1 := strings.LastIndex(path, "/")
	index2 := strings.LastIndex(path, "\\")
	var index int
	offset := 0
	if index1 >= index2 {
		index = index1
	} else {
		index = index2
		// Check if the last separator is "\\\\" or "\\".
		index3 := strings.LastIndex(path, "\\\\")
		if index3 != -1 && index2-index3 == 1 {
			offset = 1
		}
	}
	if index != -1 {
		fileName = path[index+1:]
		// If the last separator is "\\\\" index will contain the index of the last "\\" ,
		// to get the dir path (without separator suffix) we will use the offset's value.
		dir = path[:index-offset]
		return
	}
	fileName = path
	dir = ""
	return
}

func CreateFilePath(localPath, fileName string) (string, error) {
	if localPath != "" {
		err := os.MkdirAll(localPath, 0777)
		if err != nil {
			return "", err
		}
		fileName = filepath.Join(localPath, fileName)
	}
	return fileName, nil
}

func CreateDirIfNotExist(path string) error {
	exist, err := IsDirExists(path, false)
	if exist || err != nil {
		return err
	}
	_, err = CreateFilePath(path, "")
	return err
}

func IsStringInSlice(string string, strings []string) bool {
	for _, v := range strings {
		if v == string {
			return true
		}
	}
	return false
}

func IsPathSymlink(path string) bool {
	f, _ := os.Lstat(path)
	return f != nil && IsFileSymlink(f)
}

func IsFileSymlink(file os.FileInfo) bool {
	return file.Mode()&os.ModeSymlink != 0
}

// Parses the JSON-encoded data and stores the result in the value pointed to by 'loadTarget'.
// filePath - Path to json file.
// loadTarget - Pointer to a struct
func Unmarshal(filePath string, loadTarget interface{}) (err error) {
	var jsonFile *os.File
	jsonFile, err = os.Open(filePath)
	if err != nil {
		return
	}
	defer func() {
		closeErr := jsonFile.Close()
		if err == nil {
			err = closeErr
		}
	}()
	var byteValue []byte
	byteValue, err = ioutil.ReadAll(jsonFile)
	if err != nil {
		return
	}
	return json.Unmarshal(byteValue, &loadTarget)
}

// strip '\n' or read until EOF, return error if read error
// readNLines reads up to 'total' number of lines separated by \n.
func ReadNLines(path string, total int) (lines []string, err error) {
	reader, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		deferErr := reader.Close()
		if err == nil {
			err = deferErr
		}
	}()
	r := bufio.NewReader(reader)
	for i := 0; i < total; i++ {
		line, _, err := r.ReadLine()
		lines = append(lines, string(line))
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}
