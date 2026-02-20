package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	iafgithub "github.com/dlapiduz/iaf/internal/github"
)

// newTestClient creates an HTTPClient pointed at the given test server.
func newTestClient(t *testing.T, token, serverURL string) *iafgithub.HTTPClient {
	t.Helper()
	c := iafgithub.NewHTTPClient(token)
	c.SetBaseURL(serverURL)
	return c
}

func TestHTTPClient_CreateRepo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/repos") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Verify Authorization header present but do not log the token.
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["auto_init"] != true {
			t.Error("expected auto_init=true in request body")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"name":      "my-repo",
			"clone_url": "https://github.com/my-org/my-repo.git",
			"html_url":  "https://github.com/my-org/my-repo",
			"private":   true,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, "test-token", srv.URL)
	info, err := c.CreateRepo(context.Background(), "my-org", "my-repo", true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "my-repo" {
		t.Errorf("expected name 'my-repo', got %q", info.Name)
	}
	if info.CloneURL == "" {
		t.Error("expected non-empty clone_url")
	}
}

func TestHTTPClient_CreateRepo_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"message": "Repository creation failed."})
	}))
	defer srv.Close()

	c := newTestClient(t, "test-token", srv.URL)
	_, err := c.CreateRepo(context.Background(), "my-org", "my-repo", false)
	if err == nil {
		t.Fatal("expected error for 422")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %v", err)
	}
	// Token must not appear in error message.
	if strings.Contains(err.Error(), "test-token") {
		t.Error("token leaked in error message")
	}
}

func TestHTTPClient_SetBranchProtection_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"url": "https://api.github.com/..."})
	}))
	defer srv.Close()

	c := newTestClient(t, "test-token", srv.URL)
	err := c.SetBranchProtection(context.Background(), "my-org", "my-repo", "main", iafgithub.BranchProtectionConfig{
		RequiredReviewers:    1,
		RequiredStatusChecks: []string{"CI / ci"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClient_CreateFile_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"content": "ok"})
	}))
	defer srv.Close()

	c := newTestClient(t, "test-token", srv.URL)
	err := c.CreateFile(context.Background(), "my-org", "my-repo", ".github/workflows/ci.yml", "Add CI", []byte("name: CI"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClient_APIError_TokenNotLeaked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"message": "Resource not accessible by integration"})
	}))
	defer srv.Close()

	c := newTestClient(t, "secret-token", srv.URL)
	err := c.SetBranchProtection(context.Background(), "my-org", "my-repo", "main", iafgithub.BranchProtectionConfig{})
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Error("token leaked in error message")
	}
}
