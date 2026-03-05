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
	"regexp"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

const maxFileSize = 1 << 20 // 1 MB

// versionPattern allows version strings like "3.2", "4.x", "1.0.0-beta", "*".
// Rejects shell metacharacters that could appear in agent-generated build files.
var versionPattern = regexp.MustCompile(`^[0-9a-zA-Z.\-+*x]+$`)

// FrameworkStandard describes a web or application framework.
type FrameworkStandard struct {
	Name       string `json:"name"                 yaml:"name"`
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
	Notes      string `json:"notes,omitempty"      yaml:"notes,omitempty"`
	Reason     string `json:"reason,omitempty"     yaml:"reason,omitempty"` // populated for prohibited entries
}

// LibraryStandard describes a library or package.
type LibraryStandard struct {
	Name       string `json:"name"                 yaml:"name"`
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
	Notes      string `json:"notes,omitempty"      yaml:"notes,omitempty"`
	Category   string `json:"category,omitempty"   yaml:"category,omitempty"`
	Reason     string `json:"reason,omitempty"     yaml:"reason,omitempty"` // populated for prohibited entries
}

// PerLanguageStandards holds language-specific notes, framework, and library standards.
type PerLanguageStandards struct {
	Notes                []string            `json:"notes"                yaml:"notes"`
	ApprovedFrameworks   []FrameworkStandard `json:"approvedFrameworks"   yaml:"approvedFrameworks"`
	ProhibitedFrameworks []FrameworkStandard `json:"prohibitedFrameworks" yaml:"prohibitedFrameworks"`
	ApprovedLibraries    []LibraryStandard   `json:"approvedLibraries"    yaml:"approvedLibraries"`
	ProhibitedLibraries  []LibraryStandard   `json:"prohibitedLibraries"  yaml:"prohibitedLibraries"`
}

// OrgStandards is the full set of coding standards served to agents.
type OrgStandards struct {
	HealthCheckPath string                          `json:"healthCheckPath"  yaml:"healthCheckPath"`
	LoggingFormat   string                          `json:"loggingFormat"    yaml:"loggingFormat"`
	DefaultPort     int                             `json:"defaultPort"      yaml:"defaultPort"`
	EnvVarNaming    string                          `json:"envVarNaming"     yaml:"envVarNaming"`
	RequiredEnvVars []string                        `json:"requiredEnvVars"  yaml:"requiredEnvVars"`
	BestPractices   []string                        `json:"bestPractices"    yaml:"bestPractices"`
	PerLanguage     map[string]PerLanguageStandards `json:"perLanguage"      yaml:"perLanguage"`
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
				Notes: []string{"Use Go modules", "Run as non-root"},
				ApprovedFrameworks: []FrameworkStandard{
					{Name: "net/http", Notes: "Standard library — preferred for simple services"},
					{Name: "gin", MinVersion: "1.9", Notes: "Lightweight HTTP router; use when you need middleware or routing groups"},
				},
				ProhibitedFrameworks: []FrameworkStandard{},
				ApprovedLibraries: []LibraryStandard{
					{Name: "github.com/labstack/echo", MinVersion: "4.0", Category: "web", Notes: "Alternative to gin; acceptable for existing projects"},
					{Name: "database/sql", Category: "database", Notes: "Standard library; always use parameterized queries"},
					{Name: "github.com/jackc/pgx", MinVersion: "5.0", Category: "database", Notes: "Preferred PostgreSQL driver"},
				},
				ProhibitedLibraries: []LibraryStandard{},
			},
			"nodejs": {
				Notes: []string{"Pin exact versions in package-lock.json", "Use node:lts-alpine base"},
				ApprovedFrameworks: []FrameworkStandard{
					{Name: "express", MinVersion: "4.0", Notes: "Standard choice for REST APIs"},
					{Name: "fastify", MinVersion: "4.0", Notes: "Preferred for high-throughput APIs; better TypeScript support"},
					{Name: "next.js", MinVersion: "14.0", Notes: "Required for server-rendered React applications"},
				},
				ProhibitedFrameworks: []FrameworkStandard{},
				ApprovedLibraries: []LibraryStandard{
					{Name: "pg", Category: "database", Notes: "PostgreSQL client; always use parameterized queries"},
					{Name: "sequelize", MinVersion: "6.0", Category: "database", Notes: "ORM — use where clause objects, not raw SQL strings"},
					{Name: "zod", Category: "validation", Notes: "Schema validation for request bodies"},
				},
				ProhibitedLibraries: []LibraryStandard{},
			},
			"python": {
				Notes: []string{"Use requirements.txt or pyproject.toml", "Do not use root user"},
				ApprovedFrameworks: []FrameworkStandard{
					{Name: "fastapi", MinVersion: "0.100", Notes: "Preferred for new APIs; async-first with automatic OpenAPI docs"},
					{Name: "flask", MinVersion: "3.0", Notes: "Acceptable for simple services; use application factory pattern"},
					{Name: "django", MinVersion: "5.0", Notes: "Preferred for full-stack applications with ORM"},
				},
				ProhibitedFrameworks: []FrameworkStandard{},
				ApprovedLibraries: []LibraryStandard{
					{Name: "psycopg2-binary", Category: "database", Notes: "PostgreSQL adapter; always use %s parameterized queries"},
					{Name: "sqlalchemy", MinVersion: "2.0", Category: "database", Notes: "ORM — use text() with named bind parameters"},
					{Name: "pydantic", MinVersion: "2.0", Category: "validation", Notes: "Required for FastAPI; also use for config validation"},
				},
				ProhibitedLibraries: []LibraryStandard{},
			},
			"java": {
				Notes: []string{"Use Gradle or Maven wrapper scripts"},
				ApprovedFrameworks: []FrameworkStandard{
					{Name: "spring-boot", MinVersion: "3.2", Notes: "Standard choice; use Spring Boot 3.x (Jakarta EE namespace)"},
					{Name: "quarkus", MinVersion: "3.0", Notes: "Approved for new microservices; native compilation supported"},
				},
				ProhibitedFrameworks: []FrameworkStandard{
					{Name: "spring-mvc-standalone", Reason: "Use Spring Boot instead — standalone Spring MVC lacks auto-configuration and actuator"},
					{Name: "spring-boot-2.x", Reason: "Spring Boot 2.x reached EOL; migrate to Spring Boot 3.x (requires Java 17+)"},
				},
				ApprovedLibraries: []LibraryStandard{
					{Name: "spring-data-jpa", Category: "database", Notes: "Preferred ORM; use @Query with named parameters"},
					{Name: "liquibase", Category: "database", Notes: "Required for schema migrations"},
				},
				ProhibitedLibraries: []LibraryStandard{},
			},
			"ruby": {
				Notes: []string{"Include a Gemfile.lock"},
				ApprovedFrameworks: []FrameworkStandard{
					{Name: "rails", MinVersion: "7.1", Notes: "Standard choice for full-stack applications"},
					{Name: "sinatra", MinVersion: "3.0", Notes: "Approved for lightweight APIs and microservices"},
				},
				ProhibitedFrameworks: []FrameworkStandard{},
				ApprovedLibraries: []LibraryStandard{
					{Name: "pg", Category: "database", Notes: "PostgreSQL adapter; use ActiveRecord parameterized queries"},
					{Name: "devise", MinVersion: "4.9", Category: "auth", Notes: "Preferred authentication library for Rails"},
					{Name: "sidekiq", MinVersion: "7.0", Category: "background-jobs", Notes: "Standard background job processor"},
				},
				ProhibitedLibraries: []LibraryStandard{},
			},
		},
	}
}

// validateVersionString returns true if s is a valid version string or empty.
func validateVersionString(s string) bool {
	if s == "" {
		return true
	}
	return versionPattern.MatchString(s)
}

// sanitizePerLanguage validates version strings in all framework and library entries.
// Entries with invalid versions are removed and a warning is logged.
func sanitizePerLanguage(langs map[string]PerLanguageStandards, logger *slog.Logger) map[string]PerLanguageStandards {
	out := make(map[string]PerLanguageStandards, len(langs))
	for lang, std := range langs {
		std.ApprovedFrameworks = filterFrameworks(std.ApprovedFrameworks, lang, logger)
		std.ProhibitedFrameworks = filterFrameworks(std.ProhibitedFrameworks, lang, logger)
		std.ApprovedLibraries = filterLibraries(std.ApprovedLibraries, lang, logger)
		std.ProhibitedLibraries = filterLibraries(std.ProhibitedLibraries, lang, logger)
		out[lang] = std
	}
	return out
}

func filterFrameworks(fs []FrameworkStandard, lang string, logger *slog.Logger) []FrameworkStandard {
	out := fs[:0:0]
	for _, f := range fs {
		if !validateVersionString(f.MinVersion) {
			logger.Warn("orgstandards: invalid version string in framework — entry skipped",
				"lang", lang, "framework", f.Name, "minVersion", f.MinVersion)
			continue
		}
		out = append(out, f)
	}
	return out
}

func filterLibraries(ls []LibraryStandard, lang string, logger *slog.Logger) []LibraryStandard {
	out := ls[:0:0]
	for _, l := range ls {
		if !validateVersionString(l.MinVersion) {
			logger.Warn("orgstandards: invalid version string in library — entry skipped",
				"lang", lang, "library", l.Name, "minVersion", l.MinVersion)
			continue
		}
		out = append(out, l)
	}
	return out
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

	// Validate and sanitize version strings to prevent injection.
	standards.PerLanguage = sanitizePerLanguage(standards.PerLanguage, l.logger)

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
