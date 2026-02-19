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

func setupToolServer(t *testing.T) (*gomcp.ClientSession, *auth.SessionStore) {
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
	tools.RegisterDeployApp(server, deps)
	tools.RegisterListApps(server, deps)
	tools.RegisterPushCode(server, deps)
	tools.RegisterDeleteApp(server, deps)

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
	return cs, sessions
}

func TestRegisterTool(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "my-project"},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Content[0].(*gomcp.TextContent).Text
	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}

	sessionID, ok := result["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatal("expected non-empty session_id")
	}
	ns, ok := result["namespace"].(string)
	if !ok || ns == "" {
		t.Fatal("expected non-empty namespace")
	}
	if ns != "iaf-"+sessionID {
		t.Errorf("expected namespace iaf-%s, got %s", sessionID, ns)
	}
}

func TestToolsRequireSession(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	// Calling a tool without session_id should fail
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_apps",
		Arguments: map[string]any{},
	})
	// The SDK may return either an error or a result with IsError=true
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error when calling tool without session_id")
	}
}

func TestToolsWithInvalidSession(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_apps",
		Arguments: map[string]any{"session_id": "nonexistent"},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error for invalid session_id")
	}
}

func TestSessionIsolation(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	// Register two sessions
	res1, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var s1 map[string]any
	json.Unmarshal([]byte(res1.Content[0].(*gomcp.TextContent).Text), &s1)
	sid1 := s1["session_id"].(string)

	res2, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var s2 map[string]any
	json.Unmarshal([]byte(res2.Content[0].(*gomcp.TextContent).Text), &s2)
	sid2 := s2["session_id"].(string)

	// Deploy an app in session 1
	_, err = cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id": sid1,
			"name":       "myapp",
			"image":      "nginx:latest",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Session 1 should see the app
	listRes1, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_apps",
		Arguments: map[string]any{"session_id": sid1},
	})
	if err != nil {
		t.Fatal(err)
	}
	var list1 map[string]any
	json.Unmarshal([]byte(listRes1.Content[0].(*gomcp.TextContent).Text), &list1)
	if list1["total"].(float64) != 1 {
		t.Errorf("session 1: expected 1 app, got %v", list1["total"])
	}

	// Session 2 should see empty list
	listRes2, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_apps",
		Arguments: map[string]any{"session_id": sid2},
	})
	if err != nil {
		t.Fatal(err)
	}
	var list2 map[string]any
	json.Unmarshal([]byte(listRes2.Content[0].(*gomcp.TextContent).Text), &list2)
	if list2["total"].(float64) != 0 {
		t.Errorf("session 2: expected 0 apps, got %v", list2["total"])
	}
}

func TestAppNameCollision_DeployApp(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	// Register two sessions
	res1, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-a"},
	})
	var s1 map[string]any
	json.Unmarshal([]byte(res1.Content[0].(*gomcp.TextContent).Text), &s1)
	sid1 := s1["session_id"].(string)

	res2, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-b"},
	})
	var s2 map[string]any
	json.Unmarshal([]byte(res2.Content[0].(*gomcp.TextContent).Text), &s2)
	sid2 := s2["session_id"].(string)

	// Deploy "hello-world" in session 1
	_, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id": sid1,
			"name":       "hello-world",
			"image":      "nginx:latest",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Deploy "hello-world" in session 2 should fail
	collisionRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id": sid2,
			"name":       "hello-world",
			"image":      "nginx:latest",
		},
	})
	if err == nil && (collisionRes == nil || !collisionRes.IsError) {
		t.Fatal("expected error when deploying app with name already used by another session")
	}
}

func TestAppNameCollision_PushCode(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	// Register two sessions
	res1, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-a"},
	})
	var s1 map[string]any
	json.Unmarshal([]byte(res1.Content[0].(*gomcp.TextContent).Text), &s1)
	sid1 := s1["session_id"].(string)

	res2, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-b"},
	})
	var s2 map[string]any
	json.Unmarshal([]byte(res2.Content[0].(*gomcp.TextContent).Text), &s2)
	sid2 := s2["session_id"].(string)

	// Push code for "hello-world" in session 1
	_, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "push_code",
		Arguments: map[string]any{
			"session_id": sid1,
			"name":       "hello-world",
			"files":      map[string]any{"main.go": "package main\nfunc main() {}\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Push code for "hello-world" in session 2 should fail
	collisionRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "push_code",
		Arguments: map[string]any{
			"session_id": sid2,
			"name":       "hello-world",
			"files":      map[string]any{"main.go": "package main\nfunc main() {}\n"},
		},
	})
	if err == nil && (collisionRes == nil || !collisionRes.IsError) {
		t.Fatal("expected error when pushing code with name already used by another session")
	}
}

func TestAppNameCollision_SameSessionAllowed(t *testing.T) {
	cs, _ := setupToolServer(t)
	ctx := context.Background()

	// Register one session
	res1, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "agent-a"},
	})
	var s1 map[string]any
	json.Unmarshal([]byte(res1.Content[0].(*gomcp.TextContent).Text), &s1)
	sid1 := s1["session_id"].(string)

	// Deploy "hello-world" in session 1
	deployRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id": sid1,
			"name":       "hello-world",
			"image":      "nginx:latest",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deployRes.IsError {
		t.Fatal("first deploy should succeed")
	}

	// push_code for same name in same session should succeed (updates existing)
	pushRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "push_code",
		Arguments: map[string]any{
			"session_id": sid1,
			"name":       "hello-world",
			"files":      map[string]any{"main.go": "package main\nfunc main() {}\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pushRes.IsError {
		t.Fatal("push_code to own app should succeed")
	}
}
