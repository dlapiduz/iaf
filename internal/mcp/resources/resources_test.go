package resources_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	iafgithub "github.com/dlapiduz/iaf/internal/github"
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
	resources.RegisterApplicationSpec(server, deps)
	resources.RegisterDataCatalog(server, deps)

	return connectServer(t, ctx, server)
}

func setupGitHubResourceServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	deps := &tools.Dependencies{
		BaseDomain: "test.example.com",
		GitHub:     &iafgithub.MockClient{},
		GitHubOrg:  "test-org",
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	resources.RegisterPlatformInfo(server, deps)
	resources.RegisterApplicationSpec(server, deps)
	resources.RegisterGitHubStandards(server, deps)

	return connectServer(t, ctx, server)
}

func connectServer(t *testing.T, ctx context.Context, server *gomcp.Server) *gomcp.ClientSession {
	t.Helper()

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

	// Should have 3 static resources (platform-info, application-spec, data-catalog)
	if len(res.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %v", len(res.Resources), func() []string {
			names := make([]string, len(res.Resources))
			for i, r := range res.Resources {
				names[i] = r.Name
			}
			return names
		}())
	}

	names := map[string]bool{}
	for _, r := range res.Resources {
		names[r.Name] = true
	}
	for _, expected := range []string{"platform-info", "application-spec", "data-catalog"} {
		if !names[expected] {
			t.Errorf("expected resource %q in listing", expected)
		}
	}
}

func TestGitHubStandards(t *testing.T) {
	cs := setupGitHubResourceServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://org/github-standards",
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

	var standards map[string]any
	if err := json.Unmarshal([]byte(content.Text), &standards); err != nil {
		t.Fatalf("failed to parse github-standards JSON: %v", err)
	}

	for _, field := range []string{
		"defaultBranch",
		"branchNamingPattern",
		"requiredReviewers",
		"requiredStatusChecks",
		"commitMessageFormat",
		"ciTemplate",
		"iafIntegration",
	} {
		if _, ok := standards[field]; !ok {
			t.Errorf("expected field %q in github-standards", field)
		}
	}

	if standards["defaultBranch"] != "main" {
		t.Errorf("expected defaultBranch 'main', got %v", standards["defaultBranch"])
	}

	checks, ok := standards["requiredStatusChecks"].([]any)
	if !ok || len(checks) == 0 {
		t.Error("expected requiredStatusChecks to be a non-empty array")
	} else if checks[0] != "CI / ci" {
		t.Errorf("expected required status check 'CI / ci', got %v", checks[0])
	}
}

func TestGitHubStandards_ListedInResources(t *testing.T) {
	cs := setupGitHubResourceServer(t)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, r := range res.Resources {
		names[r.Name] = true
	}
	if !names["github-standards"] {
		t.Error("expected 'github-standards' resource to be listed when GitHub is configured")
	}
}
