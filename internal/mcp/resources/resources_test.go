package resources_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/mcp/resources"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	deps := &tools.Dependencies{
		BaseDomain:   "test.example.com",
		OrgStandards: orgstandards.New("", nil),
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	resources.RegisterPlatformInfo(server, deps)
	resources.RegisterLanguageResources(server, deps)
	resources.RegisterApplicationSpec(server, deps)
	resources.RegisterOrgStandards(server, deps)
	resources.RegisterScaffoldResource(server, deps)
	resources.RegisterLoggingStandards(server, deps)
	resources.RegisterMetricsStandards(server, deps)
	resources.RegisterTracingStandards(server, deps)

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
	resources.RegisterLanguageResources(server, deps)
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

	// Should have 6 static resources (platform-info, application-spec, org-coding-standards + 3 observability)
	if len(res.Resources) != 6 {
		t.Fatalf("expected 6 resources, got %d", len(res.Resources))
	}

	names := map[string]bool{}
	for _, r := range res.Resources {
		names[r.Name] = true
	}
	for _, expected := range []string{"platform-info", "application-spec", "org-coding-standards", "org-logging-standards", "org-metrics-standards", "org-tracing-standards"} {
		if !names[expected] {
			t.Errorf("expected resource %q in listing", expected)
		}
	}

	// Resource templates should list language-spec and scaffold.
	tmplRes, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tmplRes.ResourceTemplates) != 2 {
		t.Fatalf("expected 2 resource templates, got %d", len(tmplRes.ResourceTemplates))
	}
	tmplNames := map[string]bool{}
	for _, tmpl := range tmplRes.ResourceTemplates {
		tmplNames[tmpl.Name] = true
	}
	for _, expected := range []string{"language-spec", "scaffold"} {
		if !tmplNames[expected] {
			t.Errorf("expected resource template %q in listing", expected)
		}
	}
}

func TestScaffoldResource_NextJS(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://scaffold/nextjs",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}
	if res.Contents[0].MIMEType != "application/json" {
		t.Errorf("expected application/json, got %q", res.Contents[0].MIMEType)
	}

	var files map[string]string
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &files); err != nil {
		t.Fatalf("failed to parse scaffold JSON: %v", err)
	}

	for _, key := range []string{"package.json", "pages/index.js", "pages/api/health.js", "styles/globals.css", "components/Layout.js"} {
		if _, ok := files[key]; !ok {
			t.Errorf("expected key %q in nextjs scaffold", key)
		}
	}

	// Verify health endpoint content.
	if !strings.Contains(files["pages/api/health.js"], "status(200)") {
		t.Error("expected health.js to return HTTP 200")
	}
}

func TestScaffoldResource_HTML(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://scaffold/html",
	})
	if err != nil {
		t.Fatal(err)
	}

	var files map[string]string
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &files); err != nil {
		t.Fatalf("failed to parse scaffold JSON: %v", err)
	}

	for _, key := range []string{"package.json", "server.js", "public/index.html"} {
		if _, ok := files[key]; !ok {
			t.Errorf("expected key %q in html scaffold", key)
		}
	}

	// Verify server.js has a /health route.
	if !strings.Contains(files["server.js"], "/health") {
		t.Error("expected server.js to define /health route")
	}
}

func TestScaffoldResource_Unknown(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	_, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://scaffold/react-native",
	})
	if err == nil {
		t.Fatal("expected error for unknown scaffold framework")
	}
	if !strings.Contains(err.Error(), "react-native") {
		t.Errorf("expected framework name in error, got: %v", err)
	}
}

func TestOrgCodingStandards(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://org/coding-standards",
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
		t.Fatalf("failed to parse org standards JSON: %v", err)
	}

	// Verify top-level keys from the OrgStandards struct are present.
	for _, key := range []string{"healthCheckPath", "loggingFormat", "defaultPort", "envVarNaming", "bestPractices", "perLanguage"} {
		if _, ok := standards[key]; !ok {
			t.Errorf("expected key %q in org standards", key)
		}
	}

	// Default port should be 8080.
	if standards["defaultPort"] != float64(8080) {
		t.Errorf("expected defaultPort 8080, got %v", standards["defaultPort"])
	}

	// perLanguage should contain at least the platform defaults (go, nodejs, python).
	perLang, ok := standards["perLanguage"].(map[string]any)
	if !ok {
		t.Fatal("expected perLanguage to be an object")
	}
	for _, lang := range []string{"go", "nodejs", "python"} {
		if _, ok := perLang[lang]; !ok {
			t.Errorf("expected language %q in perLanguage defaults", lang)
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

func TestLoggingStandards(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://org/logging-standards",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}
	if res.Contents[0].MIMEType != "application/json" {
		t.Errorf("expected application/json, got %q", res.Contents[0].MIMEType)
	}

	var standards map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &standards); err != nil {
		t.Fatalf("failed to parse logging standards JSON: %v", err)
	}

	for _, key := range []string{"requiredFields", "recommendedFields", "correlationFields", "prohibited", "output"} {
		if _, ok := standards[key]; !ok {
			t.Errorf("expected key %q in logging standards", key)
		}
	}

	output, ok := standards["output"].(map[string]any)
	if !ok {
		t.Fatal("expected output to be an object")
	}
	if output["target"] != "stdout" {
		t.Errorf("expected output.target 'stdout', got %v", output["target"])
	}
}

func TestMetricsStandards(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://org/metrics-standards",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}

	var standards map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &standards); err != nil {
		t.Fatalf("failed to parse metrics standards JSON: %v", err)
	}

	for _, key := range []string{"requiredMetrics", "endpoint", "standardLabels", "method"} {
		if _, ok := standards[key]; !ok {
			t.Errorf("expected key %q in metrics standards", key)
		}
	}

	endpoint, ok := standards["endpoint"].(map[string]any)
	if !ok {
		t.Fatal("expected endpoint to be an object")
	}
	if endpoint["path"] != "/metrics" {
		t.Errorf("expected endpoint.path '/metrics', got %v", endpoint["path"])
	}

	if standards["method"] != "RED" {
		t.Errorf("expected method 'RED', got %v", standards["method"])
	}
}

func TestTracingStandards(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "iaf://org/tracing-standards",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}

	var standards map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &standards); err != nil {
		t.Fatalf("failed to parse tracing standards JSON: %v", err)
	}

	for _, key := range []string{"sdk", "endpointSource", "requiredSpanAttributes", "traceLogCorrelation", "autoInstrumentation", "securityNote"} {
		if _, ok := standards[key]; !ok {
			t.Errorf("expected key %q in tracing standards", key)
		}
	}

	if standards["sdk"] != "OpenTelemetry" {
		t.Errorf("expected sdk 'OpenTelemetry', got %v", standards["sdk"])
	}
}
