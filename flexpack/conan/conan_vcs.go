package conan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// collectVcsInfo collects Git VCS information from the project directory
func (cf *ConanFlexPack) collectVcsInfo() *entities.Vcs {
	workDir := cf.findGitRoot()
	if workDir == "" {
		return nil
	}

	vcs := cf.extractGitInfo(workDir)
	if vcs.Revision == "" {
		return nil
	}

	log.Debug(fmt.Sprintf("Collected VCS info: url=%s, branch=%s, revision=%s", vcs.Url, vcs.Branch, vcs.Revision))
	return vcs
}

// findGitRoot finds the git repository root directory
func (cf *ConanFlexPack) findGitRoot() string {
	workDir := cf.config.WorkingDirectory

	// Check if current directory is a git repository
	gitDir := filepath.Join(workDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return workDir
	}

	// Try parent directories up to 5 levels
	currentDir := workDir
	for i := 0; i < 5; i++ {
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}

		gitDir = filepath.Join(parentDir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return parentDir
		}
		currentDir = parentDir
	}

	log.Debug("Not a git repository, skipping VCS info collection")
	return ""
}

// extractGitInfo extracts Git information from the repository
func (cf *ConanFlexPack) extractGitInfo(workDir string) *entities.Vcs {
	vcs := &entities.Vcs{}

	vcs.Url = cf.runGitCommand(workDir, "config", "--get", "remote.origin.url")
	vcs.Revision = cf.runGitCommand(workDir, "rev-parse", "HEAD")
	vcs.Branch = cf.runGitCommand(workDir, "rev-parse", "--abbrev-ref", "HEAD")
	vcs.Message = cf.runGitCommand(workDir, "log", "-1", "--pretty=%B")

	return vcs
}

// runGitCommand runs a git command and returns trimmed output
func (cf *ConanFlexPack) runGitCommand(workDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}
