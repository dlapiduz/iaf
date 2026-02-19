package validation

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// appNameRe matches valid DNS label app names: lowercase alphanumeric and hyphens,
	// must start with a lowercase alphanumeric character.
	appNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

	// envVarRe matches valid POSIX environment variable names.
	envVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ValidateAppName checks that name is a valid IAF application name:
//   - non-empty
//   - matches ^[a-z0-9][a-z0-9-]*$ (lowercase alphanumeric and hyphens, starting with alphanumeric)
//   - 63 characters or fewer
//   - does not start with the reserved prefixes "kube-" or "iaf-"
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("app name is required")
	}
	if len(name) > 63 {
		return fmt.Errorf("app name must match ^[a-z0-9][a-z0-9-]*$ and be 63 characters or less")
	}
	if !appNameRe.MatchString(name) {
		return fmt.Errorf("app name must match ^[a-z0-9][a-z0-9-]*$ and be 63 characters or less")
	}
	if strings.HasPrefix(name, "kube-") {
		return fmt.Errorf("app name must not use reserved prefix \"kube-\"")
	}
	if strings.HasPrefix(name, "iaf-") {
		return fmt.Errorf("app name must not use reserved prefix \"iaf-\"")
	}
	return nil
}

// ValidateEnvVarName checks that name is a valid environment variable name:
//   - non-empty
//   - matches ^[A-Za-z_][A-Za-z0-9_]*$ (letter/underscore start, alphanumeric/underscore body)
func ValidateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("env var name is required")
	}
	if !envVarRe.MatchString(name) {
		return fmt.Errorf("env var name %q must match ^[A-Za-z_][A-Za-z0-9_]*$ (start with letter or underscore, followed by letters, digits, or underscores)", name)
	}
	return nil
}
