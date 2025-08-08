package flexpackupload

import (
	"github.com/jfrog/build-info-go/entities"
)

// FlexPackUploadManager defines the interface for package manager upload/publish operations
type FlexPackUploadManager interface {
	// GetBuildArtifacts returns a list of artifacts (packages) that were built/published
	GetBuildArtifacts() []ArtifactInfo

	// CalculateArtifactChecksums calculates checksums for all built artifacts
	CalculateArtifactChecksums() []map[string]interface{}

	// GetPublishCommand returns the command used to publish/upload packages
	GetPublishCommand() string

	// GetArtifactRepository returns the target repository URL where packages are published
	GetArtifactRepository() string

	// ValidateArtifacts checks if artifacts are ready for publication
	ValidateArtifacts() error

	// GetPublishMetadata returns additional metadata about the publish operation
	GetPublishMetadata() map[string]interface{}
}

// ArtifactInfo represents metadata about a built package artifact
type ArtifactInfo struct {
	Type        string `json:"type"`        // e.g., "wheel", "sdist", "jar", "gem"
	Name        string `json:"name"`        // artifact filename
	Path        string `json:"path"`        // local file path
	SHA1        string `json:"sha1"`        // SHA1 checksum
	SHA256      string `json:"sha256"`      // SHA256 checksum
	MD5         string `json:"md5"`         // MD5 checksum
	Size        int64  `json:"size"`        // file size in bytes
	Repository  string `json:"repository"`  // target repository URL
	PublishedAt string `json:"publishedAt"` // publication timestamp (ISO 8601)
}

// UploadConfig holds configuration for package upload operations
type UploadConfig struct {
	WorkingDirectory string            `json:"workingDirectory"`
	RepositoryURL    string            `json:"repositoryURL"`
	Username         string            `json:"username"`
	Password         string            `json:"password"`
	Token            string            `json:"token"`
	DryRun           bool              `json:"dryRun"`
	ExtraArgs        []string          `json:"extraArgs"`
	Environment      map[string]string `json:"environment"`
}

// PublishResult represents the result of a package publish operation
type PublishResult struct {
	Success    bool                   `json:"success"`
	Command    string                 `json:"command"`
	Output     string                 `json:"output"`
	Error      string                 `json:"error,omitempty"`
	Artifacts  []ArtifactInfo         `json:"artifacts"`
	Repository string                 `json:"repository"`
	Duration   string                 `json:"duration"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// BuildArtifactsToBuildInfo converts FlexPack artifacts to build-info entities format
func BuildArtifactsToBuildInfo(artifacts []ArtifactInfo, moduleName string, repositoryName string) []entities.Artifact {
	var buildInfoArtifacts []entities.Artifact

	for _, artifact := range artifacts {
		buildInfoArtifact := entities.Artifact{
			Name:                   artifact.Name,
			Type:                   artifact.Type,
			Path:                   artifact.Path,
			OriginalDeploymentRepo: repositoryName,
			Checksum: entities.Checksum{
				Sha1:   artifact.SHA1,
				Sha256: artifact.SHA256,
				Md5:    artifact.MD5,
			},
		}
		buildInfoArtifacts = append(buildInfoArtifacts, buildInfoArtifact)
	}

	return buildInfoArtifacts
}
