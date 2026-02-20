// Package validation provides input-validation helpers for IAF.
package validation

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// appNameRe matches valid Kubernetes / DNS label names:
	// lowercase alphanumeric and hyphens, must start with alphanumeric.
	appNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

	// envVarRe matches valid POSIX / shell environment variable names.
	envVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// githubRepoRe matches valid GitHub repository names.
	githubRepoRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

// ValidateAppName returns an error if name is not a valid Kubernetes DNS label.
// Rules: 1–63 characters, lowercase alphanumeric and hyphens, must start with
// an alphanumeric character. The reserved prefixes "kube-" and "iaf-" are
// also rejected to prevent conflicts with platform-managed objects.
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("app name must not be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("app name must be 63 characters or fewer (got %d)", len(name))
	}
	if !appNameRe.MatchString(name) {
		return fmt.Errorf("app name %q is invalid: must be lowercase alphanumeric and hyphens, starting with a letter or digit", name)
	}
	if strings.HasPrefix(name, "kube-") {
		return fmt.Errorf("app name must not start with the reserved prefix 'kube-'")
	}
	if strings.HasPrefix(name, "iaf-") {
		return fmt.Errorf("app name must not start with the reserved prefix 'iaf-'")
	}
	return nil
}

// ValidateEnvVarName returns an error if name is not a valid environment
// variable name (POSIX: letter or underscore, followed by letters, digits,
// or underscores).
func ValidateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("env var name must not be empty")
	}
	if !envVarRe.MatchString(name) {
		return fmt.Errorf("env var name %q is invalid: must start with a letter or underscore and contain only letters, digits, and underscores", name)
	}
	return nil
}

// ValidateGitHubRepoName returns an error if name is not a valid GitHub
// repository name. Rules: 1–100 characters, alphanumeric plus `.`, `_`, `-`;
// must not start or end with `.` or `-`; must not contain `..`.
func ValidateGitHubRepoName(name string) error {
	if name == "" {
		return fmt.Errorf("repository name must not be empty")
	}
	if len(name) > 100 {
		return fmt.Errorf("repository name must be 100 characters or fewer (got %d)", len(name))
	}
	if !githubRepoRe.MatchString(name) {
		return fmt.Errorf("repository name %q is invalid: must contain only letters, digits, '.', '_', or '-'", name)
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-") {
		return fmt.Errorf("repository name %q must not start with '.' or '-'", name)
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("repository name %q must not end with '.' or '-'", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("repository name %q must not contain '..'", name)
	}
	return nil
}
