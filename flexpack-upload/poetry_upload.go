package flexpackupload

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// PoetryUploadManager implements basic Poetry upload functionality
type PoetryUploadManager struct {
	config      UploadConfig
	projectInfo *PoetryProjectInfo
	artifacts   []ArtifactInfo
	buildDir    string
}

// PoetryProjectInfo represents Poetry project metadata from pyproject.toml
type PoetryProjectInfo struct {
	Tool struct {
		Poetry struct {
			Name        string   `toml:"name"`
			Version     string   `toml:"version"`
			Description string   `toml:"description"`
			Authors     []string `toml:"authors"`
			Repository  string   `toml:"repository"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// NewPoetryUploadManager creates a new Poetry upload manager
func NewPoetryUploadManager(config UploadConfig) (*PoetryUploadManager, error) {
	manager := &PoetryUploadManager{
		config:   config,
		buildDir: filepath.Join(config.WorkingDirectory, "dist"),
	}

	// Load project information from pyproject.toml
	if err := manager.loadProjectInfo(); err != nil {
		return nil, fmt.Errorf("failed to load Poetry project info: %w", err)
	}

	return manager, nil
}

// loadProjectInfo loads Poetry project metadata from pyproject.toml
func (p *PoetryUploadManager) loadProjectInfo() error {
	pyprojectPath := filepath.Join(p.config.WorkingDirectory, "pyproject.toml")

	if _, err := os.Stat(pyprojectPath); os.IsNotExist(err) {
		return fmt.Errorf("pyproject.toml not found in %s", p.config.WorkingDirectory)
	}

	content, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return fmt.Errorf("failed to read pyproject.toml: %w", err)
	}

	p.projectInfo = &PoetryProjectInfo{}
	if err := toml.Unmarshal(content, p.projectInfo); err != nil {
		return fmt.Errorf("failed to parse pyproject.toml: %w", err)
	}

	log.Debug(fmt.Sprintf("Loaded Poetry project: %s v%s", p.projectInfo.Tool.Poetry.Name, p.projectInfo.Tool.Poetry.Version))
	return nil
}

// GetBuildArtifacts returns a list of artifacts that were built by Poetry
func (p *PoetryUploadManager) GetBuildArtifacts() []ArtifactInfo {
	if len(p.artifacts) == 0 {
		p.scanBuildArtifacts()
	}
	return p.artifacts
}

// scanBuildArtifacts scans the dist/ directory for built artifacts
func (p *PoetryUploadManager) scanBuildArtifacts() {
	p.artifacts = []ArtifactInfo{}

	if _, err := os.Stat(p.buildDir); os.IsNotExist(err) {
		log.Debug("Build directory does not exist: " + p.buildDir)
		return
	}

	files, err := filepath.Glob(filepath.Join(p.buildDir, "*"))
	if err != nil {
		log.Debug("Failed to scan build directory: " + err.Error())
		return
	}

	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			artifact := p.createArtifactInfo(file, info)
			if artifact != nil {
				p.artifacts = append(p.artifacts, *artifact)
			}
		}
	}

	log.Debug(fmt.Sprintf("Found %d build artifacts", len(p.artifacts)))
}

// createArtifactInfo creates an ArtifactInfo from a file path
func (p *PoetryUploadManager) createArtifactInfo(filePath string, fileInfo os.FileInfo) *ArtifactInfo {
	filename := filepath.Base(filePath)

	// Determine artifact type based on file extension
	var artifactType string
	switch {
	case strings.HasSuffix(filename, ".whl"):
		artifactType = "wheel"
	case strings.HasSuffix(filename, ".tar.gz"):
		artifactType = "sdist"
	default:
		log.Debug("Skipping unknown artifact type: " + filename)
		return nil
	}

	// Calculate checksums
	checksums, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		log.Debug(fmt.Sprintf("Failed to calculate checksums for %s: %v", filename, err))
		return nil
	}

	artifact := &ArtifactInfo{
		Type:       artifactType,
		Name:       filename,
		Path:       filePath,
		SHA1:       checksums.Checksum.Sha1,
		SHA256:     checksums.Checksum.Sha256,
		MD5:        checksums.Checksum.Md5,
		Size:       fileInfo.Size(),
		Repository: p.config.RepositoryURL,
	}

	log.Debug(fmt.Sprintf("Created artifact info for %s (%s)", filename, artifactType))
	return artifact
}

// BuildArtifacts builds Poetry packages using 'poetry build'
func (p *PoetryUploadManager) BuildArtifacts() (*PublishResult, error) {
	log.Debug("Building Poetry artifacts in " + p.config.WorkingDirectory)

	cmd := exec.Command("poetry", "build")
	cmd.Dir = p.config.WorkingDirectory

	// Set environment variables
	cmd.Env = os.Environ()
	for key, value := range p.config.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	result := &PublishResult{
		Success:    err == nil,
		Command:    "poetry build",
		Output:     string(output),
		Repository: p.GetArtifactRepository(),
		Duration:   duration.String(),
		Metadata:   p.GetPublishMetadata(),
	}

	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("poetry build failed: %w", err)
	}

	// Refresh artifacts after build
	p.scanBuildArtifacts()
	result.Artifacts = p.artifacts

	log.Debug(fmt.Sprintf("Poetry build completed successfully in %s", duration))
	return result, nil
}

// PublishArtifacts publishes Poetry packages using 'poetry publish'
func (p *PoetryUploadManager) PublishArtifacts() (*PublishResult, error) {
	log.Debug("Publishing Poetry artifacts to " + p.GetArtifactRepository())

	args := []string{"publish"}

	// Add authentication if provided
	if p.config.Username != "" && p.config.Password != "" {
		args = append(args, "-u", p.config.Username, "-p", p.config.Password)
	} else if p.config.Token != "" {
		args = append(args, "-u", "__token__", "-p", p.config.Token)
	}

	// Add dry-run flag
	if p.config.DryRun {
		args = append(args, "--dry-run")
	}

	// Add extra arguments
	args = append(args, p.config.ExtraArgs...)

	cmd := exec.Command("poetry", args...)
	cmd.Dir = p.config.WorkingDirectory

	// Set environment variables
	cmd.Env = os.Environ()
	for key, value := range p.config.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	result := &PublishResult{
		Success:    err == nil,
		Command:    p.GetPublishCommand(),
		Output:     string(output),
		Artifacts:  p.GetBuildArtifacts(),
		Repository: p.GetArtifactRepository(),
		Duration:   duration.String(),
		Metadata:   p.GetPublishMetadata(),
	}

	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("poetry publish failed: %w", err)
	}

	// Update publish timestamp on artifacts
	publishTime := time.Now().Format(time.RFC3339)
	for i := range result.Artifacts {
		result.Artifacts[i].PublishedAt = publishTime
	}

	log.Debug(fmt.Sprintf("Poetry publish completed successfully in %s", duration))
	return result, nil
}

// GetPublishCommand returns the Poetry publish command
func (p *PoetryUploadManager) GetPublishCommand() string {
	cmd := "poetry publish"

	if p.config.Username != "" && p.config.Password != "" {
		cmd += fmt.Sprintf(" -u %s -p ****", p.config.Username)
	} else if p.config.Token != "" {
		cmd += " -u __token__ -p ****"
	}

	if p.config.DryRun {
		cmd += " --dry-run"
	}

	if len(p.config.ExtraArgs) > 0 {
		cmd += " " + strings.Join(p.config.ExtraArgs, " ")
	}

	return cmd
}

// GetArtifactRepository returns the target repository URL
func (p *PoetryUploadManager) GetArtifactRepository() string {
	if p.config.RepositoryURL != "" {
		return p.config.RepositoryURL
	}

	// Default to PyPI
	return "https://pypi.org/simple/"
}

// GetPublishMetadata returns additional metadata about the publish operation
func (p *PoetryUploadManager) GetPublishMetadata() map[string]interface{} {
	metadata := map[string]interface{}{
		"packageManager": "poetry",
		"projectName":    p.projectInfo.Tool.Poetry.Name,
		"projectVersion": p.projectInfo.Tool.Poetry.Version,
		"description":    p.projectInfo.Tool.Poetry.Description,
		"authors":        p.projectInfo.Tool.Poetry.Authors,
		"buildDirectory": p.buildDir,
		"artifactCount":  len(p.GetBuildArtifacts()),
	}

	if p.projectInfo.Tool.Poetry.Repository != "" {
		metadata["projectRepository"] = p.projectInfo.Tool.Poetry.Repository
	}

	return metadata
}
