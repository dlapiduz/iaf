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
    approvedLibraries:
      web: "echo"
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
	if goStd.ApprovedLibraries["web"] != "echo" {
		t.Errorf("expected approvedLibraries.web=echo, got %q", goStd.ApprovedLibraries["web"])
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
