package flexpack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

// SanitizePath cleans a path and converts it to an absolute path.
func SanitizePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	return filepath.Abs(filepath.Clean(path))
}

// isPathContainedIn checks if childPath is contained within parentPath (or equals it).
func isPathContainedIn(childPath, parentPath string) bool {
	return strings.HasPrefix(childPath+string(filepath.Separator), parentPath+string(filepath.Separator)) || childPath == parentPath
}

// SanitizeAndValidatePath sanitizes a path and validates it stays within the provided base directory.
func SanitizeAndValidatePath(path, baseDir string) (string, error) {
	sanitizedPath, err := SanitizePath(path)
	if err != nil {
		return "", err
	}
	sanitizedBase, err := SanitizePath(baseDir)
	if err != nil {
		return "", err
	}
	if !isPathContainedIn(sanitizedPath, sanitizedBase) {
		return "", fmt.Errorf("path %s escapes base directory %s", sanitizedPath, sanitizedBase)
	}
	return sanitizedPath, nil
}

// IsEscaped reports whether the byte at index is escaped by an odd number of backslashes.
func IsEscaped(content string, index int) bool {
	backslashes := 0
	for j := index - 1; j >= 0; j-- {
		if content[j] == '\\' {
			backslashes++
		} else {
			break
		}
	}
	return backslashes%2 != 0
}

// IsDelimiter reports Gradle block delimiters and whitespace.
func IsDelimiter(b byte) bool {
	switch b {
	case '{', '}', '(', ')', ';', ',':
		return true
	}
	return IsWhitespace(b)
}

// IsWhitespace reports ASCII whitespace used by Gradle parsing helpers.
func IsWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

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

// GetGradleUserHome returns the Gradle user home directory
func GetGradleUserHome() string {
	if envHome := os.Getenv("GRADLE_USER_HOME"); envHome != "" {
		if abs, err := filepath.Abs(filepath.Clean(envHome)); err == nil {
			return abs
		}
		return filepath.Clean(envHome)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	defaultHome := filepath.Join(homeDir, ".gradle")
	if abs, err := filepath.Abs(filepath.Clean(defaultHome)); err == nil {
		return abs
	}
	return filepath.Clean(defaultHome)
}

func (gf *GradleFlexPack) runGradleCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(gf.ctx, gf.config.CommandTimeout)
	defer cancel()

	// Add non-interactive flags; only disable the daemon in CI or when explicitly requested.
	base := []string{"--console=plain", "--warning-mode=none"}
	noDaemon := strings.EqualFold(os.Getenv("CI"), "true") || strings.EqualFold(os.Getenv("JFROG_GRADLE_NO_DAEMON"), "true")
	if noDaemon {
		base = append([]string{"--no-daemon"}, base...)
	}
	base = append(base, args...)
	fullArgs := base

	cmd := exec.CommandContext(ctx, gf.config.GradleExecutable, fullArgs...)
	cmd.Dir = gf.config.WorkingDirectory
	// Ensure no input is expected
	// cmd.Stdin = nil

	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("gradle command timed out after %v: %s", gf.config.CommandTimeout, strings.Join(fullArgs, " "))
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
	// Default to jar if missing.
	depType := strings.TrimSpace(dep.Type)
	if depType == "" {
		depType = "jar"
	}

	parts := strings.Split(dep.Name, ":")
	if len(parts) != 2 {
		return ""
	}

	group := parts[0]
	module := parts[1]
	classifier := ""
	// dep.ID is typically group:module:version[:classifier]
	if idParts := strings.Split(dep.ID, ":"); len(idParts) >= 4 {
		classifier = idParts[3]
	}

	// Get a validated, sanitized cache base path
	cacheBase, err := getGradleCacheBase()
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get Gradle cache base: %s", err.Error()))
		return ""
	}

	// Build path: ~/.gradle/caches/modules-2/files-2.1/{group}/{module}/{version}
	modulePath := filepath.Join(cacheBase, group, module, dep.Version)
	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		log.Debug(fmt.Sprintf("Gradle cache path not found for dependency %s: %s", dep.ID, modulePath))
		return ""
	}

	entries, err := os.ReadDir(modulePath)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to read Gradle cache directory for dependency %s: %s", dep.ID, err.Error()))
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			hashDir := filepath.Join(modulePath, entry.Name())
			var expected string
			if classifier != "" {
				expected = fmt.Sprintf("%s-%s-%s.%s", module, dep.Version, classifier, depType)
			} else {
				expected = fmt.Sprintf("%s-%s.%s", module, dep.Version, depType)
			}
			if resolved := findFileCaseInsensitive(hashDir, expected); resolved != "" {
				return resolved
			}

			if classifier == "" {
				if resolved := findArtifactFromGradleModuleMetadata(hashDir, module, dep.Version, depType); resolved != "" {
					return resolved
				}
				if resolved := findUniqueVariantArtifact(hashDir, module, dep.Version, depType); resolved != "" {
					return resolved
				}
			}
		}
	}
	return ""
}

func findFileCaseInsensitive(dir, expectedName string) string {
	expectedName = strings.TrimSpace(expectedName)
	if expectedName == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	expectedLower := strings.ToLower(expectedName)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		nameLower := strings.ToLower(e.Name())
		// Ignore sidecars/metadata even if someone asks for them by mistake.
		if strings.HasSuffix(nameLower, ".sha1") || strings.HasSuffix(nameLower, ".sha256") || strings.HasSuffix(nameLower, ".md5") {
			continue
		}
		if strings.HasSuffix(nameLower, ".module") || strings.HasSuffix(nameLower, ".json") {
			continue
		}
		if nameLower == expectedLower {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func findUniqueVariantArtifact(hashDir, module, version, ext string) string {
	module = strings.ToLower(strings.TrimSpace(module))
	version = strings.ToLower(strings.TrimSpace(version))
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		ext = "jar"
	}
	if module == "" || version == "" {
		return ""
	}

	entries, err := os.ReadDir(hashDir)
	if err != nil {
		return ""
	}

	prefix := module + "-" + version + "-"
	suffix := "." + ext

	var found string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		nameLower := strings.ToLower(e.Name())
		// Ignore sidecars/metadata
		if strings.HasSuffix(nameLower, ".sha1") || strings.HasSuffix(nameLower, ".sha256") || strings.HasSuffix(nameLower, ".md5") {
			continue
		}
		if strings.HasSuffix(nameLower, ".module") || strings.HasSuffix(nameLower, ".json") || strings.HasSuffix(nameLower, ".pom") {
			continue
		}
		if !strings.HasSuffix(nameLower, suffix) || !strings.HasPrefix(nameLower, prefix) {
			continue
		}

		// Exclude common non-runtime jars.
		if strings.Contains(nameLower, "-sources.") || strings.Contains(nameLower, "-javadoc.") || strings.Contains(nameLower, "-tests.") {
			continue
		}

		// Candidate found.
		candidate := filepath.Join(hashDir, e.Name())
		if found != "" && found != candidate {
			// Ambiguous: more than one candidate.
			return ""
		}
		found = candidate
	}
	return found
}

type gradleModuleMetadata struct {
	Variants []struct {
		Name  string `json:"name"`
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	} `json:"variants"`
}

func findArtifactFromGradleModuleMetadata(hashDir, module, version, ext string) string {
	module = strings.TrimSpace(module)
	version = strings.TrimSpace(version)
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		ext = "jar"
	}
	if module == "" || version == "" {
		return ""
	}

	entries, err := os.ReadDir(hashDir)
	if err != nil {
		return ""
	}

	// Prefer the expected "<module>-<version>.module" filename, but fall back to any ".module" file.
	expectedModuleFile := strings.ToLower(fmt.Sprintf("%s-%s.module", module, version))
	var moduleMetaPath string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		nameLower := strings.ToLower(e.Name())
		if strings.HasSuffix(nameLower, ".module") && nameLower == expectedModuleFile {
			moduleMetaPath = filepath.Join(hashDir, e.Name())
			break
		}
	}
	if moduleMetaPath == "" {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			nameLower := strings.ToLower(e.Name())
			if strings.HasSuffix(nameLower, ".module") {
				moduleMetaPath = filepath.Join(hashDir, e.Name())
				break
			}
		}
	}
	if moduleMetaPath == "" {
		return ""
	}

	content, err := os.ReadFile(moduleMetaPath)
	if err != nil || len(content) == 0 {
		return ""
	}

	var meta gradleModuleMetadata
	if err := json.Unmarshal(content, &meta); err != nil {
		return ""
	}

	// We prefer runtime variants, since build-info dependencies represent runtime-resolved artifacts.
	preferredVariantNameContains := []string{"runtime"}

	// Try preferred variants first; if none exist, fall back to all variants.
	tryVariants := func(filter bool) string {
		var chosen string
		for _, v := range meta.Variants {
			nameLower := strings.ToLower(v.Name)
			if filter {
				ok := false
				for _, needle := range preferredVariantNameContains {
					if strings.Contains(nameLower, needle) {
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
			}
			for _, f := range v.Files {
				fileName := strings.TrimSpace(f.Name)
				if fileName == "" {
					continue
				}
				if !strings.HasSuffix(strings.ToLower(fileName), "."+ext) {
					continue
				}
				// Keep this strict: the file must exist in the cache hash dir.
				p := filepath.Join(hashDir, fileName)
				if _, err := os.Stat(p); err != nil {
					continue
				}
				// If multiple different candidates exist across variants/files, treat as ambiguous.
				if chosen != "" && chosen != p {
					return ""
				}
				chosen = p
			}
		}
		return chosen
	}

	if resolved := tryVariants(true); resolved != "" {
		return resolved
	}
	return tryVariants(false)
}
