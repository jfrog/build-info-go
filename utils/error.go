package utils

import (
	"fmt"
	"strings"
)

type PackageManager string

const (
	Npm   PackageManager = "npm"
	Maven PackageManager = "maven"
	Pip   PackageManager = "pip"
	Go    PackageManager = "go"
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
	switch tech {
	case "npm":
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
	}
	return false
}
