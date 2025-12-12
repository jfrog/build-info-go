package conan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// CollectArtifacts collects Conan artifacts from the local cache
func (cf *ConanFlexPack) CollectArtifacts() []entities.Artifact {
	var artifacts []entities.Artifact

	packageRef := cf.formatPackageRef()
	recipePath := cf.getRecipePath(packageRef)

	if recipePath != "" {
		recipeArtifacts := cf.collectRecipeArtifacts(recipePath, packageRef)
		artifacts = append(artifacts, recipeArtifacts...)
	}

	packageArtifacts := cf.collectAllPackageArtifacts(packageRef)
	artifacts = append(artifacts, packageArtifacts...)

	log.Info(fmt.Sprintf("Collected %d Conan artifacts from local cache", len(artifacts)))
	return artifacts
}

// formatPackageRef formats the package reference string
func (cf *ConanFlexPack) formatPackageRef() string {
	return fmt.Sprintf("%s/%s", cf.projectName, cf.projectVersion)
}

// getRecipePath gets the recipe path from Conan cache
func (cf *ConanFlexPack) getRecipePath(packageRef string) string {
	cmd := exec.Command(cf.config.ConanExecutable, "cache", "path", packageRef)
	cmd.Dir = cf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Could not find recipe in cache for %s: %v", packageRef, err))
		return ""
	}

	return strings.TrimSpace(string(output))
}

// collectRecipeArtifacts collects artifacts from the recipe export folder
func (cf *ConanFlexPack) collectRecipeArtifacts(recipePath, packageRef string) []entities.Artifact {
	var artifacts []entities.Artifact

	recipeFiles := []string{"conanfile.py", "conandata.yml", "conanmanifest.txt"}
	for _, filename := range recipeFiles {
		if artifact := cf.tryCreateArtifact(recipePath, filename, packageRef, "recipe"); artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}

	// Check for conan_sources.tgz in the download folder
	downloadPath := filepath.Join(filepath.Dir(recipePath), "d")
	if artifact := cf.tryCreateArtifact(downloadPath, "conan_sources.tgz", packageRef, "sources"); artifact != nil {
		artifacts = append(artifacts, *artifact)
	}

	return artifacts
}

// collectAllPackageArtifacts collects artifacts from all package binaries
func (cf *ConanFlexPack) collectAllPackageArtifacts(packageRef string) []entities.Artifact {
	var artifacts []entities.Artifact

	packageIds := cf.listPackageIds(packageRef)
	for _, pkgId := range packageIds {
		pkgPath := cf.getPackagePath(packageRef, pkgId)
		if pkgPath != "" {
			pkgArtifacts := cf.collectPackageArtifacts(pkgPath, packageRef, pkgId)
			artifacts = append(artifacts, pkgArtifacts...)
		}
	}

	return artifacts
}

// listPackageIds lists all package IDs for a recipe
func (cf *ConanFlexPack) listPackageIds(packageRef string) []string {
	cmd := exec.Command(cf.config.ConanExecutable, "list", packageRef+":*", "--format=json")
	cmd.Dir = cf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Could not list packages for %s: %v", packageRef, err))
		return nil
	}

	return cf.extractPackageIdsFromList(output)
}

// getPackagePath gets the path to a specific package binary
func (cf *ConanFlexPack) getPackagePath(packageRef, pkgId string) string {
	pkgRef := fmt.Sprintf("%s:%s", packageRef, pkgId)
	cmd := exec.Command(cf.config.ConanExecutable, "cache", "path", pkgRef)
	cmd.Dir = cf.config.WorkingDirectory

	output, err := cmd.Output()
	if err != nil {
		log.Debug(fmt.Sprintf("Could not find package in cache for %s: %v", pkgRef, err))
		return ""
	}

	return strings.TrimSpace(string(output))
}

// collectPackageArtifacts collects artifacts from the package binary folder
func (cf *ConanFlexPack) collectPackageArtifacts(pkgPath, packageRef, packageId string) []entities.Artifact {
	var artifacts []entities.Artifact
	pkgRef := packageRef + ":" + packageId

	packageFiles := []string{"conaninfo.txt", "conanmanifest.txt"}
	for _, filename := range packageFiles {
		if artifact := cf.tryCreateArtifact(pkgPath, filename, pkgRef, "package"); artifact != nil {
			artifacts = append(artifacts, *artifact)
		}
	}

	// Check for conan_package.tgz in the build folder
	buildPath := filepath.Dir(pkgPath)
	if artifact := cf.tryCreateArtifact(buildPath, "conan_package.tgz", pkgRef, "package"); artifact != nil {
		artifacts = append(artifacts, *artifact)
	}

	return artifacts
}

// tryCreateArtifact attempts to create an artifact from a file
func (cf *ConanFlexPack) tryCreateArtifact(dirPath, filename, packageRef, artifactType string) *entities.Artifact {
	filePath := filepath.Join(dirPath, filename)
	if _, err := os.Stat(filePath); err != nil {
		return nil
	}
	return cf.createArtifactFromFile(filePath, filename, packageRef, artifactType)
}

// createArtifactFromFile creates an artifact entry with checksums
func (cf *ConanFlexPack) createArtifactFromFile(filePath, filename, packageRef, artifactType string) *entities.Artifact {
	fileDetails, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to get file details for %s: %v", filePath, err))
		return nil
	}

	return &entities.Artifact{
		Name: filename,
		Path: packageRef,
		Type: fmt.Sprintf("conan-%s", artifactType),
		Checksum: entities.Checksum{
			Sha1:   fileDetails.Checksum.Sha1,
			Sha256: fileDetails.Checksum.Sha256,
			Md5:    fileDetails.Checksum.Md5,
		},
	}
}

// extractPackageIdsFromList extracts package IDs from 'conan list' JSON output
func (cf *ConanFlexPack) extractPackageIdsFromList(listOutput []byte) []string {
	var packageIds []string
	var listData map[string]interface{}

	if err := json.Unmarshal(listOutput, &listData); err != nil {
		log.Debug("Failed to parse conan list output: " + err.Error())
		return packageIds
	}

	// Navigate: {"Local Cache": {"<name>/<version>": {"revisions": {"<rrev>": {"packages": {"<pkg_id>": ...}}}}}}
	for _, cache := range listData {
		cacheMap, ok := cache.(map[string]interface{})
		if !ok {
			continue
		}

		for _, pkg := range cacheMap {
			pkgMap, ok := pkg.(map[string]interface{})
			if !ok {
				continue
			}

			revisions, ok := pkgMap["revisions"].(map[string]interface{})
			if !ok {
				continue
			}

			for _, rev := range revisions {
				revMap, ok := rev.(map[string]interface{})
				if !ok {
					continue
				}

				packages, ok := revMap["packages"].(map[string]interface{})
				if !ok {
					continue
				}

				for pkgId := range packages {
					packageIds = append(packageIds, pkgId)
				}
			}
		}
	}

	return packageIds
}

