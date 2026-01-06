package build

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveUserSpecificBuildDirName(t *testing.T) {

	// in windows temporary directories are per-user basis already
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}
	result := resolveUserSpecificBuildDirName()

	// Should contain "jfrog-" prefix
	assert.True(t, strings.Contains(result, BuildsJfrogPath), "Expected path to contain '%s', got: %s", BuildsJfrogPath, result)

	// Should contain "builds" directory
	assert.True(t, strings.Contains(result, BuildsDirPath), "Expected path to contain '%s', got: %s", BuildsDirPath, result)

	// Should have format: jfrog-<username>/builds
	parts := strings.Split(result, string(filepath.Separator))
	// Handle both forward and backslash separators
	if len(parts) == 1 {
		parts = strings.Split(result, "/")
	}
	assert.Equal(t, 2, len(parts), "Expected 2 path components, got: %v", parts)
	assert.True(t, strings.HasPrefix(parts[0], BuildsJfrogPath), "First component should start with '%s', got: %s", BuildsJfrogPath, parts[0])
	assert.Equal(t, BuildsDirPath, parts[1], "Second component should be '%s', got: %s", BuildsDirPath, parts[1])
}

func TestResolveUserSpecificBuildDirNameContainsUsername(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err, "Failed to get current user")

	result := resolveUserSpecificBuildDirName()

	// The path should contain the current username
	expectedPrefix := BuildsJfrogPath + currentUser.Username
	assert.True(t, strings.Contains(result, expectedPrefix),
		"Expected path to contain '%s', got: %s", expectedPrefix, result)
}

func TestNewBuildInfoService(t *testing.T) {
	service := NewBuildInfoService()

	// Service should not be nil
	require.NotNil(t, service, "NewBuildInfoService should not return nil")

	// tempDirPath should be set
	assert.NotEmpty(t, service.tempDirPath, "tempDirPath should not be empty")

	// tempDirPath should start with OS temp directory
	assert.True(t, strings.HasPrefix(service.tempDirPath, os.TempDir()),
		"tempDirPath should start with OS temp dir. Got: %s, Expected prefix: %s",
		service.tempDirPath, os.TempDir())

	// tempDirPath should contain user-specific directory
	assert.True(t, strings.Contains(service.tempDirPath, BuildsJfrogPath),
		"tempDirPath should contain '%s', got: %s", BuildsJfrogPath, service.tempDirPath)
}

func TestGetUserSpecificBuildDirName(t *testing.T) {
	service := NewBuildInfoService()

	dirName := service.GetUserSpecificBuildDirName()

	// Should not be empty
	assert.NotEmpty(t, dirName, "GetUserSpecificBuildDirName should not return empty string")

	// Should match the expected format
	assert.True(t, strings.HasPrefix(dirName, BuildsJfrogPath),
		"Directory name should start with '%s', got: %s", BuildsJfrogPath, dirName)
	assert.True(t, strings.HasSuffix(dirName, BuildsDirPath),
		"Directory name should end with '%s', got: %s", BuildsDirPath, dirName)
}

func TestGetUserSpecificBuildDirNameLazyInit(t *testing.T) {
	// Create service with empty buildDirectory to test lazy initialization
	service := &BuildInfoService{
		tempDirPath: "/tmp/test",
	}

	dirName := service.GetUserSpecificBuildDirName()

	// Should initialize and return valid directory name
	assert.NotEmpty(t, dirName, "GetUserSpecificBuildDirName should initialize if empty")
	assert.True(t, strings.HasPrefix(dirName, BuildsJfrogPath),
		"Lazily initialized directory should start with '%s', got: %s", BuildsJfrogPath, dirName)
}

func TestMultipleUsersGetDifferentPaths(t *testing.T) {
	// This test verifies that the path includes a user identifier
	// that would be different for different users
	service := NewBuildInfoService()

	path := service.tempDirPath

	// Path should contain username component
	currentUser, err := user.Current()
	require.NoError(t, err)

	expectedUserDir := BuildsJfrogPath + currentUser.Username
	assert.True(t, strings.Contains(path, expectedUserDir),
		"Path should contain user-specific directory '%s', got: %s", expectedUserDir, path)
}
