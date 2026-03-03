package tools_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAppStatus_TraceExploreURL_Integration(t *testing.T) {
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

	tempoURL := "https://grafana.example.com"
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
		TempoURL:   tempoURL,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterAppStatus(server, deps)

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

	// Register session
	regRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "test"},
	})
	if err != nil || regRes.IsError {
		t.Fatal("register failed")
	}
	var reg map[string]any
	_ = json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &reg)
	sid := reg["session_id"].(string)
	namespace := reg["namespace"].(string)

	// Create an Application in the shared k8s client
	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: namespace},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest", Port: 8080, Replicas: 1},
	}
	if err := k8sClient.Create(ctx, app); err != nil {
		t.Fatalf("creating app: %v", err)
	}

	// Call app_status
	statusRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "app_status",
		Arguments: map[string]any{"session_id": sid, "name": "myapp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if statusRes.IsError {
		t.Fatalf("app_status returned error: %s", statusRes.Content[0].(*gomcp.TextContent).Text)
	}

	var result map[string]any
	text := statusRes.Content[0].(*gomcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("failed to parse app_status JSON: %v", err)
	}

	traceURL, ok := result["traceExploreUrl"].(string)
	if !ok || traceURL == "" {
		t.Errorf("expected traceExploreUrl in app_status response when TempoURL is set, got: %v", result["traceExploreUrl"])
	}
	if !strings.Contains(traceURL, "grafana.example.com") {
		t.Errorf("expected traceExploreUrl to contain Grafana host, got %q", traceURL)
	}
	if !strings.Contains(traceURL, "myapp") {
		t.Errorf("expected traceExploreUrl to contain app name, got %q", traceURL)
	}
	if !strings.Contains(traceURL, "/explore") {
		t.Errorf("expected traceExploreUrl to contain /explore, got %q", traceURL)
	}
}

func TestAppStatus_NoTraceExploreURL_WhenTempoNotConfigured(t *testing.T) {
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
		TempoURL:   "", // no Tempo configured
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterAppStatus(server, deps)

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

	// Register and create app
	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "test"},
	})
	var reg map[string]any
	_ = json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &reg)
	sid := reg["session_id"].(string)
	namespace := reg["namespace"].(string)

	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: namespace},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest", Port: 8080, Replicas: 1},
	}
	_ = k8sClient.Create(ctx, app)

	statusRes, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "app_status",
		Arguments: map[string]any{"session_id": sid, "name": "myapp"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	_ = json.Unmarshal([]byte(statusRes.Content[0].(*gomcp.TextContent).Text), &result)

	if _, ok := result["traceExploreUrl"]; ok {
		t.Error("expected traceExploreUrl to be absent when TempoURL is not configured")
	}
}
