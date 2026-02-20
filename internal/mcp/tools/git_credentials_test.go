package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	iafk8s "github.com/dlapiduz/iaf/internal/k8s"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// setupCredToolServer creates a server with the three git credential tools registered
// and returns the client session, session store, and the underlying k8s client for assertions.
func setupCredToolServer(t *testing.T) (*gomcp.ClientSession, *auth.SessionStore, client.Client) {
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
	tools.RegisterAddGitCredential(server, deps)
	tools.RegisterListGitCredentials(server, deps)
	tools.RegisterDeleteGitCredential(server, deps)
	tools.RegisterDeployApp(server, deps)

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

// registerCredSession registers a session (which also creates the kpack SA via EnsureNamespace)
// and returns the session ID and namespace.
func registerCredSession(t *testing.T, cs *gomcp.ClientSession, k8sClient client.Client) (sessionID, namespace string) {
	t.Helper()
	ctx := context.Background()
	_ = k8sClient // retained in signature for use in test assertions

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)
	sessionID = out["session_id"].(string)
	namespace = out["namespace"].(string)
	return sessionID, namespace
}

func TestAddGitCredential_BasicAuth(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "github-creds",
			"type":           "basic-auth",
			"git_server_url": "https://github.com",
			"username":       "myuser",
			"password":       "ghp_token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(*gomcp.TextContent).Text)
	}

	var out map[string]any
	json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &out)

	// Output must not contain credential material.
	text := res.Content[0].(*gomcp.TextContent).Text
	if strings.Contains(text, "ghp_token") {
		t.Error("password must not appear in tool output")
	}
	if strings.Contains(text, "myuser") {
		t.Error("username must not appear in tool output")
	}
	if out["created"] != true {
		t.Errorf("expected created=true, got %v", out["created"])
	}
	if out["git_server_url"] != "https://github.com" {
		t.Errorf("expected git_server_url in output, got %v", out["git_server_url"])
	}

	// Secret must exist with correct type and labels.
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "github-creds"}, secret); err != nil {
		t.Fatalf("expected secret to exist: %v", err)
	}
	if secret.Type != corev1.SecretTypeBasicAuth {
		t.Errorf("expected type kubernetes.io/basic-auth, got %s", secret.Type)
	}
	if secret.Labels[iafk8s.LabelCredentialType] != "git" {
		t.Errorf("expected label %s=git", iafk8s.LabelCredentialType)
	}
	if secret.Annotations[iafk8s.AnnotationKpackGit] != "https://github.com" {
		t.Errorf("expected kpack.io/git annotation")
	}

	// kpack SA must have the secret in its secrets list.
	sa := &corev1.ServiceAccount{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: iafk8s.KpackServiceAccount}, sa); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range sa.Secrets {
		if ref.Name == "github-creds" {
			found = true
		}
	}
	if !found {
		t.Error("expected secret to be added to kpack SA secrets list")
	}
}

func TestAddGitCredential_SSH(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	fakeKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nZmFrZWtleQ==\n-----END OPENSSH PRIVATE KEY-----\n"

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "ssh-creds",
			"type":           "ssh",
			"git_server_url": "git@github.com",
			"private_key":    fakeKey,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(*gomcp.TextContent).Text)
	}

	text := res.Content[0].(*gomcp.TextContent).Text
	if strings.Contains(text, fakeKey) || strings.Contains(text, "PRIVATE KEY") {
		t.Error("private key must not appear in tool output")
	}

	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "ssh-creds"}, secret); err != nil {
		t.Fatalf("expected secret to exist: %v", err)
	}
	if secret.Type != corev1.SecretTypeSSHAuth {
		t.Errorf("expected type kubernetes.io/ssh-auth, got %s", secret.Type)
	}
}

func TestAddGitCredential_InvalidURL_HTTP(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, _ := registerCredSession(t, cs, k8sClient)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "bad-creds",
			"type":           "basic-auth",
			"git_server_url": "http://github.com",
			"username":       "user",
			"password":       "pass",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error for http:// scheme")
	}
}

func TestAddGitCredential_InvalidURL_InternalIP(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, _ := registerCredSession(t, cs, k8sClient)

	for _, url := range []string{
		"https://10.0.0.1",
		"https://192.168.1.1",
		"https://127.0.0.1",
		"https://169.254.169.254",
	} {
		t.Run(url, func(t *testing.T) {
			res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
				Name: "add_git_credential",
				Arguments: map[string]any{
					"session_id":     sid,
					"name":           "bad-creds",
					"type":           "basic-auth",
					"git_server_url": url,
					"username":       "user",
					"password":       "pass",
				},
			})
			if err == nil && (res == nil || !res.IsError) {
				t.Errorf("expected error for internal IP URL %q", url)
			}
		})
	}
}

func TestAddGitCredential_InvalidType(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, _ := registerCredSession(t, cs, k8sClient)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "bad-creds",
			"type":           "bearer",
			"git_server_url": "https://github.com",
			"username":       "user",
			"password":       "pass",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error for invalid type")
	}
}

func TestAddGitCredential_SSH_InvalidKey(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, _ := registerCredSession(t, cs, k8sClient)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "bad-creds",
			"type":           "ssh",
			"git_server_url": "git@github.com",
			"private_key":    "not-a-pem-key",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error for non-PEM SSH key")
	}
}

func TestAddGitCredential_CountLimit(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	// Directly create 20 fake git credential secrets to hit the limit.
	for i := 0; i < 20; i++ {
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("cred-%02d", i),
				Namespace: namespace,
				Labels:    map[string]string{iafk8s.LabelCredentialType: "git"},
			},
			Type: corev1.SecretTypeBasicAuth,
		}
		if err := k8sClient.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "one-too-many",
			"type":           "basic-auth",
			"git_server_url": "https://github.com",
			"username":       "user",
			"password":       "pass",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error when exceeding credential limit")
	}
	errText := res.Content[0].(*gomcp.TextContent).Text
	if !strings.Contains(errText, "limit") {
		t.Errorf("expected 'limit' in error message, got: %s", errText)
	}
}

func TestListGitCredentials_NoCredentialMaterial(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	// Add a credential first.
	_, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "my-creds",
			"type":           "basic-auth",
			"git_server_url": "https://github.com",
			"username":       "secretuser",
			"password":       "secretpass",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = namespace

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "list_git_credentials",
		Arguments: map[string]any{"session_id": sid},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(*gomcp.TextContent).Text)
	}

	text := res.Content[0].(*gomcp.TextContent).Text

	// Credential material must not appear in the listing.
	if strings.Contains(text, "secretuser") {
		t.Error("username must not appear in list output")
	}
	if strings.Contains(text, "secretpass") {
		t.Error("password must not appear in list output")
	}
	if strings.Contains(text, "stringData") || strings.Contains(text, "data") {
		t.Error("raw secret data must not appear in list output")
	}

	var out map[string]any
	json.Unmarshal([]byte(text), &out)
	if out["total"].(float64) != 1 {
		t.Errorf("expected 1 credential, got %v", out["total"])
	}
	creds := out["credentials"].([]any)
	cred := creds[0].(map[string]any)
	if cred["name"] != "my-creds" {
		t.Errorf("expected name 'my-creds', got %v", cred["name"])
	}
	if cred["type"] != "basic-auth" {
		t.Errorf("expected type 'basic-auth', got %v", cred["type"])
	}
	if cred["git_server_url"] != "https://github.com" {
		t.Errorf("expected git_server_url 'https://github.com', got %v", cred["git_server_url"])
	}
}

func TestDeleteGitCredential_Success(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	// Add a credential.
	_, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "my-creds",
			"type":           "basic-auth",
			"git_server_url": "https://github.com",
			"username":       "user",
			"password":       "pass",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "delete_git_credential",
		Arguments: map[string]any{"session_id": sid, "name": "my-creds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(*gomcp.TextContent).Text)
	}

	// Secret must be gone.
	secret := &corev1.Secret{}
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "my-creds"}, secret)
	if err == nil {
		t.Error("expected secret to be deleted")
	}

	// SA secrets list must no longer contain the credential.
	sa := &corev1.ServiceAccount{}
	k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: iafk8s.KpackServiceAccount}, sa)
	for _, ref := range sa.Secrets {
		if ref.Name == "my-creds" {
			t.Error("expected secret to be removed from kpack SA secrets list")
		}
	}
}

func TestDeleteGitCredential_LabelCheck(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	// Create a secret WITHOUT the git credential label (simulates an arbitrary secret).
	arbitrary := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "not-a-credential",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
	}
	if err := k8sClient.Create(ctx, arbitrary); err != nil {
		t.Fatal(err)
	}

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "delete_git_credential",
		Arguments: map[string]any{"session_id": sid, "name": "not-a-credential"},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error when trying to delete a non-credential secret")
	}
}

func TestDeleteGitCredential_NotFound(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, _ := registerCredSession(t, cs, k8sClient)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "delete_git_credential",
		Arguments: map[string]any{"session_id": sid, "name": "nonexistent"},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error for nonexistent credential")
	}
}

func TestDeployApp_WithGitCredential(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	// Add a credential.
	_, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "add_git_credential",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "my-creds",
			"type":           "basic-auth",
			"git_server_url": "https://github.com",
			"username":       "user",
			"password":       "pass",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = namespace

	// Deploy with the credential — should succeed.
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "my-app",
			"git_url":        "https://github.com/example/repo",
			"git_credential": "my-creds",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("deploy_app with valid credential failed: %s", res.Content[0].(*gomcp.TextContent).Text)
	}
}

func TestDeployApp_WithNonexistentGitCredential(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, _ := registerCredSession(t, cs, k8sClient)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "my-app",
			"git_url":        "https://github.com/example/repo",
			"git_credential": "nonexistent",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error when git_credential does not exist")
	}
}

func TestDeployApp_WithArbitrarySecretAsCredential(t *testing.T) {
	cs, _, k8sClient := setupCredToolServer(t)
	ctx := context.Background()
	sid, namespace := registerCredSession(t, cs, k8sClient)

	// Create a secret without the git credential label.
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "other-secret", Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
	}
	k8sClient.Create(ctx, s)

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "deploy_app",
		Arguments: map[string]any{
			"session_id":     sid,
			"name":           "my-app",
			"git_url":        "https://github.com/example/repo",
			"git_credential": "other-secret",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error when using a non-credential secret as git_credential")
	}
}
