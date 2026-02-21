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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// setupDSToolServer creates a server with all data source tools registered.
func setupDSToolServer(t *testing.T) (*gomcp.ClientSession, *auth.SessionStore, client.Client) {
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
	tools.RegisterListDataSources(server, deps)
	tools.RegisterGetDataSource(server, deps)
	tools.RegisterAttachDataSource(server, deps)

	st, ct := gomcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	mc := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := mc.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, sessions, k8sClient
}

// registerDSSession registers a session and returns the session ID and namespace.
func registerDSSession(t *testing.T, cs *gomcp.ClientSession) (sessionID, namespace string) {
	t.Helper()
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "ds-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	return out["session_id"].(string), out["namespace"].(string)
}

// makeDataSource creates a cluster-scoped DataSource CR in the fake client.
func makeDataSource(t *testing.T, k8sClient client.Client, name, kind string, tags []string, envMapping map[string]string, secretName, secretNS string) *iafv1alpha1.DataSource {
	t.Helper()
	ds := &iafv1alpha1.DataSource{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: iafv1alpha1.DataSourceSpec{
			Kind:          kind,
			Description:   "Test " + name,
			Tags:          tags,
			EnvVarMapping: envMapping,
			SecretRef: iafv1alpha1.DataSourceSecretRef{
				Name:      secretName,
				Namespace: secretNS,
			},
		},
	}
	if err := k8sClient.Create(context.Background(), ds); err != nil {
		t.Fatal(err)
	}
	return ds
}

// makeSecret creates a plain Opaque Secret in the fake client.
func makeSecret(t *testing.T, k8sClient client.Client, name, namespace string, data map[string][]byte) {
	t.Helper()
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
	if err := k8sClient.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

// makeApp creates an Application CR in the given namespace.
func makeApp(t *testing.T, k8sClient client.Client, name, namespace string) *iafv1alpha1.Application {
	t.Helper()
	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: "test-uid"},
		Spec: iafv1alpha1.ApplicationSpec{
			Image:    "nginx:latest",
			Port:     8080,
			Replicas: 1,
		},
	}
	if err := k8sClient.Create(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return app
}

// ---- Tests -------------------------------------------------------------------

func TestListDataSources_FiltersWork(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, _ := registerDSSession(t, cs)

	makeDataSource(t, k8sClient, "prod-postgres", "postgres", []string{"production", "sql"}, map[string]string{"host": "PG_HOST"}, "pg-creds", "iaf-system")
	makeDataSource(t, k8sClient, "dev-mysql", "mysql", []string{"dev"}, map[string]string{"url": "MYSQL_URL"}, "mysql-creds", "iaf-system")

	// No filter — both returned.
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_data_sources",
		Arguments: map[string]any{"session_id": sid},
	})
	if err != nil || res.IsError {
		t.Fatalf("list_data_sources error: %v", err)
	}
	var out map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	if int(out["total"].(float64)) != 2 {
		t.Errorf("expected 2 data sources, got %v", out["total"])
	}

	// Filter by kind=postgres.
	res, err = cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_data_sources",
		Arguments: map[string]any{"session_id": sid, "kind": "postgres"},
	})
	if err != nil || res.IsError {
		t.Fatalf("list_data_sources (kind filter) error: %v", err)
	}
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	if int(out["total"].(float64)) != 1 {
		t.Errorf("expected 1 postgres data source, got %v", out["total"])
	}

	// Filter by tag=production.
	res, err = cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_data_sources",
		Arguments: map[string]any{"session_id": sid, "tags": "production"},
	})
	if err != nil || res.IsError {
		t.Fatalf("list_data_sources (tag filter) error: %v", err)
	}
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	if int(out["total"].(float64)) != 1 {
		t.Errorf("expected 1 production data source, got %v", out["total"])
	}
}

func TestGetDataSource_NoCredentialData(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, _ := registerDSSession(t, cs)

	makeDataSource(t, k8sClient, "prod-postgres", "postgres", nil,
		map[string]string{"password": "PG_PASSWORD", "host": "PG_HOST"},
		"pg-creds", "iaf-system")
	makeSecret(t, k8sClient, "pg-creds", "iaf-system", map[string][]byte{
		"password": []byte("supersecret"),
		"host":     []byte("db.prod.example.com"),
	})

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "get_data_source",
		Arguments: map[string]any{"session_id": sid, "name": "prod-postgres"},
	})
	if err != nil || res.IsError {
		t.Fatalf("get_data_source error: %v", err)
	}

	text := res.Content[0].(*gomcp.TextContent).Text

	// Credential values must never appear.
	if strings.Contains(text, "supersecret") {
		t.Error("credential value 'supersecret' must not appear in get_data_source output")
	}
	if strings.Contains(text, "db.prod.example.com") {
		t.Error("credential value 'db.prod.example.com' must not appear in get_data_source output")
	}

	// Env var names must appear.
	var out map[string]any
	json.Unmarshal([]byte(text), &out)
	names, _ := out["envVarNames"].([]any)
	if len(names) != 2 {
		t.Errorf("expected 2 envVarNames, got %v", names)
	}
}

func TestAttachDataSource_SecretCopied(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, namespace := registerDSSession(t, cs)

	makeDataSource(t, k8sClient, "prod-postgres", "postgres", nil,
		map[string]string{"host": "PG_HOST", "password": "PG_PASSWORD"},
		"pg-creds", "iaf-system")
	makeSecret(t, k8sClient, "pg-creds", "iaf-system", map[string][]byte{
		"host":     []byte("db.example.com"),
		"password": []byte("s3cr3t"),
	})
	makeApp(t, k8sClient, "myapp", namespace)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "attach_data_source",
		Arguments: map[string]any{"session_id": sid, "app_name": "myapp", "datasource_name": "prod-postgres"},
	})
	if err != nil || res.IsError {
		t.Fatalf("attach_data_source error: %v / %v", err, res)
	}

	// Verify the Secret was copied into the session namespace.
	var copiedSecret corev1.Secret
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "iaf-ds-prod-postgres", Namespace: namespace}, &copiedSecret); err != nil {
		t.Fatalf("expected copied Secret to exist: %v", err)
	}

	// Copied Secret must be owned by the Application.
	if len(copiedSecret.OwnerReferences) == 0 || copiedSecret.OwnerReferences[0].Name != "myapp" {
		t.Errorf("expected copied Secret to be owned by Application 'myapp', got %v", copiedSecret.OwnerReferences)
	}

	// Application CR must be updated with the attachment.
	var app iafv1alpha1.Application
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: namespace}, &app); err != nil {
		t.Fatal(err)
	}
	if len(app.Spec.AttachedDataSources) != 1 || app.Spec.AttachedDataSources[0].DataSourceName != "prod-postgres" {
		t.Errorf("expected AttachedDataSources to contain 'prod-postgres', got %v", app.Spec.AttachedDataSources)
	}
}

func TestAttachDataSource_Idempotent(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, namespace := registerDSSession(t, cs)

	makeDataSource(t, k8sClient, "prod-postgres", "postgres", nil,
		map[string]string{"host": "PG_HOST"}, "pg-creds", "iaf-system")
	makeSecret(t, k8sClient, "pg-creds", "iaf-system", map[string][]byte{"host": []byte("db.example.com")})
	makeApp(t, k8sClient, "myapp", namespace)

	attach := func() {
		res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
			Name:      "attach_data_source",
			Arguments: map[string]any{"session_id": sid, "app_name": "myapp", "datasource_name": "prod-postgres"},
		})
		if err != nil || res.IsError {
			t.Fatalf("attach_data_source error: %v", err)
		}
	}

	attach() // first call
	attach() // second call — must not error or duplicate the attachment

	var app iafv1alpha1.Application
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: namespace}, &app); err != nil {
		t.Fatal(err)
	}
	if len(app.Spec.AttachedDataSources) != 1 {
		t.Errorf("expected exactly 1 attached data source after idempotent call, got %d", len(app.Spec.AttachedDataSources))
	}
}

func TestAttachDataSource_EnvVarCollision(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, namespace := registerDSSession(t, cs)

	// App already has PG_HOST set as a literal env var.
	app := makeApp(t, k8sClient, "myapp", namespace)
	app.Spec.Env = []iafv1alpha1.EnvVar{{Name: "PG_HOST", Value: "localhost"}}
	if err := k8sClient.Update(ctx, app); err != nil {
		t.Fatal(err)
	}

	makeDataSource(t, k8sClient, "prod-postgres", "postgres", nil,
		map[string]string{"host": "PG_HOST"}, "pg-creds", "iaf-system")
	makeSecret(t, k8sClient, "pg-creds", "iaf-system", map[string][]byte{"host": []byte("db.example.com")})

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "attach_data_source",
		Arguments: map[string]any{"session_id": sid, "app_name": "myapp", "datasource_name": "prod-postgres"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected attach_data_source to return an error on env var collision")
	}
	if !strings.Contains(res.Content[0].(*gomcp.TextContent).Text, "PG_HOST") {
		t.Error("error message should mention the conflicting env var name 'PG_HOST'")
	}
}

func TestAttachDataSource_NeverExposeCredentialData(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, namespace := registerDSSession(t, cs)

	makeDataSource(t, k8sClient, "my-db", "postgres", nil,
		map[string]string{"password": "DB_PASSWORD"}, "my-db-creds", "iaf-system")
	makeSecret(t, k8sClient, "my-db-creds", "iaf-system", map[string][]byte{
		"password": []byte("top-secret-password"),
	})
	makeApp(t, k8sClient, "myapp", namespace)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "attach_data_source",
		Arguments: map[string]any{"session_id": sid, "app_name": "myapp", "datasource_name": "my-db"},
	})
	if err != nil || res.IsError {
		t.Fatalf("attach_data_source error: %v", err)
	}

	text := res.Content[0].(*gomcp.TextContent).Text
	if strings.Contains(text, "top-secret-password") {
		t.Error("credential value must never appear in attach_data_source output")
	}
	if strings.Contains(text, "my-db-creds") {
		t.Error("source secret name from iaf-system must not appear in tool output")
	}
}

func TestListDataSources_NoCredentialData(t *testing.T) {
	cs, _, k8sClient := setupDSToolServer(t)
	ctx := context.Background()
	sid, _ := registerDSSession(t, cs)

	makeDataSource(t, k8sClient, "my-db", "postgres", nil,
		map[string]string{"password": "DB_PASSWORD"}, "my-db-creds", "iaf-system")
	makeSecret(t, k8sClient, "my-db-creds", "iaf-system", map[string][]byte{
		"password": []byte("top-secret"),
	})

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_data_sources",
		Arguments: map[string]any{"session_id": sid},
	})
	if err != nil || res.IsError {
		t.Fatalf("list_data_sources error: %v", err)
	}

	text := res.Content[0].(*gomcp.TextContent).Text
	if strings.Contains(text, "top-secret") {
		t.Error("credential value must not appear in list_data_sources output")
	}
	// SecretRef details must not appear.
	if strings.Contains(text, "my-db-creds") {
		t.Error("source secret name must not appear in list_data_sources output")
	}
}
