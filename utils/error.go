package utils

import (
	"strings"
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

// IsForbiddenOutput verify the output is forbidden, each tech have its own forbidden output.
func IsForbiddenOutput(tech string, cmdOutput string) bool {
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
