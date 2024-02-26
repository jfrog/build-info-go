package utils

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestKeepFilesInTempDirIfExist(t *testing.T) {
	var testCases = []struct {
		amountFilesToCreate int
	}{
		{
			amountFilesToCreate: 0,
		},
		{
			amountFilesToCreate: 3,
		},
	}

	for _, test := range testCases {
		t.Run(fmt.Sprintf("with %d files created", test.amountFilesToCreate),
			func(t *testing.T) {
				tempDir, err := CreateTempDir()
				assert.NoError(t, err)
				defer func() {
					assert.NoError(t, RemoveTempDir(tempDir), "Couldn't remove temp dir")
				}()

				var filesList []string
				for i := 0; i < test.amountFilesToCreate; i++ {
					var file *os.File
					file, err = os.CreateTemp(tempDir, "tempfile")
					assert.NoError(t, err)
					filesList = append(filesList, file.Name())
				}
				containerDir, err := KeepFilesInTempDirIfExist(filesList)
				defer func() {
					assert.NoError(t, RemoveTempDir(containerDir), "Couldn't remove temp dir")
				}()
				assert.NoError(t, err)
				if test.amountFilesToCreate > 0 {
					assert.NotEqual(t, "", containerDir)

					for i := 0; i < test.amountFilesToCreate; i++ {
						fileName := filepath.Base(filesList[i])
						expectedFilePath := filepath.Join(containerDir, fileName)
						var exists bool
						exists, err = IsFileExists(expectedFilePath, false)
						assert.NoError(t, err)
						assert.True(t, exists)
					}
				} else {
					assert.Equal(t, "", containerDir)
				}
			})
	}

}
