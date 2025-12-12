package flexpack

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

func (gf *GradleFlexPack) getGradleExecutablePath() string {
	wrapperPath := filepath.Join(gf.config.WorkingDirectory, "gradlew")
	if _, err := os.Stat(wrapperPath); err == nil {
		return wrapperPath
	}

	// Check for Windows wrapper
	wrapperPathBat := filepath.Join(gf.config.WorkingDirectory, "gradlew.bat")
	if _, err := os.Stat(wrapperPathBat); err == nil {
		return wrapperPathBat
	}

	// Default to system Gradle
	gradleExec, err := exec.LookPath("gradle")
	if err != nil {
		log.Warn("Gradle executable not found in PATH, using 'gradle' as fallback")
		return "gradle"
	}
	return gradleExec
}

func (gf *GradleFlexPack) runGradleCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(gf.ctx, gf.config.CommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, gf.config.GradleExecutable, args...)
	cmd.Dir = gf.config.WorkingDirectory

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("gradle command timed out after %v: %s", gf.config.CommandTimeout, strings.Join(args, " "))
	}
	if err != nil {
		return output, fmt.Errorf("gradle command failed: %w", err)
	}
	return output, nil
}

func (gf *GradleFlexPack) isGradleVersionCompatible(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 1 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Debug("Failed to parse Gradle major version: " + err.Error())
		return false
	}
	return major >= 5
}

func (gf *GradleFlexPack) validatePathWithinWorkingDir(resolvedPath string) bool {
	cleanWorkingDir := filepath.Clean(gf.config.WorkingDirectory)
	cleanResolvedPath := filepath.Clean(resolvedPath)

	absWorkingDir, err := filepath.Abs(cleanWorkingDir)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get absolute path for working directory: %s", err.Error()))
		return false
	}
	absResolvedPath, err := filepath.Abs(cleanResolvedPath)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get absolute path for resolved path: %s", err.Error()))
		return false
	}
	absWorkingDir = filepath.Clean(absWorkingDir)
	absResolvedPath = filepath.Clean(absResolvedPath)
	if absResolvedPath == absWorkingDir {
		return true
	}
	separator := string(filepath.Separator)
	expectedPrefix := absWorkingDir + separator
	return strings.HasPrefix(absResolvedPath, expectedPrefix)
}

func safeJoinPath(baseDir string, components ...string) (string, error) {
	// Validate each component individually
	for _, component := range components {
		if component == "" {
			return "", fmt.Errorf("empty path component")
		}
		// Check for path traversal patterns
		if strings.Contains(component, "..") {
			return "", fmt.Errorf("path traversal pattern detected in component: %s", component)
		}
		// Check for absolute path indicators
		if filepath.IsAbs(component) {
			return "", fmt.Errorf("absolute path not allowed in component: %s", component)
		}
		// Check for path separators within the component (components should be single directory names)
		if strings.ContainsAny(component, `/\`) {
			return "", fmt.Errorf("path separator not allowed in component: %s", component)
		}
	}

	// Get absolute path of base directory
	absBaseDir, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for base directory: %w", err)
	}

	// Construct the path by joining base with components
	// We rebuild the path from scratch using only the validated component values
	result := absBaseDir
	for _, component := range components {
		// Use only the base name to ensure no path separators sneak through
		safeName := filepath.Base(component)
		if safeName != component || safeName == "." || safeName == ".." {
			return "", fmt.Errorf("invalid path component after sanitization: %s", component)
		}
		result = filepath.Join(result, safeName)
	}

	// Clean and get absolute path of result
	absResult, err := filepath.Abs(filepath.Clean(result))
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for result: %w", err)
	}

	// Verify the result is within the base directory
	absBaseDir = filepath.Clean(absBaseDir)
	absResult = filepath.Clean(absResult)

	if absResult == absBaseDir {
		return absResult, nil
	}

	separator := string(filepath.Separator)
	expectedPrefix := absBaseDir + separator
	if !strings.HasPrefix(absResult, expectedPrefix) {
		return "", fmt.Errorf("path traversal attempt: result %s escapes base %s", absResult, absBaseDir)
	}

	return absResult, nil
}

// The filename is validated and sanitized before joining.
func safeJoinFilename(dir, filename string) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("empty filename")
	}
	// Check for path traversal patterns in filename
	if strings.Contains(filename, "..") {
		return "", fmt.Errorf("path traversal pattern detected in filename: %s", filename)
	}
	// Check for path separators in filename
	if strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("path separator not allowed in filename: %s", filename)
	}

	// Get absolute path of directory
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for directory: %w", err)
	}

	// Use filepath.Base to ensure filename is just a name
	safeName := filepath.Base(filename)
	if safeName != filename || safeName == "." || safeName == ".." {
		return "", fmt.Errorf("invalid filename after sanitization: %s", filename)
	}

	// Join and verify
	result := filepath.Join(absDir, safeName)
	absResult, err := filepath.Abs(filepath.Clean(result))
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for result: %w", err)
	}

	// Verify result is within directory
	separator := string(filepath.Separator)
	expectedPrefix := absDir + separator
	if !strings.HasPrefix(absResult, expectedPrefix) {
		return "", fmt.Errorf("path traversal attempt: result %s escapes directory %s", absResult, absDir)
	}

	return absResult, nil
}

// getBuildFileContent reads the build.gradle or build.gradle.kts file for a module.
func (gf *GradleFlexPack) getBuildFileContent(moduleName string) ([]byte, string, error) {
	subPath := ""
	if moduleName != "" {
		// moduleName is "a:b" -> "a/b"
		subPath = strings.ReplaceAll(moduleName, ":", string(filepath.Separator))
	}

	buildGradlePath := filepath.Join(gf.config.WorkingDirectory, subPath, "build.gradle")
	buildGradleKtsPath := filepath.Join(gf.config.WorkingDirectory, subPath, "build.gradle.kts")

	if !gf.validatePathWithinWorkingDir(buildGradlePath) {
		return nil, "", fmt.Errorf("path traversal attempt detected for module %s (build.gradle)", moduleName)
	}
	if !gf.validatePathWithinWorkingDir(buildGradleKtsPath) {
		return nil, "", fmt.Errorf("path traversal attempt detected for module %s (build.gradle.kts)", moduleName)
	}

	if _, err := os.Stat(buildGradlePath); err == nil {
		content, err := os.ReadFile(buildGradlePath)
		return content, buildGradlePath, err
	}

	if _, err := os.Stat(buildGradleKtsPath); err == nil {
		content, err := os.ReadFile(buildGradleKtsPath)
		return content, buildGradleKtsPath, err
	}
	return nil, "", fmt.Errorf("%w: neither build.gradle nor build.gradle.kts found", os.ErrNotExist)
}

func (gf *GradleFlexPack) readSettingsFile() (string, error) {
	settingsPath := filepath.Join(gf.config.WorkingDirectory, "settings.gradle")
	settingsKtsPath := filepath.Join(gf.config.WorkingDirectory, "settings.gradle.kts")

	if _, err := os.Stat(settingsPath); err == nil {
		data, err := os.ReadFile(settingsPath)
		return string(data), err
	}
	if _, err := os.Stat(settingsKtsPath); err == nil {
		data, err := os.ReadFile(settingsKtsPath)
		return string(data), err
	}
	return "", nil
}

func (gf *GradleFlexPack) validateAndNormalizeScopes(scopes []string) []string {
	validScopes := map[string]bool{
		"compile":  true,
		"runtime":  true,
		"test":     true,
		"provided": true,
		"system":   true,
	}

	var normalized []string
	for _, scope := range scopes {
		if validScopes[scope] {
			normalized = append(normalized, scope)
		}
	}
	if len(normalized) == 0 {
		normalized = []string{"compile"}
	}
	return normalized
}

func getGradleCacheBase() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	gradleUserHome := os.Getenv("GRADLE_USER_HOME")
	if gradleUserHome == "" {
		// Default to ~/.gradle - this is a safe, known path
		gradleUserHome = filepath.Join(homeDir, ".gradle")
	} else {
		// Validate the environment variable value
		// Check for path traversal patterns
		if strings.Contains(gradleUserHome, "..") {
			return "", fmt.Errorf("path traversal pattern detected in GRADLE_USER_HOME: %s", gradleUserHome)
		}
	}

	// Clean and convert to absolute path to normalize the path
	cleanPath := filepath.Clean(gradleUserHome)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for GRADLE_USER_HOME: %w", err)
	}

	// Construct the cache base path using known, safe path components
	// Note: We don't check if the path exists here - the caller will handle non-existent paths
	cacheBase := filepath.Join(absPath, "caches", "modules-2", "files-2.1")
	cleanCacheBase := filepath.Clean(cacheBase)
	absCacheBase, err := filepath.Abs(cleanCacheBase)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for cache base: %w", err)
	}

	return absCacheBase, nil
}

func (gf *GradleFlexPack) findGradleArtifact(dep flexpack.DependencyInfo) string {
	parts := strings.Split(dep.Name, ":")
	if len(parts) != 2 {
		return ""
	}

	group := parts[0]
	module := parts[1]

	// Get a validated, sanitized cache base path
	cacheBase, err := getGradleCacheBase()
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get Gradle cache base: %s", err.Error()))
		return ""
	}

	// Securely construct and validate the module path - this returns a sanitized path
	safeModulePath, err := safeJoinPath(cacheBase, group, module, dep.Version)
	if err != nil {
		log.Debug(fmt.Sprintf("Invalid module path components: %s", err.Error()))
		return ""
	}

	if _, err := os.Stat(safeModulePath); os.IsNotExist(err) {
		return ""
	}
	entries, err := os.ReadDir(safeModulePath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Securely join hash directory name
			safeHashDir, err := safeJoinPath(safeModulePath, entry.Name())
			if err != nil {
				log.Debug(fmt.Sprintf("Invalid hash directory: %s", err.Error()))
				continue
			}

			// Try module-version.type filename
			jarFilename := fmt.Sprintf("%s-%s.%s", filepath.Base(module), dep.Version, dep.Type)
			safeJarFile, err := safeJoinFilename(safeHashDir, jarFilename)
			if err != nil {
				log.Debug(fmt.Sprintf("Invalid jar filename: %s", err.Error()))
				continue
			}
			if _, err := os.Stat(safeJarFile); err == nil {
				return safeJarFile
			}

			// Try module.type filename
			jarFilenameAlt := fmt.Sprintf("%s.%s", filepath.Base(module), dep.Type)
			safeJarFileAlt, err := safeJoinFilename(safeHashDir, jarFilenameAlt)
			if err != nil {
				log.Debug(fmt.Sprintf("Invalid alternative jar filename: %s", err.Error()))
				continue
			}
			if _, err := os.Stat(safeJarFileAlt); err == nil {
				return safeJarFileAlt
			}
		}
	}
	return ""
}

func (gf *GradleFlexPack) calculateFileChecksum(filePath string) (string, string, string, error) {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return "", "", "", err
	}

	if fileDetails == nil {
		return "", "", "", fmt.Errorf("fileDetails is nil for file: %s", filePath)
	}

	return fileDetails.Checksum.Sha1,
		fileDetails.Checksum.Sha256,
		fileDetails.Checksum.Md5,
		nil
}
