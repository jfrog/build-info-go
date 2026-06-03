package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
	currentBranch := os.Getenv("CURRENT_BRANCH")
	if currentBranch == "" {
		currentBranch = "main"
	}

	// Parse go.mod
	goMod, err := parseGoMod()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing go.mod: %v\n", err)
		os.Exit(1)
	}

	// Build a map of replace directives
	replaces := make(map[string]Replace)
	for _, r := range goMod.Replace {
		replaces[r.Old.Path] = r
	}

	// Open GITHUB_OUTPUT file for writing outputs
	outputFile := os.Getenv("GITHUB_OUTPUT")
	var output *os.File
	if outputFile != "" {
		var err error
		cleanOutputFile := filepath.Clean(outputFile)
		output, err = os.OpenFile(cleanOutputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644) // #nosec G703 -- GitHub Actions env var
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening GITHUB_OUTPUT: %v\n", err)
			os.Exit(1)
		}
		defer output.Close()
	}

	// Process each dependency
	for name, modulePath := range jfrogDependencies {
		info := detectDependency(name, modulePath, replaces, currentBranch)
		writeOutput(output, name, info)
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
					fmt.Printf("Found replace directive: %s => %s, branch '%s' exists, using it\n", name, repo, currentBranch)
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
					fmt.Fprintf(os.Stderr, "Error: could not resolve commit %s to full SHA in %s\n", ref, repo)
					os.Exit(1)
				}
				ref = fullSHA
				fmt.Printf("Found replace directive: %s => %s @ %s\n", name, repo, ref)
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
	repo := fmt.Sprintf("jfrog/%s", name)

	// Check if the current branch exists in the repo
	if branchExists(repo, currentBranch) {
		fmt.Printf("Branch '%s' exists in %s, using it\n", currentBranch, repo)
		return &DependencyInfo{
			Name:       name,
			ModulePath: modulePath,
			Repo:       repo,
			Ref:        currentBranch,
		}
	}

	fmt.Printf("No matching branch for %s, will use default (master)\n", name)
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
	if len(shortHash) < 7 || len(shortHash) >= 40 {
		return ""
	}
	if !validGitRefPattern.MatchString(repo) || !validGitRefPattern.MatchString(shortHash) {
		return ""
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repo, shortHash)
	cmd := exec.Command("curl", "-sf", "-H", "Accept: application/vnd.github.v3.sha", apiURL) // #nosec G204 -- inputs validated
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve full SHA for %s in %s: %v\n", shortHash, repo, err)
		return ""
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) == 40 {
		fmt.Printf("Resolved short hash %s to full SHA %s\n", shortHash, sha)
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
	if branch == "" || branch == "main" || branch == "master" {
		return false
	}

	// Validate repo format to prevent command injection (should be org/repo format)
	if !validGitRefPattern.MatchString(repo) {
		fmt.Fprintf(os.Stderr, "Warning: invalid repo format '%s'\n", repo)
		return false
	}

	// Validate branch name to prevent command injection
	if !validGitRefPattern.MatchString(branch) {
		fmt.Fprintf(os.Stderr, "Warning: invalid branch format '%s'\n", branch)
		return false
	}

	url := fmt.Sprintf("https://github.com/%s.git", repo)
	cmd := exec.Command("git", "ls-remote", "--heads", url, branch) // #nosec G702 -- inputs validated above
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to check branch '%s' in %s: %v\n", branch, repo, err)
		return false
	}

	return strings.Contains(string(out), fmt.Sprintf("refs/heads/%s", branch))
}

// writeOutput writes the dependency info to GITHUB_OUTPUT
func writeOutput(output *os.File, name string, info *DependencyInfo) {
	// Convert name to output key format (e.g., "build-info-go" -> "build_info_go")
	keyName := strings.ReplaceAll(name, "-", "_")

	var repo, ref string
	if info != nil {
		repo = info.Repo
		ref = info.Ref
	}

	// Write to GITHUB_OUTPUT if available
	if output != nil {
		fmt.Fprintf(output, "%s_repo=%s\n", keyName, repo)
		fmt.Fprintf(output, "%s_ref=%s\n", keyName, ref)
	}

	if info != nil {
		fmt.Printf("  %s_repo=%s\n", keyName, repo)
		fmt.Printf("  %s_ref=%s\n", keyName, ref)
	} else {
		fmt.Printf("  %s: using default\n", keyName)
	}
}
