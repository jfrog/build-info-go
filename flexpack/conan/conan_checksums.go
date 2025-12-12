package conan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// calculateChecksumWithFallback calculates checksums for a dependency
func (cf *ConanFlexPack) calculateChecksumWithFallback(dep DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  cf.validateAndNormalizeScopes(dep.Scopes),
	}

	artifactPath := cf.findConanArtifact(dep)
	if artifactPath == "" {
		cf.logChecksumFailure(dep)
		return nil
	}

	sha1, sha256, md5, err := cf.calculateFileChecksum(artifactPath)
	if err != nil {
		log.Warn(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
		return nil
	}

	checksumMap["sha1"] = sha1
	checksumMap["sha256"] = sha256
	checksumMap["md5"] = md5
	checksumMap["path"] = artifactPath

	return checksumMap
}

// logChecksumFailure logs appropriate message for checksum calculation failure
func (cf *ConanFlexPack) logChecksumFailure(dep DependencyInfo) {
	if cf.isBuildDependency(dep) {
		log.Debug(fmt.Sprintf("Skipping checksum calculation for build dependency: %s:%s", dep.Name, dep.Version))
	} else {
		log.Warn(fmt.Sprintf("Failed to calculate checksums for dependency: %s:%s", dep.Name, dep.Version))
	}
}

// isBuildDependency checks if a dependency is a build dependency
func (cf *ConanFlexPack) isBuildDependency(dep DependencyInfo) bool {
	for _, scope := range dep.Scopes {
		if strings.ToLower(scope) == "build" {
			return true
		}
	}
	return false
}

// findConanArtifact locates a Conan artifact in the cache
func (cf *ConanFlexPack) findConanArtifact(dep DependencyInfo) string {
	ref := fmt.Sprintf("%s/%s", dep.Name, dep.Version)
	cmd := exec.Command(cf.config.ConanExecutable, "cache", "path", ref)
	cmd.Dir = cf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get cache path for %s: %v", dep.ID, err))
		return ""
	}

	packagePath := strings.TrimSpace(string(output))
	if _, err := os.Stat(packagePath); err == nil {
		return cf.findPackageFile(packagePath)
	}

	return ""
}

// findPackageFile finds a checksummable file in the package directory
func (cf *ConanFlexPack) findPackageFile(packagePath string) string {
	// Try archive files first
	patterns := []string{"*.tgz", "*.tar.gz", "*.zip"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(packagePath, pattern))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}

	// Try manifest file
	manifestPath := filepath.Join(packagePath, "conanmanifest.txt")
	if _, err := os.Stat(manifestPath); err == nil {
		return manifestPath
	}

	// Try conanfile
	conanfilePath := filepath.Join(packagePath, "conanfile.py")
	if _, err := os.Stat(conanfilePath); err == nil {
		return conanfilePath
	}

	return ""
}

// calculateFileChecksum calculates checksums for a file
func (cf *ConanFlexPack) calculateFileChecksum(filePath string) (sha1, sha256, md5 string, err error) {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		return "", "", "", err
	}

	if fileDetails == nil {
		return "", "", "", fmt.Errorf("fileDetails is nil for file: %s", filePath)
	}

	return fileDetails.Checksum.Sha1, fileDetails.Checksum.Sha256, fileDetails.Checksum.Md5, nil
}

// validateAndNormalizeScopes ensures scopes are valid and normalized
func (cf *ConanFlexPack) validateAndNormalizeScopes(scopes []string) []string {
	validScopes := map[string]bool{
		"runtime": true,
		"build":   true,
		"test":    true,
		"python":  true,
	}

	var normalized []string
	for _, scope := range scopes {
		if validScopes[scope] {
			normalized = append(normalized, scope)
		}
	}

	if len(normalized) == 0 {
		return []string{"runtime"} // Default Conan scope
	}

	return normalized
}

