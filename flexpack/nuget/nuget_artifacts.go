package nuget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
)

// FindNupkgArtifacts scans outputDir for .nupkg files and returns entities.Artifact
// structs with checksums computed from the local files.
func FindNupkgArtifacts(outputDir, repoName string) ([]entities.Artifact, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("scan nupkg output dir %s: %w", outputDir, err)
	}
	var artifacts []entities.Artifact
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(name[max(0, len(name)-6):], ".nupkg") {
			continue
		}
		fullPath := filepath.Join(outputDir, name)
		details, err := crypto.GetFileDetails(fullPath, true)
		if err != nil {
			return nil, fmt.Errorf("compute checksum for %s: %w", name, err)
		}
		pkgID, version := parseNupkgFilename(name)
		path := pkgID + "/" + version + "/" + name
		if pkgID == "" {
			path = name
		}
		artifacts = append(artifacts, entities.Artifact{
			Name:                   name,
			Type:                   "nupkg",
			Path:                   path,
			OriginalDeploymentRepo: repoName,
			Checksum: entities.Checksum{
				Sha1:   details.Checksum.Sha1,
				Sha256: details.Checksum.Sha256,
				Md5:    details.Checksum.Md5,
			},
		})
	}
	return artifacts, nil
}

// parseNupkgFilename extracts PackageId and Version from "<id>.<version>.nupkg".
// NuGet convention: first numeric segment (+ following parts) is the version.
func parseNupkgFilename(filename string) (pkgID, version string) {
	base := strings.TrimSuffix(filename, ".nupkg")
	parts := strings.Split(base, ".")
	for i, p := range parts {
		if len(p) > 0 && p[0] >= '0' && p[0] <= '9' {
			return strings.Join(parts[:i], "."), strings.Join(parts[i:], ".")
		}
	}
	return base, ""
}
