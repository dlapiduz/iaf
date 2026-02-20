// Package orgstandards loads and serves organisation coding standards.
// Standards are read from a YAML or JSON file at startup and hot-reloaded
// via fsnotify whenever the file changes.  If no file is configured or the
// file cannot be read, compile-time platform defaults are served instead.
package orgstandards

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

const maxFileSize = 1 << 20 // 1 MB

// PerLanguageStandards holds language-specific notes and library recommendations.
type PerLanguageStandards struct {
	Notes            []string          `json:"notes"            yaml:"notes"`
	ApprovedLibraries map[string]string `json:"approvedLibraries" yaml:"approvedLibraries"`
}

// OrgStandards is the full set of coding standards served to agents.
type OrgStandards struct {
	HealthCheckPath  string                          `json:"healthCheckPath"  yaml:"healthCheckPath"`
	LoggingFormat    string                          `json:"loggingFormat"    yaml:"loggingFormat"`
	DefaultPort      int                             `json:"defaultPort"      yaml:"defaultPort"`
	EnvVarNaming     string                          `json:"envVarNaming"     yaml:"envVarNaming"`
	RequiredEnvVars  []string                        `json:"requiredEnvVars"  yaml:"requiredEnvVars"`
	BestPractices    []string                        `json:"bestPractices"    yaml:"bestPractices"`
	PerLanguage      map[string]PerLanguageStandards `json:"perLanguage"      yaml:"perLanguage"`
}

// platformDefaults returns the built-in standards used when no config file is set.
func platformDefaults() *OrgStandards {
	return &OrgStandards{
		HealthCheckPath: "/health",
		LoggingFormat:   "json",
		DefaultPort:     8080,
		EnvVarNaming:    "UPPER_SNAKE_CASE",
		RequiredEnvVars: []string{},
		BestPractices: []string{
			"Listen on the PORT environment variable (default 8080)",
			"Implement a health-check endpoint at /health returning HTTP 200",
			"Do not embed secrets in source code — use environment variables",
			"Emit structured JSON logs to stdout",
			"Handle SIGTERM for graceful shutdown",
		},
		PerLanguage: map[string]PerLanguageStandards{
			"go": {
				Notes:             []string{"Use Go modules", "Run as non-root"},
				ApprovedLibraries: map[string]string{"web": "net/http or github.com/labstack/echo"},
			},
			"nodejs": {
				Notes:             []string{"Pin exact versions in package-lock.json", "Use node:lts-alpine base"},
				ApprovedLibraries: map[string]string{"web": "express", "frontend": "next.js"},
			},
			"python": {
				Notes:             []string{"Use requirements.txt or pyproject.toml", "Do not use root user"},
				ApprovedLibraries: map[string]string{"web": "flask or fastapi"},
			},
			"java": {
				Notes:             []string{"Use Gradle or Maven wrapper scripts"},
				ApprovedLibraries: map[string]string{"web": "spring-boot"},
			},
			"ruby": {
				Notes:             []string{"Include a Gemfile.lock"},
				ApprovedLibraries: map[string]string{"web": "rails or sinatra"},
			},
		},
	}
}

// Loader reads org standards from a file and provides a goroutine-safe Get().
type Loader struct {
	path    string
	mu      sync.RWMutex
	current *OrgStandards
	logger  *slog.Logger
}

// New creates a Loader.  path may be empty, in which case platform defaults
// are always returned.  Call Start to begin watching the file for changes.
func New(path string, logger *slog.Logger) *Loader {
	if logger == nil {
		logger = slog.Default()
	}
	l := &Loader{path: path, logger: logger}
	l.current = l.load()
	return l
}

// Get returns the current standards.  Safe for concurrent use.
func (l *Loader) Get() *OrgStandards {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Start begins watching the config file for changes.  It blocks until ctx is
// cancelled.  Safe to call in a goroutine.
func (l *Loader) Start(ctx context.Context) {
	if l.path == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		l.logger.Warn("orgstandards: cannot create file watcher", "error", err)
		return
	}
	defer watcher.Close()

	// Watch the directory so renames/atomic writes are detected.
	dir := filepath.Dir(l.path)
	if err := watcher.Add(dir); err != nil {
		l.logger.Warn("orgstandards: cannot watch directory", "dir", dir, "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name != l.path {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				updated := l.load()
				l.mu.Lock()
				l.current = updated
				l.mu.Unlock()
				l.logger.Info("orgstandards: reloaded", "path", l.path)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			l.logger.Warn("orgstandards: watcher error", "error", err)
		}
	}
}

// load reads and parses the config file, returning platform defaults on any error.
func (l *Loader) load() *OrgStandards {
	if l.path == "" {
		return platformDefaults()
	}

	// Defence-in-depth: reject paths with traversal sequences.
	clean := filepath.Clean(l.path)
	if strings.Contains(clean, "..") {
		l.logger.Warn("orgstandards: rejecting path with traversal", "path", l.path)
		return platformDefaults()
	}

	f, err := os.Open(clean)
	if err != nil {
		if !os.IsNotExist(err) {
			l.logger.Warn("orgstandards: cannot open file", "path", clean, "error", err)
		}
		return platformDefaults()
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxFileSize))
	if err != nil {
		l.logger.Warn("orgstandards: read error", "path", clean, "error", err)
		return platformDefaults()
	}

	var standards OrgStandards
	ext := strings.ToLower(filepath.Ext(clean))
	if ext == ".json" {
		err = json.Unmarshal(data, &standards)
	} else {
		// Default: YAML (also accepts .yaml, .yml, anything else).
		err = yaml.Unmarshal(data, &standards)
	}
	if err != nil {
		l.logger.Warn("orgstandards: parse error — retaining previous", "path", clean, "error", err)
		return l.Get() // retain current value on parse failure
	}

	// Ensure slices are non-nil for clean JSON output.
	if standards.RequiredEnvVars == nil {
		standards.RequiredEnvVars = []string{}
	}
	if standards.BestPractices == nil {
		standards.BestPractices = []string{}
	}
	if standards.PerLanguage == nil {
		standards.PerLanguage = map[string]PerLanguageStandards{}
	}

	l.logger.Info("orgstandards: loaded", "path", clean)
	return &standards

}

// validatePath returns a descriptive error if path contains traversal sequences.
func validatePath(path string) error {
	if strings.Contains(filepath.Clean(path), "..") {
		return fmt.Errorf("org standards path must not contain traversal sequences")
	}
	return nil
}

// ValidatePath is exported for use in config validation.
var ValidatePath = validatePath
