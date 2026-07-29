package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jfrog/gofrog/log"
)

const (
	// currentBranchEnvVar is the environment variable holding the branch under test.
	currentBranchEnvVar = "CURRENT_BRANCH"
	// githubOutputEnvVar is the environment variable pointing to the GitHub Actions output file.
	githubOutputEnvVar = "GITHUB_OUTPUT"
	// defaultBranch is used when no branch is provided via the environment.
	defaultBranch = "main"
	// masterBranch is the default ref the dependency repositories fall back to.
	masterBranch = "master"

	// githubBaseURL is the base URL for cloning GitHub repositories.
	githubBaseURL = "https://github.com"
	// githubAPIBaseURL is the base URL for the GitHub REST API.
	githubAPIBaseURL = "https://api.github.com"
	// jfrogOrg is the GitHub organization that hosts the dependency repositories.
	jfrogOrg = "jfrog"

	// githubSHAFullLength is the length of a full Git commit SHA.
	githubSHAFullLength = 40
	// githubSHAShortMinLength is the minimum length of an abbreviated commit SHA.
	githubSHAShortMinLength = 7

	// githubOutputFilePerm is the permission used when opening the GitHub output file.
	githubOutputFilePerm = 0644
)

// GoMod represents the JSON structure from `go mod edit -json`
type GoMod struct {
	Module  Module    `json:"Module"`
	Go      string    `json:"Go"`
	Require []Require `json:"Require"`
	Replace []Replace `json:"Replace"`
}

// Module represents the module path
type Module struct {
	Path string `json:"Path"`
}

// Require represents a required module
type Require struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Indirect bool   `json:"Indirect"`
}

// Replace represents a replace directive
type Replace struct {
	Old ModuleVersion `json:"Old"`
	New ModuleVersion `json:"New"`
}

// ModuleVersion represents a module with optional version
type ModuleVersion struct {
	Path    string `json:"Path"`
	Version string `json:"Version,omitempty"`
}

// DependencyInfo holds information about a detected dependency
type DependencyInfo struct {
	Name       string
	ModulePath string
	Repo       string
	Ref        string
}

// jfrogDependencies maps short names to their module paths.
// build-info-go is intentionally excluded: it is the repository under test and
// is always checked out at the workspace root, so it is never a detected sibling.
var jfrogDependencies = map[string]string{
	"jfrog-cli":             "github.com/jfrog/jfrog-cli",
	"jfrog-cli-artifactory": "github.com/jfrog/jfrog-cli-artifactory",
	"jfrog-cli-core":        "github.com/jfrog/jfrog-cli-core/v2",
	"jfrog-client-go":       "github.com/jfrog/jfrog-client-go",
}

func main() {
	// Get current branch from environment
	currentBranch := os.Getenv(currentBranchEnvVar)
	if currentBranch == "" {
		currentBranch = defaultBranch
	}

	// Parse go.mod
	goMod, err := parseGoMod()
	if err != nil {
		log.Error(fmt.Sprintf("Error parsing go.mod: %v", err))
		os.Exit(1)
	}

	// Build a map of replace directives
	replaces := make(map[string]Replace)
	for _, r := range goMod.Replace {
		replaces[r.Old.Path] = r
	}

	// Open GITHUB_OUTPUT file for writing outputs
	outputFile := os.Getenv(githubOutputEnvVar)
	var output *os.File
	if outputFile != "" {
		var err error
		cleanOutputFile := filepath.Clean(outputFile)
		output, err = os.OpenFile(cleanOutputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, githubOutputFilePerm) // #nosec G703 -- GitHub Actions env var
		if err != nil {
			log.Error(fmt.Sprintf("Error opening %s: %v", githubOutputEnvVar, err))
			os.Exit(1)
		}
		defer output.Close()
	}

	// Collect every resolved repo/ref pair into a single map so the workflow can
	// consume one JSON "deps" output instead of repeating per-dependency declarations.
	deps := make(map[string]string)
	for name, modulePath := range jfrogDependencies {
		info := detectDependency(name, modulePath, replaces, currentBranch)
		collectDependency(deps, name, info)
	}

	if err := writeDeps(output, deps); err != nil {
		log.Error(fmt.Sprintf("Error writing dependency output: %v", err))
		os.Exit(1)
	}
}

// parseGoMod runs `go mod edit -json` and parses the output
func parseGoMod() (*GoMod, error) {
	cmd := exec.Command("go", "mod", "edit", "-json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go mod edit -json: %w", err)
	}

	var goMod GoMod
	if err := json.Unmarshal(out, &goMod); err != nil {
		return nil, fmt.Errorf("failed to parse go.mod JSON: %w", err)
	}

	return &goMod, nil
}

// detectDependency determines the repository and ref for a dependency
func detectDependency(name, modulePath string, replaces map[string]Replace, currentBranch string) *DependencyInfo {
	// Check if there's a replace directive for this module
	if replace, ok := replaces[modulePath]; ok {
		// Parse the replace target
		newPath := replace.New.Path
		if strings.HasPrefix(newPath, "github.com/") {
			// Extract repo and version/ref, stripping Go module major version suffixes (e.g. /v2)
			parts := strings.TrimPrefix(newPath, "github.com/")
			repo := stripMajorVersionSuffix(parts)
			ref := replace.New.Version

			if ref != "" {
				// Prefer the current branch if it exists on the forked repo,
				if branchExists(repo, currentBranch) {
					log.Info(fmt.Sprintf("Found replace directive: %s => %s, branch '%s' exists, using it", name, repo, currentBranch))
					return &DependencyInfo{
						Name:       name,
						ModulePath: modulePath,
						Repo:       repo,
						Ref:        currentBranch,
					}
				}
				// Extract the short commit hash from pseudo-version, then
				// resolve it to a full 40-char SHA so actions/checkout can fetch it
				ref = extractCommitFromPseudoVersion(ref)
				fullSHA := resolveFullSHA(repo, ref)
				if fullSHA == "" {
					log.Error(fmt.Sprintf("Error: could not resolve commit %s to full SHA in %s", ref, repo))
					os.Exit(1)
				}
				ref = fullSHA
				log.Info(fmt.Sprintf("Found replace directive: %s => %s @ %s", name, repo, ref))
				return &DependencyInfo{
					Name:       name,
					ModulePath: modulePath,
					Repo:       repo,
					Ref:        ref,
				}
			}
		}
	}

	// No replace directive found, check if current branch exists in the dependency repo
	repo := fmt.Sprintf("%s/%s", jfrogOrg, name)

	// Check if the current branch exists in the repo
	if branchExists(repo, currentBranch) {
		log.Info(fmt.Sprintf("Branch '%s' exists in %s, using it", currentBranch, repo))
		return &DependencyInfo{
			Name:       name,
			ModulePath: modulePath,
			Repo:       repo,
			Ref:        currentBranch,
		}
	}

	log.Info(fmt.Sprintf("No matching branch for %s, will use default (%s)", name, masterBranch))
	return nil
}

// pseudoVersionPattern matches Go module pseudo-versions and captures the commit hash.
// Format: vX.Y.Z-0.YYYYMMDDHHMMSS-COMMITHASH or vX.Y.Z-pre.0.YYYYMMDDHHMMSS-COMMITHASH
var pseudoVersionPattern = regexp.MustCompile(`-(?:\d+\.)?(\d{14})-([a-f0-9]{12})$`)

// extractCommitFromPseudoVersion returns the 12-char commit hash from a Go pseudo-version,
// or the original string if it is not a pseudo-version.
func extractCommitFromPseudoVersion(version string) string {
	matches := pseudoVersionPattern.FindStringSubmatch(version)
	if len(matches) == 3 {
		return matches[2]
	}
	return version
}

// resolveFullSHA resolves a short commit hash to a full 40-char SHA using the GitHub API.
// Returns empty string if resolution fails
func resolveFullSHA(repo, shortHash string) string {
	if len(shortHash) < githubSHAShortMinLength || len(shortHash) >= githubSHAFullLength {
		return ""
	}
	if !validGitRefPattern.MatchString(repo) || !validGitRefPattern.MatchString(shortHash) {
		return ""
	}
	apiURL := fmt.Sprintf("%s/repos/%s/commits/%s", githubAPIBaseURL, repo, shortHash)
	cmd := exec.Command("curl", "-sf", "-H", "Accept: application/vnd.github.v3.sha", apiURL) // #nosec G204 -- inputs validated
	out, err := cmd.Output()
	if err != nil {
		log.Warn(fmt.Sprintf("could not resolve full SHA for %s in %s: %v", shortHash, repo, err))
		return ""
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) == githubSHAFullLength {
		log.Info(fmt.Sprintf("Resolved short hash %s to full SHA %s", shortHash, sha))
		return sha
	}
	return ""
}

// majorVersionSuffix matches Go module major version suffixes like /v2, /v3, etc.
var majorVersionSuffix = regexp.MustCompile(`/v\d+$`)

// stripMajorVersionSuffix removes the Go module major version suffix (e.g. /v2)
// from a repo path, since it's not part of the actual GitHub repository name.
func stripMajorVersionSuffix(repoPath string) string {
	return majorVersionSuffix.ReplaceAllString(repoPath, "")
}

// isValidGitRef validates that a string is a valid git reference name
// This prevents command injection by ensuring only safe characters are used
var validGitRefPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

// branchExists checks if a branch exists in a GitHub repository
func branchExists(repo, branch string) bool {
	if branch == "" || branch == defaultBranch || branch == masterBranch {
		return false
	}

	// Validate repo format to prevent command injection (should be org/repo format)
	if !validGitRefPattern.MatchString(repo) {
		log.Warn(fmt.Sprintf("invalid repo format '%s'", repo))
		return false
	}

	// Validate branch name to prevent command injection
	if !validGitRefPattern.MatchString(branch) {
		log.Warn(fmt.Sprintf("invalid branch format '%s'", branch))
		return false
	}

	url := fmt.Sprintf("%s/%s.git", githubBaseURL, repo)
	cmd := exec.Command("git", "ls-remote", "--heads", url, branch) // #nosec G702 -- inputs validated above
	out, err := cmd.Output()
	if err != nil {
		log.Warn(fmt.Sprintf("failed to check branch '%s' in %s: %v", branch, repo, err))
		return false
	}

	return strings.Contains(string(out), fmt.Sprintf("refs/heads/%s", branch))
}

// collectDependency records the resolved repo/ref for a dependency into the deps map,
// using the snake_case key format expected by the workflow (e.g. "jfrog-cli" -> "jfrog_cli_repo").
func collectDependency(deps map[string]string, name string, info *DependencyInfo) {
	keyName := strings.ReplaceAll(name, "-", "_")

	var repo, ref string
	if info != nil {
		repo = info.Repo
		ref = info.Ref
		log.Info(fmt.Sprintf("  %s_repo=%s", keyName, repo))
		log.Info(fmt.Sprintf("  %s_ref=%s", keyName, ref))
	} else {
		log.Info(fmt.Sprintf("  %s: using default", keyName))
	}

	deps[keyName+"_repo"] = repo
	deps[keyName+"_ref"] = ref
}

// writeDeps marshals all dependency repo/ref pairs into a single JSON object and
// writes it to GITHUB_OUTPUT as the "deps" output. This lets the workflow pass one
// value to the composite actions instead of repeating each key in every job.
func writeDeps(output *os.File, deps map[string]string) error {
	data, err := json.Marshal(deps)
	if err != nil {
		return fmt.Errorf("failed to marshal dependencies: %w", err)
	}

	// Write the machine-readable output consumed by GitHub Actions.
	if output != nil {
		if _, err := fmt.Fprintf(output, "deps=%s\n", data); err != nil {
			return fmt.Errorf("failed to write deps to %s: %w", githubOutputEnvVar, err)
		}
	}

	log.Info(fmt.Sprintf("deps=%s", data))
	return nil
}
