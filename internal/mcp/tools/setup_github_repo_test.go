package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// setupGitHubServer builds an MCP server with setup_github_repo wired to the
// given MockClient. It also registers the register tool so tests can get a
// valid session_id. Returns the client session and session store.
func setupGitHubServer(t *testing.T, mock *iafgithub.MockClient) (*gomcp.ClientSession, *auth.SessionStore) {
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
		Client:      k8sClient,
		Store:       store,
		BaseDomain:  "test.example.com",
		Sessions:    sessions,
		GitHub:      mock,
		GitHubOrg:   "test-org",
		GitHubToken: "test-token",
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	tools.RegisterRegisterTool(server, deps)
	tools.RegisterSetupGithubRepo(server, deps)

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

// registerSession calls the register tool and returns the session_id.
func registerSession(t *testing.T, cs *gomcp.ClientSession) string {
	t.Helper()
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "register",
		Arguments: map[string]any{"name": "test-agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result); err != nil {
		t.Fatal(err)
	}
	return result["session_id"].(string)
}

func TestSetupGithubRepo_Success(t *testing.T) {
	mock := &iafgithub.MockClient{}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "my-app",
			"visibility": "private",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content[0].(*gomcp.TextContent).Text)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result); err != nil {
		t.Fatalf("failed to parse JSON result: %v", err)
	}

	if result["clone_url"] == "" || result["clone_url"] == nil {
		t.Error("expected clone_url in result")
	}
	if result["html_url"] == "" || result["html_url"] == nil {
		t.Error("expected html_url in result")
	}
	if result["branch_protection_applied"] != true {
		t.Error("expected branch_protection_applied=true")
	}
	if result["ci_workflow_committed"] != true {
		t.Error("expected ci_workflow_committed=true")
	}
	if _, ok := result["warnings"]; ok {
		t.Errorf("expected no warnings, got: %v", result["warnings"])
	}
}

func TestSetupGithubRepo_DefaultsToPrivate(t *testing.T) {
	var capturedPrivate bool
	mock := &iafgithub.MockClient{
		CreateRepoFn: func(_ context.Context, org, name string, private bool) (*iafgithub.RepoInfo, error) {
			capturedPrivate = private
			return &iafgithub.RepoInfo{
				Name:     name,
				CloneURL: "https://github.com/" + org + "/" + name + ".git",
				HTMLURL:  "https://github.com/" + org + "/" + name,
				Private:  private,
			}, nil
		},
	}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	// No visibility field — should default to private.
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "my-app",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error")
	}
	if !capturedPrivate {
		t.Error("expected default visibility to be private")
	}
}

func TestSetupGithubRepo_PublicVisibility(t *testing.T) {
	var capturedPrivate bool
	mock := &iafgithub.MockClient{
		CreateRepoFn: func(_ context.Context, org, name string, private bool) (*iafgithub.RepoInfo, error) {
			capturedPrivate = private
			return &iafgithub.RepoInfo{
				Name:     name,
				CloneURL: "https://github.com/" + org + "/" + name + ".git",
				HTMLURL:  "https://github.com/" + org + "/" + name,
				Private:  private,
			}, nil
		},
	}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "open-source-app",
			"visibility": "public",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error")
	}
	if capturedPrivate {
		t.Error("expected visibility=public to create a non-private repo")
	}
}

func TestSetupGithubRepo_BranchProtectionFails_Warning(t *testing.T) {
	mock := &iafgithub.MockClient{
		SetBranchProtectionFn: func(_ context.Context, _, _, _ string, _ iafgithub.BranchProtectionConfig) error {
			return errors.New("branch protection unavailable on free plan")
		},
	}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "my-app",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal("branch protection failure should be a warning, not a hard error")
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result); err != nil {
		t.Fatal(err)
	}
	if result["branch_protection_applied"] != false {
		t.Error("expected branch_protection_applied=false on failure")
	}
	warnings, _ := result["warnings"].([]any)
	if len(warnings) == 0 {
		t.Error("expected at least one warning when branch protection fails")
	}
	// CI workflow step should still succeed.
	if result["ci_workflow_committed"] != true {
		t.Error("expected ci_workflow_committed=true even when branch protection fails")
	}
}

func TestSetupGithubRepo_CICommitFails_Warning(t *testing.T) {
	mock := &iafgithub.MockClient{
		CreateFileFn: func(_ context.Context, _, _, _, _ string, _ []byte) error {
			return errors.New("file write error")
		},
	}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "my-app",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal("CI commit failure should be a warning, not a hard error")
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &result); err != nil {
		t.Fatal(err)
	}
	if result["ci_workflow_committed"] != false {
		t.Error("expected ci_workflow_committed=false on failure")
	}
	warnings, _ := result["warnings"].([]any)
	if len(warnings) == 0 {
		t.Error("expected at least one warning when CI commit fails")
	}
}

func TestSetupGithubRepo_CreateRepoFails_Error(t *testing.T) {
	mock := &iafgithub.MockClient{
		CreateRepoFn: func(_ context.Context, _, name string, _ bool) (*iafgithub.RepoInfo, error) {
			return nil, errors.New(`repository "my-app" already exists in org "test-org" — choose a different name`)
		},
	}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "my-app",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected hard error when CreateRepo fails")
	}
}

func TestSetupGithubRepo_InvalidRepoName(t *testing.T) {
	mock := &iafgithub.MockClient{}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	cases := []string{
		"",                         // empty
		"../secret",                // path traversal
		"a b",                      // space
		strings.Repeat("a", 101),   // too long
	}

	for _, name := range cases {
		res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
			Name: "setup_github_repo",
			Arguments: map[string]any{
				"session_id": sessionID,
				"repo_name":  name,
			},
		})
		if err == nil && (res == nil || !res.IsError) {
			t.Errorf("expected error for repo_name %q", name)
		}
	}
}

func TestSetupGithubRepo_NoSession(t *testing.T) {
	mock := &iafgithub.MockClient{}
	cs, _ := setupGitHubServer(t, mock)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"repo_name": "my-app",
		},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected error for missing session_id")
	}
}

func TestSetupGithubRepo_StatusChecksApplied(t *testing.T) {
	var capturedChecks []string
	mock := &iafgithub.MockClient{
		SetBranchProtectionFn: func(_ context.Context, _, _, _ string, cfg iafgithub.BranchProtectionConfig) error {
			capturedChecks = cfg.RequiredStatusChecks
			return nil
		},
	}
	cs, _ := setupGitHubServer(t, mock)
	sessionID := registerSession(t, cs)
	ctx := context.Background()

	_, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "setup_github_repo",
		Arguments: map[string]any{
			"session_id": sessionID,
			"repo_name":  "my-app",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(capturedChecks) != 1 || capturedChecks[0] != "CI / ci" {
		t.Errorf("expected required status check 'CI / ci', got %v", capturedChecks)
	}
}
