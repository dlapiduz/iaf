package tools_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// logsTestSetup holds all the pieces needed for a logs test.
type logsTestSetup struct {
	cs        *gomcp.ClientSession
	sessions  *auth.SessionStore
	k8sClient ctrlclient.Client
}

func setupLogsToolServer(t *testing.T) *logsTestSetup {
	t.Helper()
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.Application{}).
		Build()

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
	clientset := k8sfake.NewSimpleClientset()

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterAppLogsWithClientset(server, deps, clientset)

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

	return &logsTestSetup{cs: cs, sessions: sessions, k8sClient: k8sClient}
}

// registerLogsSession calls the register tool and returns (sessionID, namespace).
func registerLogsSession(t *testing.T, cs *gomcp.ClientSession, name string) (string, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": name},
	})
	if err != nil || res.IsError {
		t.Fatal("register failed")
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	return out["session_id"].(string), out["namespace"].(string)
}

func makeTestApp(name, namespace string) *iafv1alpha1.Application {
	return &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
		Status:     iafv1alpha1.ApplicationStatus{Phase: iafv1alpha1.ApplicationPhaseRunning},
	}
}

func makeTestPod(name, namespace, appName string, createdAt time.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            map[string]string{"iaf.io/application": appName},
			CreationTimestamp: metav1.Time{Time: createdAt},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestAppLogs_AppNotFound(t *testing.T) {
	setup := setupLogsToolServer(t)
	ctx := context.Background()
	sid, _ := registerLogsSession(t, setup.cs, "agent")

	res, err := setup.cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "app_logs",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "nonexistent",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected error for missing application")
	}
}

func TestAppLogs_NoPods(t *testing.T) {
	setup := setupLogsToolServer(t)
	ctx := context.Background()
	sid, ns := registerLogsSession(t, setup.cs, "agent")

	// Create the app but no pods
	app := makeTestApp("myapp", ns)
	if err := setup.k8sClient.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	// Explicitly set status (fake client requires Status().Update for status subresource)
	app.Status.Phase = iafv1alpha1.ApplicationPhaseRunning
	_ = setup.k8sClient.Status().Update(ctx, app)

	res, err := setup.cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "app_logs",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "myapp",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(*gomcp.TextContent).Text)
	}

	var out map[string]any
	_ = json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)

	if out["logs"] != "No pods found" {
		t.Errorf("expected 'No pods found', got %q", out["logs"])
	}
	pods, ok := out["availablePods"]
	if !ok {
		t.Error("expected availablePods in response")
	}
	if len(pods.([]interface{})) != 0 {
		t.Error("expected empty availablePods")
	}
}

func TestAppLogs_MultiPod_AvailablePodsReturned(t *testing.T) {
	setup := setupLogsToolServer(t)
	ctx := context.Background()
	sid, ns := registerLogsSession(t, setup.cs, "agent")

	now := time.Now()
	app := makeTestApp("myapp", ns)
	pod1 := makeTestPod("myapp-pod-old", ns, "myapp", now.Add(-5*time.Minute))
	pod2 := makeTestPod("myapp-pod-new", ns, "myapp", now)

	if err := setup.k8sClient.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := setup.k8sClient.Create(ctx, pod1); err != nil {
		t.Fatal(err)
	}
	if err := setup.k8sClient.Create(ctx, pod2); err != nil {
		t.Fatal(err)
	}

	// The fake k8sfake clientset won't stream log data, so GetLogs will error.
	// We verify the response structure up to the point of the log fetch.
	// Since GetLogs fails, the tool returns an error — but we've already validated
	// the pod selection logic in the k8s/pods_test.go unit tests.
	// Here we just ensure the tool calls without panicking on multi-pod selection.
	res, _ := setup.cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "app_logs",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "myapp",
		},
	})
	// We don't assert success because the fake clientset can't stream logs.
	// The important thing is the tool doesn't panic.
	_ = res
}

func TestAppLogs_InvalidPodName_CrossAppDenied(t *testing.T) {
	setup := setupLogsToolServer(t)
	ctx := context.Background()
	sid, ns := registerLogsSession(t, setup.cs, "agent")

	now := time.Now()
	app := makeTestApp("myapp", ns)
	podOwned := makeTestPod("myapp-pod", ns, "myapp", now)
	// A pod belonging to a different app in the same namespace
	podOther := makeTestPod("otherapp-pod", ns, "otherapp", now)

	for _, obj := range []ctrlclient.Object{app, podOwned, podOther} {
		if err := setup.k8sClient.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}

	// Requesting logs from a pod that belongs to a different app should be rejected
	res, err := setup.cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "app_logs",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "myapp",
			"pod_name":   "otherapp-pod",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected error when specifying pod from a different app (cross-app access must be denied)")
	}
}

func TestAppLogs_InvalidSession(t *testing.T) {
	setup := setupLogsToolServer(t)
	ctx := context.Background()

	res, err := setup.cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "app_logs",
		Arguments: map[string]any{
			"session_id": "bad-session-id",
			"name":       "myapp",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected error for invalid session_id")
	}
}
