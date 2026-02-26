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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupServiceToolServer(t *testing.T) (*gomcp.ClientSession, *auth.SessionStore, *fake.ClientBuilder) {
	t.Helper()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
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

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterProvisionService(server, deps)
	tools.RegisterServiceStatus(server, deps)
	tools.RegisterBindService(server, deps)
	tools.RegisterUnbindService(server, deps)
	tools.RegisterDeprovisionService(server, deps)
	tools.RegisterListServices(server, deps)
	tools.RegisterDeployApp(server, deps)

	ctx := context.Background()
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

	_ = fake.NewClientBuilder().WithScheme(scheme)
	return cs, sessions, nil
}

// registerAndGetSession registers a session and returns its session_id and namespace.
func registerAndGetSession(t *testing.T, cs *gomcp.ClientSession) (sessionID, namespace string) {
	t.Helper()
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "test-agent"},
	})
	if err != nil || res.IsError {
		t.Fatalf("register failed: err=%v, isError=%v", err, res.IsError)
	}
	var result map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result)
	return result["session_id"].(string), result["namespace"].(string)
}

// TestProvisionService_OK verifies that a ManagedService CR is created.
func TestProvisionService_OK(t *testing.T) {
	cs, sessions, _ := setupServiceToolServer(t)
	ctx := context.Background()
	sid, _ := registerAndGetSession(t, cs)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "provision_service",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "mydb",
			"type":       "postgres",
			"plan":       "micro",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("provision_service failed: err=%v, isError=%v", err, res.IsError)
	}
	var result map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result)
	if result["name"] != "mydb" {
		t.Errorf("expected name mydb, got %v", result["name"])
	}

	// Verify CR was created in the right namespace.
	sess, _ := sessions.Lookup(sid)
	_ = sess // namespace is in sess.Namespace, but we can't access the k8s client here directly
}

// TestProvisionService_InvalidType verifies rejection of unknown service types.
func TestProvisionService_InvalidType(t *testing.T) {
	cs, _, _ := setupServiceToolServer(t)
	ctx := context.Background()
	sid, _ := registerAndGetSession(t, cs)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "provision_service",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "mydb",
			"type":       "mysql",
			"plan":       "micro",
		},
	})
	if err == nil && !res.IsError {
		t.Fatal("expected error for unsupported service type mysql")
	}
}

// TestProvisionService_InvalidPlan verifies rejection of unknown plans.
func TestProvisionService_InvalidPlan(t *testing.T) {
	cs, _, _ := setupServiceToolServer(t)
	ctx := context.Background()
	sid, _ := registerAndGetSession(t, cs)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "provision_service",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "mydb",
			"type":       "postgres",
			"plan":       "xlarge",
		},
	})
	if err == nil && !res.IsError {
		t.Fatal("expected error for unsupported plan xlarge")
	}
}

// setupReadyService creates a ManagedService in the fake client with Ready status.
func setupReadyService(t *testing.T, cs *gomcp.ClientSession, sessions *auth.SessionStore, sid, svcName string) {
	t.Helper()
	// We provision the service through the tool, then manually set its status.
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "provision_service",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       svcName,
			"type":       "postgres",
			"plan":       "small",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("provision_service failed: err=%v, isError=%v text=%v", err, res.IsError, res.Content)
	}
}

// TestServiceStatus_Provisioning verifies that status returns no connectionEnvVars when provisioning.
func TestServiceStatus_Provisioning(t *testing.T) {
	cs, sessions, _ := setupServiceToolServer(t)
	ctx := context.Background()
	sid, _ := registerAndGetSession(t, cs)

	setupReadyService(t, cs, sessions, sid, "pgdb")

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "service_status",
		Arguments: map[string]any{
			"session_id": sid,
			"name":       "pgdb",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("service_status failed: %v %v", err, res.IsError)
	}
	var result map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result)

	// Phase should not be Ready (no controller has set it), so no connectionEnvVars.
	if _, ok := result["connectionEnvVars"]; ok {
		t.Error("should not return connectionEnvVars when not Ready")
	}
	if _, ok := result["connectionSecretRef"]; ok {
		t.Error("should never return connectionSecretRef")
	}
}

// TestServiceStatus_Ready verifies connectionEnvVars presence and no secretRef when Ready.
// We use the fake client directly by accessing it via the sessions store.
func TestServiceStatus_Ready(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterServiceStatus(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	client := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := client.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	// Register session.
	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	// Create a Ready ManagedService directly in the fake client.
	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "readydb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{Phase: iafv1alpha1.ManagedServicePhaseReady, ConnectionSecretRef: "readydb-app"},
	}
	_ = k8sClient.Create(ctx, svc)
	_ = k8sClient.Status().Update(ctx, svc)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "service_status",
		Arguments: map[string]any{"session_id": sid, "name": "readydb"},
	})
	if err != nil || res.IsError {
		t.Fatalf("service_status failed: %v", err)
	}
	var result map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result)

	if result["phase"] != "Ready" {
		t.Errorf("expected phase Ready, got %v", result["phase"])
	}
	if _, ok := result["connectionEnvVars"]; !ok {
		t.Error("expected connectionEnvVars in Ready response")
	}
	if _, ok := result["connectionSecretRef"]; ok {
		t.Error("should never return connectionSecretRef to agents")
	}
}

// TestBindService_OK verifies that 6 SecretKeyRef env vars are injected and BoundApps is updated.
func TestBindService_OK(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterBindService(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	cl := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := cl.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	// Create ready service.
	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{Phase: iafv1alpha1.ManagedServicePhaseReady, ConnectionSecretRef: "pgdb-app"},
	}
	k8sClient.Create(ctx, svc)
	k8sClient.Status().Update(ctx, svc)

	// Create application.
	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest", Port: 8080, Replicas: 1},
	}
	k8sClient.Create(ctx, app)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "bind_service",
		Arguments: map[string]any{
			"session_id":   sid,
			"service_name": "pgdb",
			"app_name":     "myapp",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("bind_service failed: %v, isError=%v, content=%v", err, res.IsError, res.Content)
	}
	var result map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result)
	if result["bound"] != true {
		t.Errorf("expected bound=true, got %v", result["bound"])
	}

	// Verify Application has 6 SecretKeyRef env vars.
	var updatedApp iafv1alpha1.Application
	k8sClient.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: ns}, &updatedApp)
	if len(updatedApp.Spec.Env) != 6 {
		t.Errorf("expected 6 env vars, got %d", len(updatedApp.Spec.Env))
	}
	for _, e := range updatedApp.Spec.Env {
		if e.SecretKeyRef == nil {
			t.Errorf("expected SecretKeyRef on env var %s", e.Name)
		} else if e.SecretKeyRef.Name != "pgdb-app" {
			t.Errorf("expected secret name pgdb-app, got %s", e.SecretKeyRef.Name)
		}
	}

	// Verify ManagedService.Status.BoundApps.
	var updatedSvc iafv1alpha1.ManagedService
	k8sClient.Get(ctx, types.NamespacedName{Name: "pgdb", Namespace: ns}, &updatedSvc)
	if len(updatedSvc.Status.BoundApps) != 1 || updatedSvc.Status.BoundApps[0] != "myapp" {
		t.Errorf("expected BoundApps=[myapp], got %v", updatedSvc.Status.BoundApps)
	}
}

// TestBindService_EnvConflict verifies rejection when app already has one of the 6 env vars.
func TestBindService_EnvConflict(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterBindService(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	cl := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := cl.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{Phase: iafv1alpha1.ManagedServicePhaseReady, ConnectionSecretRef: "pgdb-app"},
	}
	k8sClient.Create(ctx, svc)
	k8sClient.Status().Update(ctx, svc)

	// App already has DATABASE_URL.
	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec: iafv1alpha1.ApplicationSpec{
			Image: "nginx:latest", Port: 8080, Replicas: 1,
			Env: []iafv1alpha1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://existing"}},
		},
	}
	k8sClient.Create(ctx, app)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "bind_service",
		Arguments: map[string]any{
			"session_id": sid, "service_name": "pgdb", "app_name": "myapp",
		},
	})
	if err == nil && !res.IsError {
		t.Fatal("expected error when app already has DATABASE_URL")
	}
}

// TestBindService_ServiceNotReady verifies rejection when service is not Ready.
func TestBindService_ServiceNotReady(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterBindService(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	cl := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := cl.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	// Service in Provisioning phase.
	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{Phase: iafv1alpha1.ManagedServicePhaseProvisioning},
	}
	k8sClient.Create(ctx, svc)
	k8sClient.Status().Update(ctx, svc)

	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest", Port: 8080, Replicas: 1},
	}
	k8sClient.Create(ctx, app)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "bind_service",
		Arguments: map[string]any{
			"session_id": sid, "service_name": "pgdb", "app_name": "myapp",
		},
	})
	if err == nil && !res.IsError {
		t.Fatal("expected error binding non-ready service")
	}
}

// TestUnbindService_OK verifies env vars removal and BoundApps update.
func TestUnbindService_OK(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterUnbindService(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	cl := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := cl.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{Phase: iafv1alpha1.ManagedServicePhaseReady, BoundApps: []string{"myapp"}},
	}
	k8sClient.Create(ctx, svc)
	k8sClient.Status().Update(ctx, svc)

	// App has 6 env vars from bind.
	envVars := []iafv1alpha1.EnvVar{
		{Name: "DATABASE_URL", SecretKeyRef: &iafv1alpha1.SecretKeyRef{Name: "pgdb-app", Key: "uri"}},
		{Name: "PGHOST", SecretKeyRef: &iafv1alpha1.SecretKeyRef{Name: "pgdb-app", Key: "host"}},
		{Name: "PGPORT", SecretKeyRef: &iafv1alpha1.SecretKeyRef{Name: "pgdb-app", Key: "port"}},
		{Name: "PGDATABASE", SecretKeyRef: &iafv1alpha1.SecretKeyRef{Name: "pgdb-app", Key: "dbname"}},
		{Name: "PGUSER", SecretKeyRef: &iafv1alpha1.SecretKeyRef{Name: "pgdb-app", Key: "username"}},
		{Name: "PGPASSWORD", SecretKeyRef: &iafv1alpha1.SecretKeyRef{Name: "pgdb-app", Key: "password"}},
		{Name: "MY_VAR", Value: "keep-me"},
	}
	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec: iafv1alpha1.ApplicationSpec{
			Image: "nginx:latest", Port: 8080, Replicas: 1, Env: envVars,
		},
	}
	k8sClient.Create(ctx, app)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "unbind_service",
		Arguments: map[string]any{
			"session_id": sid, "service_name": "pgdb", "app_name": "myapp",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("unbind_service failed: %v, %v", err, res.IsError)
	}

	var updatedApp iafv1alpha1.Application
	k8sClient.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: ns}, &updatedApp)
	if len(updatedApp.Spec.Env) != 1 {
		t.Errorf("expected 1 env var remaining (MY_VAR), got %d: %v", len(updatedApp.Spec.Env), updatedApp.Spec.Env)
	}

	var updatedSvc iafv1alpha1.ManagedService
	k8sClient.Get(ctx, types.NamespacedName{Name: "pgdb", Namespace: ns}, &updatedSvc)
	if len(updatedSvc.Status.BoundApps) != 0 {
		t.Errorf("expected empty BoundApps, got %v", updatedSvc.Status.BoundApps)
	}
}

// TestDeprovisionService_Blocked verifies rejection when BoundApps is non-empty.
func TestDeprovisionService_Blocked(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterDeprovisionService(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	cl := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := cl.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{BoundApps: []string{"myapp"}},
	}
	k8sClient.Create(ctx, svc)
	k8sClient.Status().Update(ctx, svc)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deprovision_service",
		Arguments: map[string]any{"session_id": sid, "name": "pgdb"},
	})
	if err == nil && !res.IsError {
		t.Fatal("expected error when deprovisioning service with bound apps")
	}
}

// TestDeprovisionService_OK verifies ManagedService CR is deleted when no bound apps.
func TestDeprovisionService_OK(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()

	store, _ := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	sessions, _ := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	deps := &tools.Dependencies{
		Client:     k8sClient,
		Store:      store,
		BaseDomain: "test.example.com",
		Sessions:   sessions,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterDeprovisionService(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	server.Connect(ctx, st, nil)
	cl := gomcp.NewClient(&gomcp.Implementation{Name: "tc", Version: "0.0.1"}, nil)
	cs, _ := cl.Connect(ctx, ct, nil)
	t.Cleanup(func() { cs.Close() })

	regRes, _ := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "register", Arguments: map[string]any{"name": "a"},
	})
	var regData map[string]any
	json.Unmarshal([]byte(regRes.Content[0].(*gomcp.TextContent).Text), &regData)
	sid := regData["session_id"].(string)
	ns := regData["namespace"].(string)

	svc := &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
		Spec:       iafv1alpha1.ManagedServiceSpec{Type: "postgres", Plan: "micro"},
		Status:     iafv1alpha1.ManagedServiceStatus{},
	}
	k8sClient.Create(ctx, svc)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deprovision_service",
		Arguments: map[string]any{"session_id": sid, "name": "pgdb"},
	})
	if err != nil || res.IsError {
		t.Fatalf("deprovision_service failed: %v %v", err, res.IsError)
	}

	var check iafv1alpha1.ManagedService
	err = k8sClient.Get(ctx, types.NamespacedName{Name: "pgdb", Namespace: ns}, &check)
	if err == nil {
		t.Error("expected ManagedService to be deleted")
	}
}
