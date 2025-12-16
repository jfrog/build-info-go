package flexpack

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

func GetGradleExecutablePath(workingDirectory string) (string, error) {
	// Check for OS-appropriate wrapper first
	if runtime.GOOS == "windows" {
		if wrapperPath := filepath.Join(workingDirectory, "gradlew.bat"); fileExists(wrapperPath) {
			return filepath.Abs(wrapperPath)
		}
	} else {
		if wrapperPath := filepath.Join(workingDirectory, "gradlew"); fileExists(wrapperPath) {
			return filepath.Abs(wrapperPath)
		}
	}

	// Fallback to system Gradle
	gradleExec, err := exec.LookPath("gradle")
	if err != nil {
		return "", fmt.Errorf("gradle executable not found: neither wrapper in %s nor system gradle in PATH", workingDirectory)
	}
	return gradleExec, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

// isSubPath checks if child path is within parent directory.
func isSubPath(parent, child string) bool {
	parentAbs, err := filepath.Abs(filepath.Clean(parent))
	if err != nil {
		return false
	}
	childAbs, err := filepath.Abs(filepath.Clean(child))
	if err != nil {
		return false
	}
	return strings.HasPrefix(childAbs, parentAbs+string(filepath.Separator)) || childAbs == parentAbs
}

func FindGradleFile(dir, baseName string) (path string, isKts bool, err error) {
	// Sanitize the directory path
	sanitizedDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", false, fmt.Errorf("invalid directory path: %w", err)
	}

	// Validate baseName doesn't contain path separators (prevent traversal via filename)
	if strings.ContainsAny(baseName, `/\`) {
		return "", false, fmt.Errorf("invalid base name: %s", baseName)
	}

	// Check for Groovy DSL file
	groovyPath := filepath.Join(sanitizedDir, baseName+".gradle")
	absGroovyPath, err := filepath.Abs(filepath.Clean(groovyPath))
	if err != nil {
		return "", false, fmt.Errorf("invalid gradle file path: %w", err)
	}
	// Validate path is within directory
	if !strings.HasPrefix(absGroovyPath, sanitizedDir+string(filepath.Separator)) && absGroovyPath != sanitizedDir {
		return "", false, fmt.Errorf("path traversal detected")
	}
	if _, err := os.Stat(absGroovyPath); err == nil {
		return absGroovyPath, false, nil
	}

	// Check for Kotlin DSL file
	ktsPath := filepath.Join(sanitizedDir, baseName+".gradle.kts")
	absKtsPath, err := filepath.Abs(filepath.Clean(ktsPath))
	if err != nil {
		return "", false, fmt.Errorf("invalid gradle file path: %w", err)
	}
	// Validate path is within directory
	if !strings.HasPrefix(absKtsPath, sanitizedDir+string(filepath.Separator)) && absKtsPath != sanitizedDir {
		return "", false, fmt.Errorf("path traversal detected")
	}
	if _, err := os.Stat(absKtsPath); err == nil {
		return absKtsPath, true, nil
	}

	return "", false, fmt.Errorf("no %s.gradle or %s.gradle.kts found in %s", baseName, baseName, dir)
}

// getBuildFileContent reads the build.gradle or build.gradle.kts file for a module.
func (gf *GradleFlexPack) getBuildFileContent(moduleName string) ([]byte, string, error) {
	subPath := ""
	if moduleName != "" {
		// moduleName is "a:b" -> "a/b"
		subPath = strings.ReplaceAll(moduleName, ":", string(filepath.Separator))
	}

	moduleDir := filepath.Join(gf.config.WorkingDirectory, subPath)
	if !isSubPath(gf.config.WorkingDirectory, moduleDir) {
		return nil, "", fmt.Errorf("path traversal attempt detected for module %s", moduleName)
	}

	path, _, err := FindGradleFile(moduleDir, "build")
	if err != nil {
		return nil, "", fmt.Errorf("%w: neither build.gradle nor build.gradle.kts found", os.ErrNotExist)
	}

	content, err := os.ReadFile(path)
	return content, path, err
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
	} else if strings.Contains(gradleUserHome, "..") {
		// Validate the environment variable value - check for path traversal patterns
		return "", fmt.Errorf("path traversal pattern detected in GRADLE_USER_HOME: %s", gradleUserHome)
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

	// Build path: ~/.gradle/caches/modules-2/files-2.1/{group}/{module}/{version}
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

			// Try module-version.type filename (e.g., commons-io-2.11.0.jar)
			jarFile := filepath.Join(hashDir, fmt.Sprintf("%s-%s.%s", module, dep.Version, dep.Type))
			if _, err := os.Stat(jarFile); err == nil {
				return jarFile
			}

			// Try module.type filename (e.g., commons-io.jar)
			jarFileAlt := filepath.Join(hashDir, fmt.Sprintf("%s.%s", module, dep.Type))
			if _, err := os.Stat(jarFileAlt); err == nil {
				return jarFileAlt
			}
		}
	}
	return ""
}
