package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	fileContent    = "Why did the robot bring a ladder to the bar? It heard the drinks were on the house."
	expectedMd5    = "70bd6370a86813f2504020281e4a2e2e"
	expectedSha1   = "8c3578ac814c9f02803001a5d3e5d78a7fd0f9cc"
	expectedSha256 = "093d901b28a59f7d95921f3f4fb97a03fe7a1cf8670507ffb1d6f9a01b3e890a"
)

func TestGetFileChecksums(t *testing.T) {
	// Create a temporary file
	tempFile, err := os.CreateTemp("", "TestGetFileChecksums")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, tempFile.Close())
		assert.NoError(t, os.Remove(tempFile.Name()))
	}()

	// Write something to the file
	_, err = tempFile.Write([]byte(fileContent))
	assert.NoError(t, err)

	// Calculate only sha1 and match
	checksums, err := GetFileChecksums(tempFile.Name(), SHA1)
	assert.NoError(t, err)
	assert.Len(t, checksums, 1)
	assert.Equal(t, expectedSha1, checksums[SHA1])

	// Calculate md5, sha1 and sha256 checksums and match
	checksums, err = GetFileChecksums(tempFile.Name())
	assert.NoError(t, err)
	assert.Equal(t, expectedMd5, checksums[MD5])
	assert.Equal(t, expectedSha1, checksums[SHA1])
	assert.Equal(t, expectedSha256, checksums[SHA256])
}
