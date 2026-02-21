package validation

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	appNameRegex       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	envVarNameRegex    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	githubRepoRegex    = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

	reservedPrefixes = []string{"kube-", "iaf-"}

	// RFC 1918 private address ranges
	privateRanges = []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
	}
)

// ValidateAppName validates that name is a valid Kubernetes DNS label suitable
// for use as an application name. Returns a descriptive error if invalid.
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("app name is required")
	}
	if len(name) > 63 {
		return fmt.Errorf("app name must be 63 characters or less (got %d)", len(name))
	}
	if !appNameRegex.MatchString(name) {
		return fmt.Errorf("app name %q is invalid: must match ^[a-z0-9][a-z0-9-]*$ (lowercase letters, digits, and hyphens only; must start with a letter or digit)", name)
	}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("app name %q is invalid: must not use reserved prefix %q", name, prefix)
		}
	}
	return nil
}

// ValidateBasicAuthGitServerURL validates a git server URL for basic-auth (HTTPS).
// Rejects internal/RFC 1918 addresses to prevent SSRF.
func ValidateBasicAuthGitServerURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("git_server_url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("git_server_url %q is not a valid URL: %w", rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("git_server_url %q must use https:// (http is not allowed — credentials would be sent in plaintext)", rawURL)
	}
	if err := rejectPrivateHost(u.Hostname()); err != nil {
		return fmt.Errorf("git_server_url %q: %w", rawURL, err)
	}
	return nil
}

// ValidateSSHGitServerURL validates a git server URL for SSH (git@ format).
// Rejects internal/RFC 1918 addresses to prevent SSRF.
func ValidateSSHGitServerURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("git_server_url is required")
	}
	// Accept git@host or git@host:path format
	if strings.HasPrefix(rawURL, "git@") {
		hostPart := rawURL[4:]
		// Strip optional :path suffix
		if idx := strings.Index(hostPart, ":"); idx >= 0 {
			hostPart = hostPart[:idx]
		}
		if hostPart == "" {
			return fmt.Errorf("git_server_url %q is not a valid SSH git URL (expected git@host or git@host:path)", rawURL)
		}
		if err := rejectPrivateHost(hostPart); err != nil {
			return fmt.Errorf("git_server_url %q: %w", rawURL, err)
		}
		return nil
	}
	// Also accept ssh:// URLs
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("git_server_url %q is not a valid URL: %w", rawURL, err)
	}
	if u.Scheme != "ssh" {
		return fmt.Errorf("git_server_url %q must use git@host:path or ssh:// format", rawURL)
	}
	if err := rejectPrivateHost(u.Hostname()); err != nil {
		return fmt.Errorf("git_server_url %q: %w", rawURL, err)
	}
	return nil
}

// ValidateGitHubRepoName validates a GitHub repository name (not org/repo — just the repo part).
func ValidateGitHubRepoName(name string) error {
	if name == "" {
		return fmt.Errorf("repo name is required")
	}
	if len(name) > 100 {
		return fmt.Errorf("repo name must be 100 characters or less")
	}
	if !githubRepoRegex.MatchString(name) {
		return fmt.Errorf("repo name %q is invalid: must contain only letters, digits, hyphens, underscores, and dots", name)
	}
	return nil
}

// rejectPrivateHost returns an error if the hostname resolves to a private/internal IP.
func rejectPrivateHost(host string) error {
	// Parse private CIDR ranges once; ignore parse errors (they won't happen for hardcoded values)
	var privateNets []*net.IPNet
	for _, cidr := range privateRanges {
		_, ipNet, _ := net.ParseCIDR(cidr)
		if ipNet != nil {
			privateNets = append(privateNets, ipNet)
		}
	}

	// If host is a raw IP, check it directly
	if ip := net.ParseIP(host); ip != nil {
		for _, pNet := range privateNets {
			if pNet.Contains(ip) {
				return fmt.Errorf("host %q is a private/internal address and is not allowed", host)
			}
		}
		return nil
	}

	// Resolve hostname and check each address
	addrs, err := net.LookupHost(host)
	if err != nil {
		// If resolution fails, we can't confirm it's safe — reject it
		return fmt.Errorf("host %q could not be resolved: %w", host, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		for _, pNet := range privateNets {
			if pNet.Contains(ip) {
				return fmt.Errorf("host %q resolves to private/internal address %s and is not allowed", host, addr)
			}
		}
	}
	return nil
}

// ValidateEnvVarName validates that name is a valid environment variable name.
// Returns a descriptive error if invalid.
func ValidateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("env var name is required")
	}
	if !envVarNameRegex.MatchString(name) {
		return fmt.Errorf("env var name %q is invalid: must match ^[A-Za-z_][A-Za-z0-9_]*$ (letters, digits, and underscores only; must start with a letter or underscore)", name)
	}
	return nil
}
