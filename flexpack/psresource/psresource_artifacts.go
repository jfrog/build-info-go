package psresource

import (
	"fmt"
	"strings"

	"github.com/jfrog/build-info-go/entities"
)

// PublishedArtifact is a PSResource package published via Publish-PSResource whose identity and
// checksum have already been resolved by the caller (jfrog-cli-artifactory), typically via a HEAD
// request to Artifactory after the publish completed. Publish-PSResource uploads directly without
// leaving a local .nupkg behind, so - unlike Chocolatey's local pack output - there is no file here
// for build-info-go to hash itself; it only assembles the already-resolved data.
type PublishedArtifact struct {
	// Name is the PSResource package name.
	Name string

	// Version is the published package version.
	Version string

	// Repo is the target Artifactory repository the package was published to.
	Repo string

	// Checksum is the package checksum, already fetched by the caller from Artifactory.
	Checksum entities.Checksum
}

// BuildPublishedArtifact assembles an entities.Artifact for a Publish-PSResource command from the
// already-resolved package data in published. It performs no network or filesystem I/O: checksum
// resolution (via a HEAD request to Artifactory) is the caller's responsibility.
func BuildPublishedArtifact(published PublishedArtifact) (entities.Artifact, error) {
	if published.Name == "" {
		return entities.Artifact{}, fmt.Errorf("published PSResource package is missing a name")
	}
	if published.Version == "" {
		return entities.Artifact{}, fmt.Errorf("published PSResource package %q is missing a version", published.Name)
	}

	fileName := fmt.Sprintf("%s.%s.nupkg", published.Name, published.Version)
	return entities.Artifact{
		Name:                   fileName,
		Type:                   nupkgType,
		Path:                   derivePublishedPath(published.Name, published.Version),
		OriginalDeploymentRepo: published.Repo,
		Checksum:               published.Checksum,
	}, nil
}

// derivePublishedPath constructs the Artifactory path for a published PSResource package.
// NuGet convention: <name>/<version>/<Name>.<version>.nupkg
// Note: Path uses lowercase name, filename uses original case (typically PascalCase).
func derivePublishedPath(name, version string) string {
	return fmt.Sprintf("%s/%s/%s.%s.nupkg", strings.ToLower(name), version, name, version)
}
