package orgstandards_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dlapiduz/iaf/internal/orgstandards"
)

func TestLoader_NoPath_ReturnsDefaults(t *testing.T) {
	l := orgstandards.New("", slog.Default())
	s := l.Get()
	if s == nil {
		t.Fatal("expected non-nil standards")
	}
	if s.DefaultPort != 8080 {
		t.Errorf("expected default port 8080, got %d", s.DefaultPort)
	}
	if len(s.BestPractices) == 0 {
		t.Error("expected non-empty BestPractices in defaults")
	}
	if _, ok := s.PerLanguage["go"]; !ok {
		t.Error("expected 'go' in default PerLanguage")
	}
}

func TestLoader_MissingFile_ReturnsDefaults(t *testing.T) {
	l := orgstandards.New("/nonexistent/path/standards.yaml", slog.Default())
	s := l.Get()
	if s == nil {
		t.Fatal("expected non-nil standards (defaults)")
	}
	if s.DefaultPort != 8080 {
		t.Errorf("expected default port 8080, got %d", s.DefaultPort)
	}
}

func TestLoader_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "standards.yaml")

	content := `
healthCheckPath: /healthz
loggingFormat: text
defaultPort: 3000
envVarNaming: UPPER_SNAKE_CASE
bestPractices:
  - "Use environment variables for config"
perLanguage:
  go:
    notes:
      - "Use Go modules"
    approvedFrameworks:
      - name: gin
        minVersion: "1.9"
        notes: "Preferred HTTP router"
    approvedLibraries:
      - name: pgx
        category: database
        minVersion: "5.0"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l := orgstandards.New(path, slog.Default())
	s := l.Get()

	if s.HealthCheckPath != "/healthz" {
		t.Errorf("expected healthCheckPath=/healthz, got %q", s.HealthCheckPath)
	}
	if s.DefaultPort != 3000 {
		t.Errorf("expected defaultPort=3000, got %d", s.DefaultPort)
	}
	if len(s.BestPractices) != 1 {
		t.Errorf("expected 1 best practice, got %d", len(s.BestPractices))
	}
	goStd, ok := s.PerLanguage["go"]
	if !ok {
		t.Fatal("expected 'go' in PerLanguage")
	}
	if len(goStd.ApprovedFrameworks) != 1 || goStd.ApprovedFrameworks[0].Name != "gin" {
		t.Errorf("expected approvedFrameworks[0].name=gin, got %v", goStd.ApprovedFrameworks)
	}
	if len(goStd.ApprovedLibraries) != 1 || goStd.ApprovedLibraries[0].Name != "pgx" {
		t.Errorf("expected approvedLibraries[0].name=pgx, got %v", goStd.ApprovedLibraries)
	}
}

func TestLoader_VersionValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "standards.yaml")

	// One valid entry, one with a shell-metacharacter version — the bad one should be dropped.
	content := `
perLanguage:
  go:
    approvedFrameworks:
      - name: gin
        minVersion: "1.9"
      - name: evil
        minVersion: "$(rm -rf /)"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l := orgstandards.New(path, slog.Default())
	s := l.Get()

	goStd := s.PerLanguage["go"]
	if len(goStd.ApprovedFrameworks) != 1 {
		t.Fatalf("expected 1 approved framework after validation, got %d: %v", len(goStd.ApprovedFrameworks), goStd.ApprovedFrameworks)
	}
	if goStd.ApprovedFrameworks[0].Name != "gin" {
		t.Errorf("expected 'gin' to survive version validation, got %q", goStd.ApprovedFrameworks[0].Name)
	}
}

func TestLoader_DefaultsHaveFrameworks(t *testing.T) {
	l := orgstandards.New("", slog.Default())
	s := l.Get()

	for _, lang := range []string{"go", "nodejs", "python", "java", "ruby"} {
		std, ok := s.PerLanguage[lang]
		if !ok {
			t.Errorf("expected language %q in defaults", lang)
			continue
		}
		if len(std.ApprovedFrameworks) == 0 {
			t.Errorf("expected non-empty ApprovedFrameworks for %q", lang)
		}
	}

	// Java should have prohibited frameworks.
	javaStd := s.PerLanguage["java"]
	if len(javaStd.ProhibitedFrameworks) == 0 {
		t.Error("expected non-empty ProhibitedFrameworks for java")
	}
}

func TestLoader_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "standards.json")

	content := `{"healthCheckPath":"/ping","defaultPort":9000,"bestPractices":["test"],"perLanguage":{},"requiredEnvVars":[]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l := orgstandards.New(path, slog.Default())
	s := l.Get()

	if s.HealthCheckPath != "/ping" {
		t.Errorf("expected healthCheckPath=/ping, got %q", s.HealthCheckPath)
	}
	if s.DefaultPort != 9000 {
		t.Errorf("expected defaultPort=9000, got %d", s.DefaultPort)
	}
}

func TestLoader_HotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "standards.yaml")

	// Write initial file.
	if err := os.WriteFile(path, []byte("defaultPort: 1111\nbestPractices: []\nrequiredEnvVars: []\nperLanguage: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := orgstandards.New(path, slog.Default())
	if l.Get().DefaultPort != 1111 {
		t.Fatalf("initial load: expected port 1111, got %d", l.Get().DefaultPort)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Start(ctx)

	// Give the watcher time to set up.
	time.Sleep(50 * time.Millisecond)

	// Update the file.
	if err := os.WriteFile(path, []byte("defaultPort: 2222\nbestPractices: []\nrequiredEnvVars: []\nperLanguage: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for hot-reload (up to 2 seconds).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if l.Get().DefaultPort == 2222 {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("hot-reload: expected port 2222 after file update, got %d", l.Get().DefaultPort)
}

func TestLoader_InvalidYAML_RetainsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "standards.yaml")

	if err := os.WriteFile(path, []byte(":::invalid yaml:::\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should not panic; returns defaults.
	l := orgstandards.New(path, slog.Default())
	s := l.Get()
	if s == nil {
		t.Fatal("expected non-nil result even for invalid YAML")
	}
}
