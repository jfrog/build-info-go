package flexpackupload

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jfrog/gofrog/crypto"
	"github.com/jfrog/gofrog/log"
)

// PoetryUploadManager implements FlexPackUploadManager for Poetry
type PoetryUploadManager struct {
	config        UploadConfig
	projectInfo   *PoetryProjectInfo
	artifacts     []ArtifactInfo
	buildDir      string
	publishResult *PublishResult
	authStrategy  AuthStrategy
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

// ===== Authentication Strategy Interfaces =====

// AuthStrategy defines the interface for different authentication methods
type AuthStrategy interface {
	// Configure sets up authentication from environment/config
	Configure(config map[string]string) error

	// Authenticate adds auth to HTTP request
	Authenticate(req *http.Request) error

	// Refresh handles token renewal if needed
	Refresh() error

	// IsValid checks if current auth is valid
	IsValid() bool

	// GetAuthType returns the type of authentication
	GetAuthType() string
}

// TokenAuth implements API token authentication
type TokenAuth struct {
	token     string
	tokenType string // "Bearer", "Token", etc.
}

// BasicAuth implements username/password authentication
type BasicAuth struct {
	username string
	password string
}

// JFrogAuth implements JFrog Platform authentication with token refresh
type JFrogAuth struct {
	baseURL      string
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	username     string
	password     string
}

// ===== Authentication Strategy Implementations =====

// Configure sets up token authentication
func (t *TokenAuth) Configure(config map[string]string) error {
	if token, exists := config["token"]; exists && token != "" {
		t.token = token
		t.tokenType = "Bearer"
		if tokenType, exists := config["token_type"]; exists {
			t.tokenType = tokenType
		}
		return nil
	}
	return fmt.Errorf("token not provided in configuration")
}

func (t *TokenAuth) Authenticate(req *http.Request) error {
	if t.token == "" {
		return fmt.Errorf("token not configured")
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", t.tokenType, t.token))
	return nil
}

func (t *TokenAuth) Refresh() error {
	// Token auth doesn't require refresh
	return nil
}

func (t *TokenAuth) IsValid() bool {
	return t.token != ""
}

func (t *TokenAuth) GetAuthType() string {
	return "token"
}

// Configure sets up basic authentication
func (b *BasicAuth) Configure(config map[string]string) error {
	username, hasUsername := config["username"]
	password, hasPassword := config["password"]

	if !hasUsername || !hasPassword {
		return fmt.Errorf("username and password required for basic auth")
	}

	b.username = username
	b.password = password
	return nil
}

func (b *BasicAuth) Authenticate(req *http.Request) error {
	if b.username == "" || b.password == "" {
		return fmt.Errorf("basic auth credentials not configured")
	}
	req.SetBasicAuth(b.username, b.password)
	return nil
}

func (b *BasicAuth) Refresh() error {
	// Basic auth doesn't require refresh
	return nil
}

func (b *BasicAuth) IsValid() bool {
	return b.username != "" && b.password != ""
}

func (b *BasicAuth) GetAuthType() string {
	return "basic"
}

// Configure sets up JFrog authentication
func (j *JFrogAuth) Configure(config map[string]string) error {
	j.baseURL = config["base_url"]
	j.accessToken = config["access_token"]
	j.refreshToken = config["refresh_token"]
	j.username = config["username"]
	j.password = config["password"]

	// If we have username/password but no tokens, we can authenticate to get tokens
	if j.accessToken == "" && j.username != "" && j.password != "" {
		return j.authenticateAndGetTokens()
	}

	if j.accessToken == "" {
		return fmt.Errorf("access token or username/password required for JFrog auth")
	}

	return nil
}

func (j *JFrogAuth) Authenticate(req *http.Request) error {
	if err := j.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", j.accessToken))
	return nil
}

func (j *JFrogAuth) Refresh() error {
	// Check if token needs refresh (5 minutes before expiry)
	if time.Now().After(j.expiresAt.Add(-5 * time.Minute)) {
		log.Debug("JFrog token needs refresh")
		return j.refreshAccessToken()
	}
	return nil
}

func (j *JFrogAuth) IsValid() bool {
	return j.accessToken != "" && time.Now().Before(j.expiresAt)
}

func (j *JFrogAuth) GetAuthType() string {
	return "jfrog"
}

func (j *JFrogAuth) authenticateAndGetTokens() error {
	// This would implement the JFrog authentication flow
	// For now, return an error indicating it's not implemented
	return fmt.Errorf("JFrog token authentication not yet implemented - please provide access_token directly")
}

func (j *JFrogAuth) refreshAccessToken() error {
	// This would implement the JFrog token refresh flow
	// For now, return an error indicating it's not implemented
	return fmt.Errorf("JFrog token refresh not yet implemented")
}

// ===== Error Handling and Retry Logic =====

// UploadError represents different types of upload errors
type UploadError struct {
	Type       ErrorType
	Message    string
	Retryable  bool
	RetryAfter time.Duration
	StatusCode int
	Attempt    int
}

// ErrorType categorizes different types of errors
type ErrorType int

const (
	ErrorTypeAuth ErrorType = iota
	ErrorTypeNetwork
	ErrorTypeServer
	ErrorTypeClient
	ErrorTypeTimeout
	ErrorTypeQuota
	ErrorTypeValidation
)

func (e *UploadError) Error() string {
	return fmt.Sprintf("[%s] %s (attempt %d)", e.getTypeString(), e.Message, e.Attempt)
}

func (e *UploadError) getTypeString() string {
	switch e.Type {
	case ErrorTypeAuth:
		return "AUTH"
	case ErrorTypeNetwork:
		return "NETWORK"
	case ErrorTypeServer:
		return "SERVER"
	case ErrorTypeClient:
		return "CLIENT"
	case ErrorTypeTimeout:
		return "TIMEOUT"
	case ErrorTypeQuota:
		return "QUOTA"
	case ErrorTypeValidation:
		return "VALIDATION"
	default:
		return "UNKNOWN"
	}
}

// RetryStrategy defines retry behavior
type RetryStrategy struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	Jitter         bool
}

// DefaultRetryStrategy returns a sensible default retry strategy
func DefaultRetryStrategy() *RetryStrategy {
	return &RetryStrategy{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
		Jitter:         true,
	}
}

// CircuitBreaker prevents cascading failures
type CircuitBreaker struct {
	failures    int
	maxFailures int
	timeout     time.Duration
	lastFailure time.Time
	mu          sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     timeout,
	}
}

func (cb *CircuitBreaker) Call(operation func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if circuit is open
	if cb.isOpen() {
		return fmt.Errorf("circuit breaker is open - too many failures")
	}

	// Execute operation
	err := operation()
	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) isOpen() bool {
	return cb.failures >= cb.maxFailures &&
		time.Since(cb.lastFailure) < cb.timeout
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.lastFailure = time.Now()
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.failures = 0
}

// classifyError categorizes errors for retry logic
func classifyError(err error, statusCode int) *UploadError {
	uploadErr := &UploadError{
		Message:    err.Error(),
		StatusCode: statusCode,
	}

	// Network errors
	if netErr, ok := err.(net.Error); ok {
		uploadErr.Type = ErrorTypeNetwork
		uploadErr.Retryable = !netErr.Timeout()
		if netErr.Timeout() {
			uploadErr.Type = ErrorTypeTimeout
			uploadErr.Retryable = true
		}
		return uploadErr
	}

	// HTTP status code based classification
	switch {
	case statusCode == 401 || statusCode == 403:
		uploadErr.Type = ErrorTypeAuth
		uploadErr.Retryable = false
	case statusCode == 429:
		uploadErr.Type = ErrorTypeQuota
		uploadErr.Retryable = true
		// Try to parse Retry-After header
		uploadErr.RetryAfter = 60 * time.Second // Default
	case statusCode >= 500 && statusCode < 600:
		uploadErr.Type = ErrorTypeServer
		uploadErr.Retryable = true
	case statusCode >= 400 && statusCode < 500:
		uploadErr.Type = ErrorTypeClient
		uploadErr.Retryable = false
	default:
		uploadErr.Type = ErrorTypeClient
		uploadErr.Retryable = false
	}

	return uploadErr
}

// executeWithRetry executes an operation with retry logic
func (p *PoetryUploadManager) executeWithRetry(ctx context.Context, operation func() error, strategy *RetryStrategy) error {
	var lastErr error
	backoff := strategy.InitialBackoff

	for attempt := 0; attempt <= strategy.MaxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on the last attempt
		if attempt == strategy.MaxRetries {
			break
		}

		// Check if error is retryable
		if uploadErr, ok := err.(*UploadError); ok {
			uploadErr.Attempt = attempt + 1
			if !uploadErr.Retryable {
				return uploadErr
			}

			// Use specific retry delay if provided
			waitTime := backoff
			if uploadErr.RetryAfter > 0 {
				waitTime = uploadErr.RetryAfter
			}

			log.Debug("Retrying operation after %v (attempt %d/%d): %v",
				waitTime, attempt+1, strategy.MaxRetries, uploadErr)

			// Wait with context cancellation support
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
				// Continue to next attempt
			}

			// Calculate next backoff with exponential backoff and jitter
			backoff = time.Duration(float64(backoff) * strategy.Multiplier)
			if backoff > strategy.MaxBackoff {
				backoff = strategy.MaxBackoff
			}

			// Add jitter to prevent thundering herd
			if strategy.Jitter {
				jitterFactor := float64(2*time.Now().UnixNano()%2 - 1)
				jitter := time.Duration(float64(backoff) * 0.1 * jitterFactor)
				backoff += jitter
			}
		} else {
			// Non-upload error, don't retry
			return err
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", strategy.MaxRetries+1, lastErr)
}

// parseRetryAfter parses the Retry-After header
func parseRetryAfter(retryAfter string) time.Duration {
	if retryAfter == "" {
		return 0
	}

	// Try to parse as seconds
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try to parse as HTTP date
	if t, err := time.Parse(time.RFC1123, retryAfter); err == nil {
		return time.Until(t)
	}

	return 0
}

// ===== Progress Tracking and Monitoring =====

// ProgressTracker tracks upload progress
type ProgressTracker struct {
	TotalBytes    int64
	UploadedBytes int64
	StartTime     time.Time
	CurrentFile   string

	mu        sync.Mutex
	listeners []ProgressListener
}

// ProgressListener defines callbacks for progress events
type ProgressListener interface {
	OnProgress(uploaded, total int64, speed float64, currentFile string)
	OnFileStart(filename string, size int64)
	OnFileComplete(filename string, duration time.Duration)
	OnComplete(totalDuration time.Duration)
	OnError(err error)
}

// ConsoleProgressListener provides console output for progress
type ConsoleProgressListener struct{}

func (c *ConsoleProgressListener) OnProgress(uploaded, total int64, speed float64, currentFile string) {
	if total > 0 {
		percentage := float64(uploaded) / float64(total) * 100
		speedMB := speed / (1024 * 1024) // Convert to MB/s
		log.Info("Upload progress: %.1f%% (%d/%d bytes) - %.2f MB/s - %s",
			percentage, uploaded, total, speedMB, currentFile)
	}
}

func (c *ConsoleProgressListener) OnFileStart(filename string, size int64) {
	sizeMB := float64(size) / (1024 * 1024)
	log.Info("Starting upload: %s (%.2f MB)", filename, sizeMB)
}

func (c *ConsoleProgressListener) OnFileComplete(filename string, duration time.Duration) {
	log.Info("Completed upload: %s in %v", filename, duration)
}

func (c *ConsoleProgressListener) OnComplete(totalDuration time.Duration) {
	log.Info("All uploads completed in %v", totalDuration)
}

func (c *ConsoleProgressListener) OnError(err error) {
	log.Error("Upload error: %v", err)
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(totalBytes int64) *ProgressTracker {
	return &ProgressTracker{
		TotalBytes: totalBytes,
		StartTime:  time.Now(),
		listeners:  []ProgressListener{},
	}
}

// AddListener adds a progress listener
func (p *ProgressTracker) AddListener(listener ProgressListener) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.listeners = append(p.listeners, listener)
}

// SetCurrentFile sets the currently uploading file
func (p *ProgressTracker) SetCurrentFile(filename string, size int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CurrentFile = filename

	for _, listener := range p.listeners {
		listener.OnFileStart(filename, size)
	}
}

// AddProgress adds uploaded bytes and notifies listeners
func (p *ProgressTracker) AddProgress(bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.UploadedBytes += bytes
	elapsed := time.Since(p.StartTime).Seconds()
	speed := float64(p.UploadedBytes) / elapsed

	for _, listener := range p.listeners {
		listener.OnProgress(p.UploadedBytes, p.TotalBytes, speed, p.CurrentFile)
	}
}

// FileComplete notifies that a file upload is complete
func (p *ProgressTracker) FileComplete(filename string, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, listener := range p.listeners {
		listener.OnFileComplete(filename, duration)
	}
}

// Complete notifies that all uploads are complete
func (p *ProgressTracker) Complete() {
	p.mu.Lock()
	defer p.mu.Unlock()

	totalDuration := time.Since(p.StartTime)
	for _, listener := range p.listeners {
		listener.OnComplete(totalDuration)
	}
}

// Error notifies listeners of an error
func (p *ProgressTracker) Error(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, listener := range p.listeners {
		listener.OnError(err)
	}
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	reader  *os.File
	tracker *ProgressTracker
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.tracker.AddProgress(int64(n))
	}
	return n, err
}

func (pr *progressReader) Close() error {
	return pr.reader.Close()
}

// wrapWithProgressTracking wraps a file with progress tracking
func (p *PoetryUploadManager) wrapWithProgressTracking(file *os.File, tracker *ProgressTracker) *progressReader {
	return &progressReader{
		reader:  file,
		tracker: tracker,
	}
}

// ===== Build Pipeline with Validation and Post-Processing =====

// BuildPipeline manages the complete build and upload process
type BuildPipeline struct {
	validator PackageValidator
	builder   PackageBuilder
	processor PostProcessor
	uploader  PackageUploader
}

// PackageValidator validates project structure and configuration
type PackageValidator interface {
	ValidateProject(projectPath string) error
	ValidateConfiguration(config UploadConfig) error
}

// PackageBuilder handles package building
type PackageBuilder interface {
	Build(projectPath string, config UploadConfig) (*BuildResult, error)
	Clean(projectPath string) error
}

// PostProcessor handles post-build processing
type PostProcessor interface {
	Process(artifacts []ArtifactInfo, config UploadConfig) error
	ValidateArtifacts(artifacts []ArtifactInfo) error
}

// PackageUploader handles the actual upload process
type PackageUploader interface {
	Upload(ctx context.Context, artifacts []ArtifactInfo, config UploadConfig) (*PublishResult, error)
}

// BuildResult represents the result of a build operation
type BuildResult struct {
	Success   bool
	Artifacts []ArtifactInfo
	Duration  time.Duration
	Output    string
	Error     string
}

// PoetryValidator implements PackageValidator for Poetry projects
type PoetryValidator struct{}

func (v *PoetryValidator) ValidateProject(projectPath string) error {
	// Check for required files
	requiredFiles := []string{"pyproject.toml"}
	for _, file := range requiredFiles {
		filePath := filepath.Join(projectPath, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return &UploadError{
				Type:    ErrorTypeValidation,
				Message: fmt.Sprintf("required file missing: %s", file),
			}
		}
	}

	// Validate pyproject.toml structure
	return v.validatePyprojectToml(projectPath)
}

func (v *PoetryValidator) ValidateConfiguration(config UploadConfig) error {
	if config.WorkingDirectory == "" {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: "working directory is required",
		}
	}

	if config.RepositoryURL == "" {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: "repository URL is required",
		}
	}

	// Validate authentication configuration
	hasToken := config.Token != ""
	hasBasicAuth := config.Username != "" && config.Password != ""
	hasEnvAuth := false

	// Check environment variables
	for key := range config.Environment {
		if strings.Contains(strings.ToLower(key), "token") ||
			strings.Contains(strings.ToLower(key), "password") {
			hasEnvAuth = true
			break
		}
	}

	if !hasToken && !hasBasicAuth && !hasEnvAuth {
		log.Warn("No authentication configured - this may cause upload failures")
	}

	return nil
}

func (v *PoetryValidator) validatePyprojectToml(projectPath string) error {
	pyprojectPath := filepath.Join(projectPath, "pyproject.toml")
	content, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("failed to read pyproject.toml: %v", err),
		}
	}

	var project PoetryProjectInfo
	if err := toml.Unmarshal(content, &project); err != nil {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: fmt.Sprintf("invalid pyproject.toml format: %v", err),
		}
	}

	// Validate required fields
	if project.Tool.Poetry.Name == "" {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: "project name is required in pyproject.toml",
		}
	}

	if project.Tool.Poetry.Version == "" {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: "project version is required in pyproject.toml",
		}
	}

	return nil
}

// PoetryBuilder implements PackageBuilder for Poetry projects
type PoetryBuilder struct{}

func (b *PoetryBuilder) Build(projectPath string, config UploadConfig) (*BuildResult, error) {
	startTime := time.Now()

	cmd := exec.Command("poetry", "build")
	cmd.Dir = projectPath

	// Set environment variables
	cmd.Env = os.Environ()
	for key, value := range config.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	result := &BuildResult{
		Success:  err == nil,
		Duration: duration,
		Output:   string(output),
	}

	if err != nil {
		result.Error = err.Error()
		return result, &UploadError{
			Type:    ErrorTypeClient,
			Message: fmt.Sprintf("poetry build failed: %v", err),
		}
	}

	// Scan for built artifacts
	result.Artifacts = b.scanArtifacts(projectPath)

	return result, nil
}

func (b *PoetryBuilder) Clean(projectPath string) error {
	distPath := filepath.Join(projectPath, "dist")
	if _, err := os.Stat(distPath); err == nil {
		log.Debug("Cleaning build directory: %s", distPath)
		return os.RemoveAll(distPath)
	}
	return nil
}

func (b *PoetryBuilder) scanArtifacts(projectPath string) []ArtifactInfo {
	var artifacts []ArtifactInfo
	distPath := filepath.Join(projectPath, "dist")

	files, err := filepath.Glob(filepath.Join(distPath, "*"))
	if err != nil {
		log.Debug("Failed to scan artifacts: %v", err)
		return artifacts
	}

	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			artifact := ArtifactInfo{
				Name: filepath.Base(file),
				Path: file,
				Size: info.Size(),
			}

			// Determine artifact type
			ext := strings.ToLower(filepath.Ext(file))
			switch ext {
			case ".whl":
				artifact.Type = "wheel"
			case ".gz":
				if strings.Contains(file, ".tar.gz") {
					artifact.Type = "sdist"
				}
			default:
				artifact.Type = "unknown"
			}

			artifacts = append(artifacts, artifact)
		}
	}

	return artifacts
}

// PoetryPostProcessor implements PostProcessor for Poetry projects
type PoetryPostProcessor struct{}

func (p *PoetryPostProcessor) Process(artifacts []ArtifactInfo, config UploadConfig) error {
	log.Debug("Post-processing %d artifacts", len(artifacts))

	for i := range artifacts {
		// Calculate checksums
		if err := p.calculateChecksums(&artifacts[i]); err != nil {
			log.Warn("Failed to calculate checksums for %s: %v", artifacts[i].Name, err)
		}

		// Set publication timestamp
		artifacts[i].PublishedAt = time.Now().Format(time.RFC3339)
	}

	return nil
}

func (p *PoetryPostProcessor) ValidateArtifacts(artifacts []ArtifactInfo) error {
	if len(artifacts) == 0 {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: "no artifacts found - run 'poetry build' first",
		}
	}

	// Check for both wheel and source distribution
	hasWheel := false
	hasSdist := false

	for _, artifact := range artifacts {
		switch artifact.Type {
		case "wheel":
			hasWheel = true
		case "sdist":
			hasSdist = true
		}
	}

	if !hasWheel && !hasSdist {
		return &UploadError{
			Type:    ErrorTypeValidation,
			Message: "no valid Python packages found (expected .whl or .tar.gz files)",
		}
	}

	log.Debug("Validation passed: found %d artifacts (wheel: %v, sdist: %v)",
		len(artifacts), hasWheel, hasSdist)

	return nil
}

func (p *PoetryPostProcessor) calculateChecksums(artifact *ArtifactInfo) error {
	fileDetails, err := crypto.GetFileDetails(artifact.Path, true)
	if err != nil {
		return err
	}

	artifact.SHA1 = fileDetails.Checksum.Sha1
	artifact.SHA256 = fileDetails.Checksum.Sha256
	artifact.MD5 = fileDetails.Checksum.Md5

	return nil
}

// NewBuildPipeline creates a new build pipeline for Poetry
func NewBuildPipeline() *BuildPipeline {
	return &BuildPipeline{
		validator: &PoetryValidator{},
		builder:   &PoetryBuilder{},
		processor: &PoetryPostProcessor{},
	}
}

// Execute runs the complete build pipeline
func (bp *BuildPipeline) Execute(projectPath string, config UploadConfig) (*BuildResult, error) {
	// Phase 1: Validation
	log.Debug("Phase 1: Validating project and configuration")
	if err := bp.validator.ValidateProject(projectPath); err != nil {
		return nil, fmt.Errorf("project validation failed: %w", err)
	}

	if err := bp.validator.ValidateConfiguration(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Phase 2: Clean previous builds (optional)
	if !config.DryRun {
		log.Debug("Phase 2: Cleaning previous builds")
		if err := bp.builder.Clean(projectPath); err != nil {
			log.Warn("Failed to clean previous builds: %v", err)
		}
	}

	// Phase 3: Build
	log.Debug("Phase 3: Building package")
	result, err := bp.builder.Build(projectPath, config)
	if err != nil {
		return result, fmt.Errorf("build failed: %w", err)
	}

	// Phase 4: Post-processing
	log.Debug("Phase 4: Post-processing artifacts")
	if err := bp.processor.ValidateArtifacts(result.Artifacts); err != nil {
		return result, fmt.Errorf("artifact validation failed: %w", err)
	}

	if err := bp.processor.Process(result.Artifacts, config); err != nil {
		return result, fmt.Errorf("post-processing failed: %w", err)
	}

	log.Debug("Build pipeline completed successfully - %d artifacts ready", len(result.Artifacts))
	return result, nil
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

	// Setup authentication strategy
	if err := manager.setupAuthentication(); err != nil {
		return nil, fmt.Errorf("failed to setup authentication: %w", err)
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

	log.Debug("Loaded Poetry project: %s v%s", p.projectInfo.Tool.Poetry.Name, p.projectInfo.Tool.Poetry.Version)
	return nil
}

// setupAuthentication configures the appropriate authentication strategy
func (p *PoetryUploadManager) setupAuthentication() error {
	authConfig := make(map[string]string)

	// Collect authentication configuration from various sources
	authConfig["username"] = p.config.Username
	authConfig["password"] = p.config.Password
	authConfig["token"] = p.config.Token

	// Add environment variables
	for key, value := range p.config.Environment {
		authConfig[strings.ToLower(key)] = value
	}

	// Check for standard environment variables
	if token := os.Getenv("POETRY_PYPI_TOKEN_PYPI"); token != "" {
		authConfig["token"] = token
	}
	if token := os.Getenv("POETRY_HTTP_BASIC_PYPI_USERNAME"); token != "" {
		authConfig["username"] = token
	}
	if token := os.Getenv("POETRY_HTTP_BASIC_PYPI_PASSWORD"); token != "" {
		authConfig["password"] = token
	}

	// Determine authentication strategy based on available credentials
	if p.isJFrogRepository() {
		// Use JFrog authentication for JFrog repositories
		p.authStrategy = &JFrogAuth{}
		authConfig["base_url"] = p.extractBaseURL()
	} else if authConfig["token"] != "" {
		// Use token authentication if token is available
		p.authStrategy = &TokenAuth{}
	} else if authConfig["username"] != "" && authConfig["password"] != "" {
		// Use basic authentication if username/password available
		p.authStrategy = &BasicAuth{}
	} else {
		// No authentication configured - this might be okay for some repositories
		log.Debug("No authentication configured - proceeding without auth")
		return nil
	}

	// Configure the selected strategy
	if p.authStrategy != nil {
		if err := p.authStrategy.Configure(authConfig); err != nil {
			return fmt.Errorf("failed to configure %s authentication: %w", p.authStrategy.GetAuthType(), err)
		}
		log.Debug("Configured %s authentication", p.authStrategy.GetAuthType())
	}

	return nil
}

// isJFrogRepository checks if the repository URL is a JFrog Artifactory instance
func (p *PoetryUploadManager) isJFrogRepository() bool {
	repoURL := p.GetArtifactRepository()
	return strings.Contains(repoURL, "/artifactory/") ||
		strings.Contains(repoURL, ".jfrog.io") ||
		strings.Contains(repoURL, "/api/pypi/")
}

// extractBaseURL extracts the base URL from the repository URL
func (p *PoetryUploadManager) extractBaseURL() string {
	repoURL := p.GetArtifactRepository()

	// For URLs like https://company.jfrog.io/artifactory/api/pypi/repo/
	if idx := strings.Index(repoURL, "/artifactory/"); idx != -1 {
		return repoURL[:idx]
	}

	// For URLs like https://artifactory.company.com/api/pypi/repo/
	if idx := strings.Index(repoURL, "/api/"); idx != -1 {
		return repoURL[:idx]
	}

	return repoURL
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
		log.Debug("Build directory does not exist: %s", p.buildDir)
		return
	}

	files, err := filepath.Glob(filepath.Join(p.buildDir, "*"))
	if err != nil {
		log.Debug("Failed to scan build directory: %v", err)
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

	log.Debug("Found %d build artifacts", len(p.artifacts))
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
		log.Debug("Skipping unknown artifact type: %s", filename)
		return nil
	}

	// Calculate checksums
	checksums, err := crypto.GetFileDetails(filePath, true)
	if err != nil {
		log.Debug("Failed to calculate checksums for %s: %v", filename, err)
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

	log.Debug("Created artifact info for %s (%s)", filename, artifactType)
	return artifact
}

// CalculateArtifactChecksums calculates checksums for all built artifacts
func (p *PoetryUploadManager) CalculateArtifactChecksums() []map[string]interface{} {
	artifacts := p.GetBuildArtifacts()
	var checksums []map[string]interface{}

	for _, artifact := range artifacts {
		checksum := map[string]interface{}{
			"type":   artifact.Type,
			"name":   artifact.Name,
			"path":   artifact.Path,
			"sha1":   artifact.SHA1,
			"sha256": artifact.SHA256,
			"md5":    artifact.MD5,
			"size":   artifact.Size,
		}
		checksums = append(checksums, checksum)
	}

	return checksums
}

// GetPublishCommand returns the Poetry publish command
func (p *PoetryUploadManager) GetPublishCommand() string {
	cmd := "poetry publish"

	if p.config.RepositoryURL != "" {
		// Extract repository name from URL or use custom repository
		repoName := p.extractRepositoryName(p.config.RepositoryURL)
		cmd += fmt.Sprintf(" -r %s", repoName)
	}

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

// extractRepositoryName extracts a repository name from URL for Poetry configuration
func (p *PoetryUploadManager) extractRepositoryName(url string) string {
	// For PyPI, use standard names
	if strings.Contains(url, "pypi.org") {
		return "pypi"
	}
	if strings.Contains(url, "test.pypi.org") {
		return "test-pypi"
	}

	// For custom repositories, use the domain name
	parts := strings.Split(url, "://")
	if len(parts) > 1 {
		domain := strings.Split(parts[1], "/")[0]
		domain = strings.Split(domain, ":")[0] // Remove port
		return strings.ReplaceAll(domain, ".", "-")
	}

	return "custom"
}

// GetArtifactRepository returns the target repository URL
func (p *PoetryUploadManager) GetArtifactRepository() string {
	if p.config.RepositoryURL != "" {
		return p.config.RepositoryURL
	}

	// Default to PyPI
	return "https://pypi.org/simple/"
}

// GetRepositoryName returns the repository name (not URL) for build info
func (p *PoetryUploadManager) GetRepositoryName() string {
	repoURL := p.GetArtifactRepository()

	// For Artifactory URLs, extract the repository name from the path
	// Example: https://artifactory.example.com/api/pypi/pypi-local/simple -> pypi-local
	// Example: https://entplus.jfrog.io/artifactory/api/pypi/ecosys-test-repo/simple -> ecosys-test-repo
	if strings.Contains(repoURL, "/artifactory/") || strings.Contains(repoURL, "/api/pypi/") {
		// Parse the URL to extract repository name
		parts := strings.Split(repoURL, "/")
		for i, part := range parts {
			if part == "pypi" && i+1 < len(parts) {
				// The next part after /pypi/ is the repository name
				repoName := parts[i+1]
				// Remove any trailing paths like /simple
				if repoName == "simple" && i > 0 {
					// If we hit "simple", the previous part is the repo name
					return parts[i-1]
				}
				return repoName
			}
		}
	}

	// For standard PyPI URLs, return "pypi" or "test-pypi"
	if strings.Contains(repoURL, "pypi.org") {
		return "pypi"
	}
	if strings.Contains(repoURL, "test.pypi.org") {
		return "test-pypi"
	}

	// For other URLs, try to extract a meaningful name
	return p.extractRepositoryName(repoURL)
}

// ValidateArtifacts checks if artifacts are ready for publication
func (p *PoetryUploadManager) ValidateArtifacts() error {
	artifacts := p.GetBuildArtifacts()

	if len(artifacts) == 0 {
		return fmt.Errorf("no build artifacts found - run 'poetry build' first")
	}

	// Check for required artifact types (both wheel and sdist are recommended)
	hasWheel := false
	hasSdist := false

	for _, artifact := range artifacts {
		switch artifact.Type {
		case "wheel":
			hasWheel = true
		case "sdist":
			hasSdist = true
		}
	}

	if !hasWheel && !hasSdist {
		return fmt.Errorf("no valid Python packages found (wheel or sdist)")
	}

	// Validate checksums are not empty
	for _, artifact := range artifacts {
		if artifact.SHA1 == "" || artifact.SHA256 == "" || artifact.MD5 == "" {
			return fmt.Errorf("missing checksums for artifact: %s", artifact.Name)
		}
	}

	log.Debug("Validation passed: %d artifacts ready for publication", len(artifacts))
	return nil
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

// BuildArtifacts builds Poetry packages using 'poetry build'
func (p *PoetryUploadManager) BuildArtifacts() (*PublishResult, error) {
	log.Debug("Building Poetry artifacts in %s", p.config.WorkingDirectory)

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

	log.Debug("Poetry build completed successfully in %s", duration)
	return result, nil
}

// PublishArtifacts publishes Poetry packages using 'poetry publish'
func (p *PoetryUploadManager) PublishArtifacts() (*PublishResult, error) {
	if err := p.ValidateArtifacts(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	log.Debug("Publishing Poetry artifacts to %s", p.GetArtifactRepository())

	args := []string{"publish"}

	// Add repository configuration
	// Note: If ExtraArgs already contains "-r", don't add it again
	hasRepoFlag := false
	for _, arg := range p.config.ExtraArgs {
		if arg == "-r" {
			hasRepoFlag = true
			break
		}
	}

	if !hasRepoFlag && p.config.RepositoryURL != "" {
		// Only add -r flag if it's not already in ExtraArgs
		repoName := p.extractRepositoryName(p.config.RepositoryURL)
		args = append(args, "-r", repoName)
	}

	// Add authentication
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

	p.publishResult = result
	log.Debug("Poetry publish completed successfully in %s", duration)
	return result, nil
}

// ===== Resumable Upload Support =====

// ResumableUploadState represents the state of a resumable upload
type ResumableUploadState struct {
	UploadID      string
	FilePath      string
	FileSize      int64
	UploadedBytes int64
	Checksum      string
	ChunkSize     int64
	Metadata      map[string]interface{}
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ChunkedUploader handles chunked/resumable uploads
type ChunkedUploader struct {
	chunkSize  int64
	maxRetries int
	stateStore UploadStateStore
}

// UploadStateStore interface for persisting upload state
type UploadStateStore interface {
	SaveState(state *ResumableUploadState) error
	LoadState(uploadID string) (*ResumableUploadState, error)
	DeleteState(uploadID string) error
}

// NewChunkedUploader creates a new chunked uploader
func NewChunkedUploader(chunkSize int64, maxRetries int) *ChunkedUploader {
	return &ChunkedUploader{
		chunkSize:  chunkSize,
		maxRetries: maxRetries,
		stateStore: &FileStateStore{}, // Default file-based state store
	}
}

// FileStateStore implements file-based upload state persistence
type FileStateStore struct {
	stateDir string
}

func (f *FileStateStore) SaveState(state *ResumableUploadState) error {
	// Basic implementation - would save state to file
	log.Debug("Saving upload state for %s: %d/%d bytes",
		state.FilePath, state.UploadedBytes, state.FileSize)
	return nil
}

func (f *FileStateStore) LoadState(uploadID string) (*ResumableUploadState, error) {
	// Basic implementation - would load state from file
	log.Debug("Loading upload state for %s", uploadID)
	return nil, fmt.Errorf("state not found")
}

func (f *FileStateStore) DeleteState(uploadID string) error {
	// Basic implementation - would delete state file
	log.Debug("Deleting upload state for %s", uploadID)
	return nil
}

// ===== Multi-Repository Publishing Support =====

// MultiRepoPublisher handles publishing to multiple repositories
type MultiRepoPublisher struct {
	managers []RepositoryManager
	strategy PublishStrategy
}

// RepositoryManager manages uploads to a specific repository
type RepositoryManager struct {
	Name    string
	Config  UploadConfig
	Manager *PoetryUploadManager
}

// PublishStrategy defines how to handle multi-repository publishing
type PublishStrategy int

const (
	PublishParallel PublishStrategy = iota
	PublishSequential
	PublishFailFast
	PublishBestEffort
)

// MultiPublishResult represents results from multi-repository publishing
type MultiPublishResult struct {
	Results    map[string]*PublishResult
	Errors     map[string]error
	Strategy   PublishStrategy
	TotalTime  time.Duration
	Successful int
	Failed     int
}

// NewMultiRepoPublisher creates a new multi-repository publisher
func NewMultiRepoPublisher(strategy PublishStrategy) *MultiRepoPublisher {
	return &MultiRepoPublisher{
		managers: []RepositoryManager{},
		strategy: strategy,
	}
}

// AddRepository adds a repository to publish to
func (m *MultiRepoPublisher) AddRepository(name string, config UploadConfig) error {
	manager, err := NewPoetryUploadManager(config)
	if err != nil {
		return fmt.Errorf("failed to create manager for %s: %w", name, err)
	}

	m.managers = append(m.managers, RepositoryManager{
		Name:    name,
		Config:  config,
		Manager: manager,
	})

	return nil
}

// PublishToAll publishes to all configured repositories
func (m *MultiRepoPublisher) PublishToAll(ctx context.Context) (*MultiPublishResult, error) {
	startTime := time.Now()

	result := &MultiPublishResult{
		Results:  make(map[string]*PublishResult),
		Errors:   make(map[string]error),
		Strategy: m.strategy,
	}

	switch m.strategy {
	case PublishParallel:
		m.publishParallel(ctx, result)
	case PublishSequential:
		m.publishSequential(ctx, result)
	case PublishFailFast:
		if err := m.publishFailFast(ctx, result); err != nil {
			return result, err
		}
	case PublishBestEffort:
		m.publishBestEffort(ctx, result)
	}

	result.TotalTime = time.Since(startTime)
	result.Successful = len(result.Results)
	result.Failed = len(result.Errors)

	log.Info("Multi-repo publish completed: %d successful, %d failed in %v",
		result.Successful, result.Failed, result.TotalTime)

	return result, nil
}

func (m *MultiRepoPublisher) publishParallel(ctx context.Context, result *MultiPublishResult) {
	var wg sync.WaitGroup
	mu := sync.Mutex{}

	for _, repo := range m.managers {
		wg.Add(1)
		go func(r RepositoryManager) {
			defer wg.Done()

			publishResult, err := r.Manager.PublishArtifacts()

			mu.Lock()
			if err != nil {
				result.Errors[r.Name] = err
			} else {
				result.Results[r.Name] = publishResult
			}
			mu.Unlock()
		}(repo)
	}

	wg.Wait()
}

func (m *MultiRepoPublisher) publishSequential(ctx context.Context, result *MultiPublishResult) {
	for _, repo := range m.managers {
		publishResult, err := repo.Manager.PublishArtifacts()
		if err != nil {
			result.Errors[repo.Name] = err
		} else {
			result.Results[repo.Name] = publishResult
		}
	}
}

func (m *MultiRepoPublisher) publishFailFast(ctx context.Context, result *MultiPublishResult) error {
	for _, repo := range m.managers {
		publishResult, err := repo.Manager.PublishArtifacts()
		if err != nil {
			result.Errors[repo.Name] = err
			return fmt.Errorf("publish failed for %s: %w", repo.Name, err)
		}
		result.Results[repo.Name] = publishResult
	}
	return nil
}

func (m *MultiRepoPublisher) publishBestEffort(ctx context.Context, result *MultiPublishResult) {
	// Same as sequential but continues on errors
	m.publishSequential(ctx, result)
}
