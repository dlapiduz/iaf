package mcp_test

import (
	"context"
	"log/slog"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	iafmcp "github.com/dlapiduz/iaf/internal/mcp"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
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
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	store, err := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	server := iafmcp.NewServer(k8sClient, "test-ns", store, "test.example.com")

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

func TestNewServer_RegistersAllTools(t *testing.T) {
	cs := setupIntegrationServer(t)
	ctx := context.Background()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	expectedTools := []string{
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

	expectedPrompts := []string{"deploy-guide", "language-guide"}
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

	expectedResources := []string{"platform-info", "application-spec"}
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

	expectedTemplates := []string{"language-spec"}
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
