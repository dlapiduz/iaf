// Package validation provides input-validation helpers for IAF.
package validation

import (
	"fmt"
	"net"
	"net/url"
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

	// sshGitURLRe matches SSH git server URLs in the form git@<host>.
	// Host may contain alphanumerics, dots, and hyphens.
	sshGitURLRe = regexp.MustCompile(`^git@[a-zA-Z0-9][a-zA-Z0-9.\-]*$`)

	// internalCIDRs is the set of IP ranges that must never be used as git servers
	// to prevent SSRF via kpack build-time git cloning.
	internalCIDRs = func() []*net.IPNet {
		ranges := []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"127.0.0.0/8",
			"169.254.0.0/16",
			"::1/128",
			"fc00::/7",
		}
		nets := make([]*net.IPNet, 0, len(ranges))
		for _, r := range ranges {
			_, n, _ := net.ParseCIDR(r)
			nets = append(nets, n)
		}
		return nets
	}()
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

// ValidateBasicAuthGitServerURL validates a git server URL for basic-auth credentials.
// The URL must use https:// and must not point to an RFC 1918 or loopback address.
func ValidateBasicAuthGitServerURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("git_server_url must not be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("git_server_url %q is not a valid URL: %w", rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("git_server_url must use https:// scheme (got %q); http:// is not allowed", u.Scheme)
	}
	return rejectInternalHost(u.Hostname())
}

// ValidateSSHGitServerURL validates a git server URL for SSH credentials.
// The URL must match the git@<host> pattern and must not point to an internal host.
func ValidateSSHGitServerURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("git_server_url must not be empty")
	}
	if !sshGitURLRe.MatchString(rawURL) {
		return fmt.Errorf("git_server_url for ssh must match git@<host> pattern (e.g. git@github.com), got %q", rawURL)
	}
	at := strings.Index(rawURL, "@")
	host := rawURL[at+1:]
	return rejectInternalHost(host)
}

// rejectInternalHost returns an error if host is localhost, a loopback address,
// an RFC 1918 address, or a link-local address — all of which must not be used
// as git servers to prevent SSRF via kpack build-time cloning.
func rejectInternalHost(host string) error {
	if strings.ToLower(host) == "localhost" {
		return fmt.Errorf("git_server_url must not point to a local or internal host")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil // hostname (not an IP) — allow
	}
	for _, network := range internalCIDRs {
		if network.Contains(ip) {
			return fmt.Errorf("git_server_url must not point to an internal IP address (%s)", host)
		}
	}
	return nil
}
