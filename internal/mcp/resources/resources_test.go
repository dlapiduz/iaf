package resources_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dlapiduz/iaf/internal/mcp/resources"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	deps := &tools.Dependencies{
		BaseDomain: "test.example.com",
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	resources.RegisterPlatformInfo(server, deps)
	resources.RegisterLanguageResources(server, deps)
	resources.RegisterApplicationSpec(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestPlatformInfo(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://platform",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}

	content := res.Contents[0]
	if content.URI != "iaf://platform" {
		t.Errorf("expected URI 'iaf://platform', got %q", content.URI)
	}
	if content.MIMEType != "application/json" {
		t.Errorf("expected MIME type 'application/json', got %q", content.MIMEType)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(content.Text), &info); err != nil {
		t.Fatalf("failed to parse platform info JSON: %v", err)
	}

	if info["namespace"] == nil || info["namespace"] == "" {
		t.Error("expected namespace info to be present")
	}
	if info["baseDomain"] != "test.example.com" {
		t.Errorf("expected baseDomain 'test.example.com', got %v", info["baseDomain"])
	}

	langs, ok := info["supportedLanguages"].([]any)
	if !ok {
		t.Fatal("expected supportedLanguages to be an array")
	}
	if len(langs) != 5 {
		t.Errorf("expected 5 supported languages, got %d", len(langs))
	}

	defaults, ok := info["defaults"].(map[string]any)
	if !ok {
		t.Fatal("expected defaults to be an object")
	}
	if defaults["port"] != float64(8080) {
		t.Errorf("expected default port 8080, got %v", defaults["port"])
	}
}

func TestLanguageResources_AllLanguages(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	languages := []string{"go", "nodejs", "python", "java", "ruby"}
	for _, lang := range languages {
		t.Run(lang, func(t *testing.T) {
			res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
				URI: "iaf://languages/" + lang,
			})
			if err != nil {
				t.Fatal(err)
			}

			if len(res.Contents) != 1 {
				t.Fatalf("expected 1 content, got %d", len(res.Contents))
			}

			var spec map[string]any
			if err := json.Unmarshal([]byte(res.Contents[0].Text), &spec); err != nil {
				t.Fatalf("failed to parse language spec JSON: %v", err)
			}

			if spec["language"] != lang {
				t.Errorf("expected language %q, got %v", lang, spec["language"])
			}
			if _, ok := spec["buildpackId"]; !ok {
				t.Error("expected buildpackId field")
			}
			if _, ok := spec["detectionFiles"]; !ok {
				t.Error("expected detectionFiles field")
			}
			if _, ok := spec["requiredFiles"]; !ok {
				t.Error("expected requiredFiles field")
			}
			if _, ok := spec["envVars"]; !ok {
				t.Error("expected envVars field")
			}
		})
	}
}

func TestLanguageResources_Aliases(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	aliases := map[string]string{
		"golang": "go",
		"node":   "nodejs",
		"py":     "python",
		"rb":     "ruby",
	}

	for alias, canonical := range aliases {
		t.Run(alias, func(t *testing.T) {
			res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
				URI: "iaf://languages/" + alias,
			})
			if err != nil {
				t.Fatal(err)
			}

			var spec map[string]any
			if err := json.Unmarshal([]byte(res.Contents[0].Text), &spec); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}

			if spec["language"] != canonical {
				t.Errorf("alias %q: expected language %q, got %v", alias, canonical, spec["language"])
			}
		})
	}
}

func TestLanguageResources_NotFound(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	_, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://languages/cobol",
	})
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
}

func TestApplicationSpec(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://schema/application",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}

	content := res.Contents[0]
	if content.MIMEType != "application/json" {
		t.Errorf("expected MIME type 'application/json', got %q", content.MIMEType)
	}

	var spec map[string]any
	if err := json.Unmarshal([]byte(content.Text), &spec); err != nil {
		t.Fatalf("failed to parse application spec JSON: %v", err)
	}

	if spec["apiVersion"] != "iaf.io/v1alpha1" {
		t.Errorf("expected apiVersion 'iaf.io/v1alpha1', got %v", spec["apiVersion"])
	}
	if spec["kind"] != "Application" {
		t.Errorf("expected kind 'Application', got %v", spec["kind"])
	}

	specFields, ok := spec["spec"].(map[string]any)
	if !ok {
		t.Fatal("expected spec to be an object")
	}
	for _, field := range []string{"image", "git", "blob", "port", "replicas", "env", "host"} {
		if _, ok := specFields[field]; !ok {
			t.Errorf("expected spec field %q", field)
		}
	}

	// Verify dynamic values
	hostField, ok := specFields["host"].(map[string]any)
	if !ok {
		t.Fatal("expected host to be an object")
	}
	hostDefault, ok := hostField["default"].(string)
	if !ok || !strings.Contains(hostDefault, "test.example.com") {
		t.Errorf("expected host default to contain 'test.example.com', got %v", hostField["default"])
	}

	constraints, ok := spec["constraints"].(map[string]any)
	if !ok {
		t.Fatal("expected constraints to be an object")
	}
	if constraints["namespace"] == nil || constraints["namespace"] == "" {
		t.Error("expected namespace info in constraints")
	}
}

func TestListResources(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 static resources (platform-info and application-spec)
	if len(res.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(res.Resources))
	}

	names := map[string]bool{}
	for _, r := range res.Resources {
		names[r.Name] = true
	}
	for _, expected := range []string{"platform-info", "application-spec"} {
		if !names[expected] {
			t.Errorf("expected resource %q in listing", expected)
		}
	}

	// Resource templates should list the language-spec template
	tmplRes, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tmplRes.ResourceTemplates) != 1 {
		t.Fatalf("expected 1 resource template, got %d", len(tmplRes.ResourceTemplates))
	}
	if tmplRes.ResourceTemplates[0].Name != "language-spec" {
		t.Errorf("expected template name 'language-spec', got %q", tmplRes.ResourceTemplates[0].Name)
	}
}
