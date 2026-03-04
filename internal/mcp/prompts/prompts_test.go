package prompts_test

import (
	"context"
	"strings"
	"testing"

	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/mcp/prompts"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func connectServer(t *testing.T, ctx context.Context, server *gomcp.Server) *gomcp.ClientSession {
	t.Helper()
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
	return cs
}

func setupServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	deps := &tools.Dependencies{
		BaseDomain: "test.example.com",
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	prompts.RegisterDeployGuide(server, deps)
	prompts.RegisterServicesGuide(server, deps)

	return connectServer(t, ctx, server)
}

func setupGitHubPromptServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	deps := &tools.Dependencies{
		BaseDomain: "test.example.com",
		GitHub:     &iafgithub.MockClient{},
		GitHubOrg:  "test-org",
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	prompts.RegisterDeployGuide(server, deps)
	prompts.RegisterGitHubGuide(server, deps)

	return connectServer(t, ctx, server)
}

func TestDeployGuide(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "deploy-guide",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(res.Messages))
	}

	msg := res.Messages[0]
	if msg.Role != "user" {
		t.Errorf("expected role 'user', got %q", msg.Role)
	}

	text := msg.Content.(*gomcp.TextContent).Text

	// Verify dynamic values are injected
	if !strings.Contains(text, "register") {
		t.Error("expected 'register' instruction in deploy guide text")
	}
	if !strings.Contains(text, "session_id") {
		t.Error("expected 'session_id' in deploy guide text")
	}
	if !strings.Contains(text, "test.example.com") {
		t.Error("expected base domain 'test.example.com' in deploy guide text")
	}

	// Verify key content sections
	for _, section := range []string{
		"Deployment Methods",
		"Application Lifecycle Phases",
		"Naming Constraints",
		"Security Considerations",
		"Recommended Workflow",
	} {
		if !strings.Contains(text, section) {
			t.Errorf("expected section %q in deploy guide", section)
		}
	}
}

func TestListPrompts(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(res.Prompts))
	}

	names := map[string]bool{}
	for _, p := range res.Prompts {
		names[p.Name] = true
	}
	for _, expected := range []string{"deploy-guide", "services-guide"} {
		if !names[expected] {
			t.Errorf("expected prompt %q in listing", expected)
		}
	}
}

func TestDeployGuide_MentionsCodingGuide(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "deploy-guide",
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	if !strings.Contains(text, "coding-guide") {
		t.Error("deploy-guide should reference the coding-guide prompt")
	}
	if !strings.Contains(text, "iaf://org/coding-standards") {
		t.Error("deploy-guide should reference iaf://org/coding-standards resource")
	}
}

func TestDeployGuide_MentionsObservability(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{Name: "deploy-guide"})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	for _, ref := range []string{"logging-guide", "metrics-guide", "tracing-guide", "Observability"} {
		if !strings.Contains(text, ref) {
			t.Errorf("deploy-guide should reference %q", ref)
		}
	}
}

func TestGitHubGuide_DefaultWorkflow(t *testing.T) {
	cs := setupGitHubPromptServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "github-guide",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(res.Messages))
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	if !strings.Contains(text, "solo-agent") {
		t.Error("expected default workflow 'solo-agent' in guide text")
	}
	if !strings.Contains(text, "setup_github_repo") {
		t.Error("expected 'setup_github_repo' in guide text")
	}
	if !strings.Contains(text, "deploy_app") {
		t.Error("expected 'deploy_app' in guide text")
	}
	if !strings.Contains(text, "test.example.com") {
		t.Error("expected base domain in guide text")
	}
}

func TestGitHubGuide_MultiAgentWorkflow(t *testing.T) {
	cs := setupGitHubPromptServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "github-guide",
		Arguments: map[string]string{"workflow": "multi-agent"},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	if !strings.Contains(text, "multi-agent") {
		t.Error("expected 'multi-agent' in guide text")
	}
	if !strings.Contains(text, "Agent A") && !strings.Contains(text, "Agent B") {
		t.Error("expected agent collaboration instructions in multi-agent guide")
	}
}

func TestGitHubGuide_HumanReviewWorkflow(t *testing.T) {
	cs := setupGitHubPromptServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "github-guide",
		Arguments: map[string]string{"workflow": "human-review"},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	if !strings.Contains(text, "human") {
		t.Error("expected 'human' in human-review guide text")
	}
}

func TestGitHubGuide_ListedWhenConfigured(t *testing.T) {
	cs := setupGitHubPromptServer(t)
	ctx := context.Background()

	res, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, p := range res.Prompts {
		names[p.Name] = true
	}
	if !names["github-guide"] {
		t.Error("expected 'github-guide' to be listed when GitHub is configured")
	}
}
