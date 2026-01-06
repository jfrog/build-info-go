package flexpack

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

//go:embed init-artifact-extractor.gradle
var initScriptContent string

type deployedArtifactJSON struct {
	ModuleName string `json:"module_name"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Sha1       string `json:"sha1"`
	Sha256     string `json:"sha256"`
	Md5        string `json:"md5"`
}

func (gf *GradleFlexPack) getGradleDeployedArtifacts() (map[string][]entities.Artifact, error) {
	initScriptFile, err := os.CreateTemp("", "init-artifact-extractor-*.gradle")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare Gradle artifact collection: %w", err)
	}
	initScriptPath := initScriptFile.Name()
	// Use a single defer to ensure proper cleanup order: Close before Remove
	defer func() {
		if closeErr := initScriptFile.Close(); closeErr != nil {
			log.Debug(fmt.Sprintf("Failed to close temporary artifacts generation script: %s", closeErr.Error()))
		}
		if removeErr := os.Remove(initScriptPath); removeErr != nil {
			log.Warn("failed to remove temporary artifacts generation script")
		}
	}()

	if _, err := initScriptFile.WriteString(initScriptContent); err != nil {
		return nil, fmt.Errorf("failed to prepare Gradle artifact collection: %w", err)
	}

	// Generate a unique manifest filename to prevent race conditions with concurrent builds
	manifestFileName := fmt.Sprintf("ci-artifacts-manifest-%d.json", time.Now().UnixNano())
	tasks := []string{
		"flexpackPublishToLocal",
		"generateCiManifest",
		"-I", initScriptPath,
		fmt.Sprintf("-PciManifest.fileName=%s", manifestFileName),
	}
	if output, err := gf.runGradleCommand(tasks...); err != nil {
		return nil, fmt.Errorf("failed to collect Gradle artifacts: %w (output: %s)", err, string(output))
	}

	manifestPath, err := resolveManifestPath(gf.config.WorkingDirectory, manifestFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Gradle artifacts: %w", err)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Gradle artifacts: cannot read generated artifacts: %w", err)
	}
	if err := os.Remove(manifestPath); err != nil {
		log.Warn("failed to remove generated artifacts file")
	}

	var artifacts []deployedArtifactJSON
	if err := json.Unmarshal(content, &artifacts); err != nil {
		return nil, fmt.Errorf("failed to collect Gradle artifacts: cannot parse generated artifacts: %w", err)
	}

	result := make(map[string][]entities.Artifact)
	for _, art := range artifacts {
		// Skip artifacts without checksums
		if art.Sha1 == "" && art.Sha256 == "" && art.Md5 == "" {
			log.Warn(fmt.Sprintf("Skipping artifact %s: no checksums available", art.Name))
			continue
		}

		moduleName := strings.TrimPrefix(art.ModuleName, ":")
		// Handle root project if it returns just "::"
		if moduleName == ":" {
			moduleName = ""
		}

		entityArtifact := entities.Artifact{
			Name: art.Name,
			Type: art.Type,
			Path: art.Path,
			Checksum: entities.Checksum{
				Sha1:   art.Sha1,
				Sha256: art.Sha256,
				Md5:    art.Md5,
			},
		}
		result[moduleName] = append(result[moduleName], entityArtifact)
	}
	return result, nil
}

func resolveManifestPath(workingDir, manifestFileName string) (string, error) {
	// First check the expected location (root build directory)
	defaultPath := filepath.Join(workingDir, "build", manifestFileName)
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	}

	const maxDepth = 5
	startDepth := strings.Count(workingDir, string(os.PathSeparator))
	var found string

	skipDirs := map[string]bool{
		".git":         true,
		".gradle":      true,
		".idea":        true,
		"node_modules": true,
		"buildSrc":     true,
	}

	err := filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		depth := strings.Count(path, string(os.PathSeparator)) - startDepth
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		// Match the specific manifest filename we're looking for
		if !d.IsDir() && d.Name() == manifestFileName {
			found = path
			return io.EOF
		}
		return nil
	})

	if err != nil && err != io.EOF {
		return "", err
	}
	if found != "" {
		return found, nil
	}

	return "", fmt.Errorf("generated manifest %s not found under %s ", manifestFileName, workingDir)
}

// if a module is a dependency, the checksum calculation depends if the artifact is published or not
func (gf *GradleFlexPack) calculateChecksumWithFallback(dep flexpack.DependencyInfo) map[string]interface{} {
	checksumMap := map[string]interface{}{
		"id":      dep.ID,
		"name":    dep.Name,
		"version": dep.Version,
		"type":    dep.Type,
		"scopes":  gf.validateAndNormalizeScopes(dep.Scopes),
	}

	// 1. Try to find in deployed artifacts (local build)
	// Example:
	//   gf.deployedArtifacts = {
	//     "submodule-a": [{Name: "my-library-1.0.0.jar", Sha1: "abc123", ...}],
	//     "submodule-b": [{Name: "other-lib-2.0.0.jar", ...}],
	//   }
	//   We iterate all modules' artifacts to find one matching expectedName.
	if len(gf.deployedArtifacts) > 0 {
		parts := strings.Split(dep.Name, ":")
		if len(parts) == 2 {
			artifactId := parts[1]
			depType := dep.Type
			if depType == "" {
				depType = "jar"
			}

			// Check if dependency has a classifier (dep.ID format: group:artifact:version[:classifier])
			var expectedName string
			idParts := strings.Split(dep.ID, ":")
			if len(idParts) >= 4 && idParts[3] != "" {
				// Has classifier: artifact-version-classifier.type
				expectedName = fmt.Sprintf("%s-%s-%s.%s", artifactId, dep.Version, idParts[3], depType)
			} else {
				// No classifier: artifact-version.type
				expectedName = fmt.Sprintf("%s-%s.%s", artifactId, dep.Version, depType)
			}

			for _, artifacts := range gf.deployedArtifacts {
				for _, art := range artifacts {
					if art.Name == expectedName {
						checksumMap["sha1"] = art.Sha1
						checksumMap["sha256"] = art.Sha256
						checksumMap["md5"] = art.Md5
						checksumMap["path"] = art.Path
						return checksumMap
					}
				}
			}
		}
	}

	// 2. Fallback to Gradle cache
	if artifactPath := gf.findGradleArtifact(dep); artifactPath != "" {
		if fileDetails, err := crypto.GetFileDetails(artifactPath, true); err == nil && fileDetails != nil {
			checksumMap["sha1"] = fileDetails.Checksum.Sha1
			checksumMap["sha256"] = fileDetails.Checksum.Sha256
			checksumMap["md5"] = fileDetails.Checksum.Md5
			checksumMap["path"] = artifactPath
			return checksumMap
		}
		log.Debug(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
	}
	return checksumMap
}
