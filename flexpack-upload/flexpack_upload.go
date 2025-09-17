package flexpackupload

// Package flexpackupload provides interfaces and types for package manager upload operations

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
