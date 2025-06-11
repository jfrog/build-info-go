package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindFileInDirAndParents(t *testing.T) {
	const goModFileName = "go.mod"
	wd, err := os.Getwd()
	assert.NoError(t, err)
	projectRoot := filepath.Join(wd, "testdata", "project")

	// Find the file in the current directory
	root, err := FindFileInDirAndParents(projectRoot, goModFileName)
	assert.NoError(t, err)
	assert.Equal(t, projectRoot, root)

	// Find the file in the current directory's parent
	projectSubDirectory := filepath.Join(projectRoot, "dir")
	root, err = FindFileInDirAndParents(projectSubDirectory, goModFileName)
	assert.NoError(t, err)
	assert.Equal(t, projectRoot, root)

	// Look for a file that doesn't exist
	_, err = FindFileInDirAndParents(projectRoot, "notexist")
	assert.Error(t, err)
}

func TestReadNLines(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)
	path := filepath.Join(wd, "testdata", "oneline")
	lines, err := ReadNLines(path, 2)
	assert.NoError(t, err)
	assert.Len(t, lines, 1)
	assert.True(t, strings.HasPrefix(lines[0], ""))

	path = filepath.Join(wd, "testdata", "twolines")
	lines, err = ReadNLines(path, 2)
	assert.NoError(t, err)
	assert.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[1], "781"))
	assert.True(t, strings.HasSuffix(lines[1], ":true}}}"))

	path = filepath.Join(wd, "testdata", "threelines")
	lines, err = ReadNLines(path, 2)
	assert.NoError(t, err)
	assert.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[1], "781"))
	assert.True(t, strings.HasSuffix(lines[1], ":true}}}"))
}

func TestCreateTempDir(t *testing.T) {
	tempDir, err := CreateTempDir()
	assert.NoError(t, err)

	_, err = os.Stat(tempDir)
	assert.NotErrorIs(t, err, os.ErrNotExist)

	defer func() {
		// Check that a timestamp can be extracted from the temp dir name
		_, err = extractTimestamp(tempDir)
		assert.NoError(t, err)

		assert.NoError(t, os.RemoveAll(tempDir))
	}()
}

func TestCopyDir(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := CreateTempDir()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, os.RemoveAll(tempDir))
	}()

	// tmp/<unique-id>
	// -- subdir
	//    -- file1.txt
	//    -- file2.txt
	// -- file3.txt
	// -- subdir2
	//    -- subsubdir
	//       -- subfile.txt
	// -- target

	// (VALID) 1. source = tmp/<unique-id>/subdir, target = tmp/<unique-id>/target
	// (INVALID) 2. source = tmp/<unique-id>/subdir2, target = tmp/<unique-id>/subdir2
	// (INVALID) 3. source = tmp/<unique-id>, target = tmp/<unique-id>/subdir2

	// add subdirectory to the temp dir
	tempSubDir := filepath.Join(tempDir, "subdir")
	err = os.Mkdir(tempSubDir, 0755)
	assert.NoError(t, err)
	// add some files to the temp dir
	tempFile1 := filepath.Join(tempSubDir, "file1.txt")
	err = os.WriteFile(tempFile1, []byte("This is file 1"), 0644)
	assert.NoError(t, err)
	tempFile2 := filepath.Join(tempSubDir, "file2.txt")
	err = os.WriteFile(tempFile2, []byte("This is file 2"), 0644)
	assert.NoError(t, err)
	// create a file in the temp dir
	tempFile3 := filepath.Join(tempDir, "file3.txt")
	err = os.WriteFile(tempFile3, []byte("This is file 3"), 0644)
	assert.NoError(t, err)
	// create a subdirectory in the temp dir
	tempSubDir2 := filepath.Join(tempDir, "subdir2")
	err = os.Mkdir(tempSubDir2, 0755)
	assert.NoError(t, err)
	// add a subdirectory to the temp subdirectory
	tempSubSubDir := filepath.Join(tempSubDir2, "subsubdir")
	err = os.Mkdir(tempSubSubDir, 0755)
	assert.NoError(t, err)
	// add a file to the temp subdirectory
	tempSubFile := filepath.Join(tempSubSubDir, "subfile.txt")
	err = os.WriteFile(tempSubFile, []byte("This is a subfile"), 0644)
	assert.NoError(t, err)

	targetDir := filepath.Join(tempDir, "target")
	err = os.Mkdir(targetDir, 0755)
	assert.NoError(t, err)

	testCases := []struct {
		name          string
		sourceDir     string
		targetDir     string
		expectedError error
	}{
		{
			name:      "Valid copy",
			sourceDir: tempSubDir,
			targetDir: targetDir,
		},
		{
			name:          "Source and target are the same",
			sourceDir:     tempSubDir2,
			targetDir:     tempSubDir2,
			expectedError: fmt.Errorf("cannot copy directory from '%s' to '%s', because the source and destination are the same", tempSubDir2, tempSubDir2),
		},
		{
			name:      "Source is a subdirectory of target",
			sourceDir: tempSubDir2,
			targetDir: tempDir,
		},
		{
			name:          "Target is subdirectory of source",
			sourceDir:     tempDir,
			targetDir:     tempSubDir2,
			expectedError: fmt.Errorf("cannot copy directory from '%s' to '%s', because the destination is a subdirectory of the source", tempDir, tempSubDir2),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sourcePath := tc.sourceDir
			targetPath := tc.targetDir

			err := CopyDir(sourcePath, targetPath, true, nil)

			if tc.expectedError != nil {
				assert.Error(t, err)
				assert.EqualError(t, err, tc.expectedError.Error())
			} else {
				assert.NoError(t, err)

				// Verify that the target directory exists
				_, err = os.Stat(targetPath)
				assert.NoError(t, err)

				// Verify that the contents of the source exists in the target directory after copying
				sourceFiles, err := os.ReadDir(sourcePath)
				assert.NoError(t, err)
				targetFiles, err := os.ReadDir(targetPath)
				assert.NoError(t, err)

				assert.GreaterOrEqual(t, len(targetFiles), len(sourceFiles))
			}
		})
	}
}
