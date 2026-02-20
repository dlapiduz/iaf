package mcp_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	iafgithub "github.com/dlapiduz/iaf/internal/github"
	iafmcp "github.com/dlapiduz/iaf/internal/mcp"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupIntegrationServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := iafv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	store, err := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}

	server := iafmcp.NewServer(k8sClient, sessions, store, "test.example.com", nil, nil, "", "")

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

func TestNewServer_HasInstructions(t *testing.T) {
	cs := setupIntegrationServer(t)

	initResult := cs.InitializeResult()
	if initResult == nil {
		t.Fatal("expected non-nil InitializeResult")
	}
	if initResult.Instructions == "" {
		t.Fatal("expected server instructions to be set")
	}
	// Verify key guidance is present
	for _, keyword := range []string{"register", "session_id", "CALL THIS FIRST", "push_code"} {
		if !strings.Contains(initResult.Instructions, keyword) {
			t.Errorf("instructions should mention %q", keyword)
		}
	}
}

func TestNewServer_RegistersAllTools(t *testing.T) {
	cs := setupIntegrationServer(t)
	ctx := context.Background()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	expectedTools := []string{
		"register",
		"deploy_app",
		"push_code",
		"app_status",
		"app_logs",
		"list_apps",
		"delete_app",
	}

	toolNames := map[string]bool{}
	for _, tool := range res.Tools {
		toolNames[tool.Name] = true
	}
	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestNewServer_RegistersAllPrompts(t *testing.T) {
	cs := setupIntegrationServer(t)
	ctx := context.Background()

	res, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	expectedPrompts := []string{"deploy-guide", "language-guide", "coding-guide", "scaffold-guide"}
	promptNames := map[string]bool{}
	for _, p := range res.Prompts {
		promptNames[p.Name] = true
	}
	for _, name := range expectedPrompts {
		if !promptNames[name] {
			t.Errorf("expected prompt %q to be registered", name)
		}
	}
}

func TestNewServer_RegistersAllResources(t *testing.T) {
	cs := setupIntegrationServer(t)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	expectedResources := []string{"platform-info", "application-spec", "org-coding-standards"}
	resourceNames := map[string]bool{}
	for _, r := range res.Resources {
		resourceNames[r.Name] = true
	}
	for _, name := range expectedResources {
		if !resourceNames[name] {
			t.Errorf("expected resource %q to be registered", name)
		}
	}

	// Check resource templates
	tmplRes, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	expectedTemplates := []string{"language-spec", "scaffold"}
	templateNames := map[string]bool{}
	for _, tmpl := range tmplRes.ResourceTemplates {
		templateNames[tmpl.Name] = true
	}
	for _, name := range expectedTemplates {
		if !templateNames[name] {
			t.Errorf("expected resource template %q to be registered", name)
		}
	}
}

func setupGitHubIntegrationServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := iafv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	store, err := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}

	ghClient := &iafgithub.MockClient{}
	server := iafmcp.NewServer(k8sClient, sessions, store, "test.example.com", nil, ghClient, "test-org", "test-token")

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

func TestNewServer_WithGitHub_RegistersGitHubComponents(t *testing.T) {
	cs := setupGitHubIntegrationServer(t)
	ctx := context.Background()

	// setup_github_repo tool should be present.
	toolRes, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	toolNames := map[string]bool{}
	for _, tool := range toolRes.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["setup_github_repo"] {
		t.Error("expected 'setup_github_repo' tool to be registered when GitHub is configured")
	}

	// github-guide prompt should be present.
	promptRes, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	promptNames := map[string]bool{}
	for _, p := range promptRes.Prompts {
		promptNames[p.Name] = true
	}
	if !promptNames["github-guide"] {
		t.Error("expected 'github-guide' prompt to be registered when GitHub is configured")
	}

	// github-standards resource should be present.
	resourceRes, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	resourceNames := map[string]bool{}
	for _, r := range resourceRes.Resources {
		resourceNames[r.Name] = true
	}
	if !resourceNames["github-standards"] {
		t.Error("expected 'github-standards' resource to be registered when GitHub is configured")
	}
}

func TestNewServer_WithoutGitHub_NoGitHubComponents(t *testing.T) {
	cs := setupIntegrationServer(t) // no GitHub
	ctx := context.Background()

	toolRes, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range toolRes.Tools {
		if tool.Name == "setup_github_repo" {
			t.Error("expected 'setup_github_repo' to NOT be registered without GitHub config")
		}
	}

	promptRes, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range promptRes.Prompts {
		if p.Name == "github-guide" {
			t.Error("expected 'github-guide' to NOT be registered without GitHub config")
		}
	}
}
