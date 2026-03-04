package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	iafmcp "github.com/dlapiduz/iaf/internal/mcp"
	"github.com/dlapiduz/iaf/internal/mcp/coach"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// waitForCount polls cs.ListPrompts until at least minCount prompts appear or
// the deadline is exceeded. Used to account for the background registration goroutine.
func waitForPromptCount(t *testing.T, cs *gomcp.ClientSession, minCount int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := cs.ListPrompts(context.Background(), nil)
		if err == nil && len(res.Prompts) >= minCount {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d prompts", minCount)
}

func waitForResourceCount(t *testing.T, cs *gomcp.ClientSession, minCount int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := cs.ListResources(context.Background(), nil)
		if err == nil && len(res.Resources) >= minCount {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d resources", minCount)
}

// TestRegisterCoachProxy_Unreachable verifies that an unreachable coach URL
// causes graceful degradation (no error returned, platform continues).
func TestRegisterCoachProxy_Unreachable(t *testing.T) {
	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)

	err := iafmcp.RegisterCoachProxy(
		context.Background(),
		server,
		"http://127.0.0.1:19999/mcp", // nothing listening here
		"test-token",
	)
	if err != nil {
		t.Errorf("expected nil error for unreachable coach (graceful degradation), got: %v", err)
	}
}

// TestRegisterCoachProxy_EmptyURL verifies that an empty URL is handled safely
// (caller is expected to guard with cfg.CoachURL != "", but proxy should not panic).
func TestRegisterCoachProxy_ForwardsPrompts(t *testing.T) {
	// Spin up a real in-process coach MCP server exposed via streamable HTTP.
	coachDeps := &coach.Dependencies{
		BaseDomain:   "test.example.com",
		OrgStandards: orgstandards.New("", nil),
	}
	coachServer := coach.NewServer(coachDeps)

	httpHandler := gomcp.NewStreamableHTTPHandler(func(r *http.Request) *gomcp.Server {
		return coachServer
	}, &gomcp.StreamableHTTPOptions{Stateless: true})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate Bearer token
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		httpHandler.ServeHTTP(w, r)
	}))
	defer ts.Close()

	// Platform MCP server starts empty.
	platformServer := gomcp.NewServer(&gomcp.Implementation{Name: "platform", Version: "0.0.1"}, nil)

	err := iafmcp.RegisterCoachProxy(context.Background(), platformServer, ts.URL+"/mcp", "test-secret")
	if err != nil {
		t.Fatalf("RegisterCoachProxy returned error: %v", err)
	}

	// Connect a client to the platform server and verify coach prompts are visible.
	st, ct := gomcp.NewInMemoryTransports()
	if _, err := platformServer.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	c := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := c.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	// Background goroutine may still be registering — poll until prompts appear.
	waitForPromptCount(t, cs, 1)

	res, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Prompts) == 0 {
		t.Error("expected coach prompts to be forwarded to platform server, got none")
	}

	// Verify known coach prompts are present.
	names := map[string]bool{}
	for _, p := range res.Prompts {
		names[p.Name] = true
	}
	for _, expected := range []string{"language-guide", "coding-guide", "logging-guide"} {
		if !names[expected] {
			t.Errorf("expected forwarded prompt %q, not found in platform listing", expected)
		}
	}
}

func TestRegisterCoachProxy_ForwardsResources(t *testing.T) {
	coachDeps := &coach.Dependencies{
		BaseDomain:   "test.example.com",
		OrgStandards: orgstandards.New("", nil),
	}
	coachServer := coach.NewServer(coachDeps)

	httpHandler := gomcp.NewStreamableHTTPHandler(func(r *http.Request) *gomcp.Server {
		return coachServer
	}, &gomcp.StreamableHTTPOptions{Stateless: true})

	ts := httptest.NewServer(httpHandler)
	defer ts.Close()

	platformServer := gomcp.NewServer(&gomcp.Implementation{Name: "platform", Version: "0.0.1"}, nil)

	if err := iafmcp.RegisterCoachProxy(context.Background(), platformServer, ts.URL+"/mcp", ""); err != nil {
		t.Fatalf("RegisterCoachProxy returned error: %v", err)
	}

	st, ct := gomcp.NewInMemoryTransports()
	if _, err := platformServer.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	c := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := c.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	// Background goroutine may still be registering — poll until resources appear.
	waitForResourceCount(t, cs, 1)

	res, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) == 0 {
		t.Error("expected coach resources to be forwarded to platform server, got none")
	}

	names := map[string]bool{}
	for _, r := range res.Resources {
		names[r.Name] = true
	}
	if !names["org-coding-standards"] {
		t.Error("expected 'org-coding-standards' resource forwarded from coach")
	}
}
