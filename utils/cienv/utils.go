package cienv

import "os"

// CIEnvIndicators are environment variables set by common CI providers.
// Presence of any of them indicates the build is running inside a CI pipeline.
var CIEnvIndicators = []string{
	"JENKINS_URL",    // Jenkins (does not set CI=true by default)
	"GITHUB_ACTIONS", // GitHub Actions
	"GITLAB_CI",      // GitLab CI
	"TF_BUILD",       // Azure Pipelines
	"CIRCLECI",       // CircleCI
}

// IsCIRunning reports whether the process is running inside a CI environment.
// CI is checked for the exact value "true" (the universal standard); provider-specific
// variables are checked for mere presence because their values vary (e.g. JENKINS_URL is a URL).
func IsCIRunning() bool {
	if os.Getenv(CIEnvVar) == "true" {
		return true
	}
	for _, envVar := range CIEnvIndicators {
		if os.Getenv(envVar) != "" {
			return true
		}
	}
	return false
}
