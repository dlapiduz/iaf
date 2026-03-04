package tools_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupUnregisterServer(t *testing.T) (*gomcp.ClientSession, *auth.SessionStore) {
	t.Helper()
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
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

	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterUnregisterTool(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	mcpClient := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := mcpClient.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, sessions
}

func TestUnregister_HappyPath(t *testing.T) {
	cs, sessions := setupUnregisterServer(t)
	ctx := context.Background()

	// Register a session
	regRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "my-agent"},
	})
	if err != nil || regRes.IsError {
		t.Fatal("register failed")
	}
	var regOut map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regOut)
	sid := regOut["session_id"].(string)

	// Verify session exists
	if _, ok := sessions.Lookup(sid); !ok {
		t.Fatal("session must exist before unregister")
	}

	// Unregister
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "unregister",
		Arguments: map[string]any{"session_id": sid},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unregister failed: %s", res.Content[0].(*gomcp.TextContent).Text)
	}

	var out map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	if out["status"] != "unregistered" {
		t.Errorf("expected status=unregistered, got %v", out["status"])
	}

	// Session must no longer exist
	if _, ok := sessions.Lookup(sid); ok {
		t.Error("session must be removed after unregister")
	}
}

func TestUnregister_InvalidSession(t *testing.T) {
	cs, _ := setupUnregisterServer(t)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "unregister",
		Arguments: map[string]any{"session_id": "bad-session-id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected error for invalid session_id")
	}
}

func TestRegister_TTLInResponse(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))

	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
		SessionTTL: 24 * 60 * 60 * 1_000_000_000, // 24h in nanoseconds
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	mcpClient := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := mcpClient.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent"},
	})
	if err != nil || res.IsError {
		t.Fatal("register failed")
	}

	var out map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)

	if _, ok := out["ttl_seconds"]; !ok {
		t.Error("expected ttl_seconds in register response when TTL is set")
	}
	if _, ok := out["expires_after"]; !ok {
		t.Error("expected expires_after in register response when TTL is set")
	}
}
