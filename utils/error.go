package utils

import (
	"fmt"
	"strings"

	"github.com/jfrog/gofrog/log"
)

type PackageManager string

const (
	Npm    PackageManager = "npm"
	Maven  PackageManager = "maven"
	Pip    PackageManager = "pip"
	Go     PackageManager = "go"
	Poetry PackageManager = "poetry"
	Yarn   PackageManager = "yarn"
)

// ForbiddenError represents a 403 Forbidden error.
type ForbiddenError struct {
	Message string
}

// Error implements the error interface for ForbiddenError.
func (e *ForbiddenError) Error() string {
	return "403 Forbidden"
}

// NewForbiddenError creates a new ForbiddenError with the given message.
func NewForbiddenError() *ForbiddenError {
	return &ForbiddenError{}
}

type ErrProjectNotInstalled struct {
	UninstalledDir string
}

func (err *ErrProjectNotInstalled) Error() string {
	return fmt.Sprintf("Directory '%s' is not installed. Skipping SCA scan in this directory...", err.UninstalledDir)
}

// IsForbiddenOutput checks whether the provided output includes a 403 Forbidden. The various package managers have their own forbidden output formats.
func IsForbiddenOutput(tech PackageManager, cmdOutput string) bool {
	log.Debug("Checking forbidden output for package manager:", tech)
	switch tech {
	case "npm", "yarn":
		return strings.Contains(strings.ToLower(cmdOutput), "403 forbidden")
	case "maven":
		return strings.Contains(cmdOutput, "status code: 403") ||
			strings.Contains(strings.ToLower(cmdOutput), "403 forbidden") ||
			// In some cases mvn returns 500 status code even though it got 403 from artifactory.
			strings.Contains(cmdOutput, "status code: 500")
	case "pip":
		return strings.Contains(strings.ToLower(cmdOutput), "http error 403")
	case "go":
		return strings.Contains(strings.ToLower(cmdOutput), "403 forbidden") ||
			strings.Contains(strings.ToLower(cmdOutput), " 403")
	case "poetry":
		lower := strings.ToLower(cmdOutput)
		switch {
		case strings.Contains(lower, "http error 403"):
			log.Debug("Poetry forbidden output matched pattern: 'http error 403'")
			return true
		case strings.Contains(lower, "403 client error"):
			log.Debug("Poetry forbidden output matched pattern: '403 client error'")
			return true
		case strings.Contains(lower, "403 forbidden"):
			log.Debug("Poetry forbidden output matched pattern: '403 forbidden'")
			return true
		}
	}
	return false
}
