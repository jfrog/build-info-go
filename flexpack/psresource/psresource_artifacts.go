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
	// ResolvedPackage carries the same Name/Version/Checksum a resolved dependency would.
	ResolvedPackage

	// Repo is the target Artifactory repository the package was published to.
	Repo string
}

// BuildPublishedArtifact assembles an entities.Artifact for a Publish-PSResource command from the
// already-resolved package data in published. It performs no network or filesystem I/O: checksum
// resolution (via a HEAD request to Artifactory) is the caller's responsibility.
func BuildPublishedArtifact(published PublishedArtifact) (entities.Artifact, error) {
	if err := validateNameVersion("published", published.Name, published.Version); err != nil {
		return entities.Artifact{}, err
	}

	return entities.Artifact{
		Name:                   nupkgFileName(published.Name, published.Version),
		Type:                   nupkgType,
		Path:                   DerivePublishedPath(published.Name, published.Version),
		OriginalDeploymentRepo: published.Repo,
		Checksum:               published.Checksum,
	}, nil
}

// DerivePublishedPath constructs the Artifactory path for a published PSResource package.
// NuGet convention: <name>/<version>/<Name>.<version>.nupkg
// Note: Path uses lowercase name, filename uses original case (typically PascalCase).
//
// Exported so the caller (jfrog-cli-artifactory) can compute the identical path to issue a HEAD
// request for checksums *before* constructing a PublishedArtifact - both sides must agree on
// exactly the same formula, or the HEAD request and the recorded build-info path would drift apart.
func DerivePublishedPath(name, version string) string {
	return fmt.Sprintf("%s/%s/%s", strings.ToLower(name), version, nupkgFileName(name, version))
}

// nupkgFileName returns the NuGet-convention package filename: <Name>.<version>.nupkg.
func nupkgFileName(name, version string) string {
	return fmt.Sprintf("%s.%s.nupkg", name, version)
}

// validateNameVersion checks that a PSResource package carries both a name and a version. kind
// (e.g. "resolved", "published") is folded into the error so callers can tell which side failed.
func validateNameVersion(kind, name, version string) error {
	if name == "" {
		return fmt.Errorf("%s PSResource package is missing a name", kind)
	}
	if version == "" {
		return fmt.Errorf("%s PSResource package %q is missing a version", kind, name)
	}
	return nil
}
