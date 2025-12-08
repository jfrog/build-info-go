package flexpack

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
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
		return nil, fmt.Errorf("failed to create init script: %w", err)
	}
	initScriptPath := initScriptFile.Name()
	defer initScriptFile.Close()
	defer func() {
		if err := os.Remove(initScriptPath); err != nil {
			log.Debug("Failed to remove init script: " + err.Error())
		}
	}()

	if _, err := initScriptFile.WriteString(initScriptContent); err != nil {
		return nil, fmt.Errorf("failed to write init script: %w", err)
	}

	tasks := []string{"publishToMavenLocal", "generateCiManifest", "-I", initScriptPath}
	if output, err := gf.runGradleCommand(tasks...); err != nil {
		return nil, fmt.Errorf("gradle command failed: %s - %w", string(output), err)
	}

	manifestPath := filepath.Join(gf.config.WorkingDirectory, "build", "ci-artifacts-manifest.json")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	if err := os.Remove(manifestPath); err != nil {
		log.Warn("Failed to delete manifest file: " + err.Error())
	}

	var artifacts []deployedArtifactJSON
	if err := json.Unmarshal(content, &artifacts); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	result := make(map[string][]entities.Artifact)
	for _, art := range artifacts {
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
	if len(gf.deployedArtifacts) > 0 {
		parts := strings.Split(dep.Name, ":")
		if len(parts) == 2 {
			artifactId := parts[1]
			expectedName := fmt.Sprintf("%s-%s.%s", artifactId, dep.Version, dep.Type)
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
		if sha1, sha256, md5, err := gf.calculateFileChecksum(artifactPath); err == nil {
			checksumMap["sha1"] = sha1
			checksumMap["sha256"] = sha256
			checksumMap["md5"] = md5
			checksumMap["path"] = artifactPath
			return checksumMap
		}
		log.Debug(fmt.Sprintf("Failed to calculate checksum for artifact: %s", artifactPath))
	}
	return nil
}
