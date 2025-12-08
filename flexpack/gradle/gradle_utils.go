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
	if !strings.HasPrefix(absResolvedPath, expectedPrefix) {
		return false
	}
	return true
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
	return nil, "", fmt.Errorf("neither build.gradle nor build.gradle.kts found")
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

func (gf *GradleFlexPack) findGradleArtifact(dep flexpack.DependencyInfo) string {
	parts := strings.Split(dep.Name, ":")
	if len(parts) != 2 {
		return ""
	}

	group := parts[0]
	module := parts[1]

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Debug("Failed to get user home directory: " + err.Error())
		return ""
	}
	gradleUserHome := os.Getenv("GRADLE_USER_HOME")
	if gradleUserHome == "" {
		gradleUserHome = filepath.Join(homeDir, ".gradle")
	}

	// Gradle cache structure: ~/.gradle/caches/modules-2/files-2.1/group/module/version/hash/filename
	cacheBase := filepath.Join(gradleUserHome, "caches", "modules-2", "files-2.1")
	modulePath := filepath.Join(cacheBase, group, module, dep.Version)

	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		return ""
	}
	entries, err := os.ReadDir(modulePath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			hashDir := filepath.Join(modulePath, entry.Name())
			//module-version.type
			jarFile := filepath.Join(hashDir, fmt.Sprintf("%s-%s.%s", module, dep.Version, dep.Type))
			if _, err := os.Stat(jarFile); err == nil {
				return jarFile
			}
			// module.type
			jarFileAlt := filepath.Join(hashDir, fmt.Sprintf("%s.%s", module, dep.Type))
			if _, err := os.Stat(jarFileAlt); err == nil {
				return jarFileAlt
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
