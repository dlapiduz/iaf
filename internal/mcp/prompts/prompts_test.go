package prompts_test

import (
	"context"
	"strings"
	"testing"

	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/mcp/prompts"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/orgstandards"
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
		BaseDomain:   "test.example.com",
		OrgStandards: orgstandards.New("", nil),
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	prompts.RegisterDeployGuide(server, deps)
	prompts.RegisterLanguageGuide(server, deps)
	prompts.RegisterCodingGuide(server, deps)
	prompts.RegisterScaffoldGuide(server, deps)
	prompts.RegisterServicesGuide(server, deps)
	prompts.RegisterLoggingGuide(server, deps)
	prompts.RegisterMetricsGuide(server, deps)
	prompts.RegisterTracingGuide(server, deps)

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
	prompts.RegisterLanguageGuide(server, deps)
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

func TestLanguageGuide_AllLanguages(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	languages := []string{"go", "nodejs", "python", "java", "ruby"}
	for _, lang := range languages {
		t.Run(lang, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "language-guide",
				Arguments: map[string]string{"language": lang},
			})
			if err != nil {
				t.Fatal(err)
			}

			if len(res.Messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(res.Messages))
			}

			text := res.Messages[0].Content.(*gomcp.TextContent).Text

			for _, section := range []string{"Buildpack", "Detection Files", "Required Files", "Minimal Example", "Best Practices"} {
				if !strings.Contains(text, section) {
					t.Errorf("expected section %q in %s guide", section, lang)
				}
			}

			if !strings.Contains(text, "test.example.com") {
				t.Error("expected base domain in language guide")
			}
		})
	}
}

func TestLanguageGuide_Aliases(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	aliases := map[string]string{
		"golang":     "Go",
		"node":       "Nodejs",
		"node.js":    "Nodejs",
		"js":         "Nodejs",
		"javascript": "Nodejs",
		"py":         "Python",
		"python3":    "Python",
		"rb":         "Ruby",
	}

	for alias, expectedTitle := range aliases {
		t.Run(alias, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "language-guide",
				Arguments: map[string]string{"language": alias},
			})
			if err != nil {
				t.Fatal(err)
			}

			text := res.Messages[0].Content.(*gomcp.TextContent).Text
			if !strings.Contains(text, expectedTitle+" Application Guide") {
				t.Errorf("alias %q: expected title containing %q, got text starting with %q", alias, expectedTitle, text[:80])
			}
		})
	}
}

func TestLanguageGuide_CaseInsensitive(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	for _, input := range []string{"GO", "Go", "gO", "PYTHON", "Python"} {
		t.Run(input, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "language-guide",
				Arguments: map[string]string{"language": input},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(res.Messages))
			}
		})
	}
}

func TestLanguageGuide_UnsupportedLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	_, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "language-guide",
		Arguments: map[string]string{"language": "cobol"},
	})
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("expected 'unsupported language' in error, got: %v", err)
	}
}

func TestListPrompts(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Prompts) != 8 {
		t.Fatalf("expected 8 prompts, got %d", len(res.Prompts))
	}

	names := map[string]bool{}
	for _, p := range res.Prompts {
		names[p.Name] = true
	}
	for _, expected := range []string{"deploy-guide", "language-guide", "coding-guide", "scaffold-guide", "services-guide", "logging-guide", "metrics-guide", "tracing-guide"} {
		if !names[expected] {
			t.Errorf("expected prompt %q in listing", expected)
		}
	}
}

func TestCodingGuide_NoLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "coding-guide",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(res.Messages))
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text

	for _, section := range []string{"Platform Defaults", "Best Practices", "Full Standards Reference", "iaf://org/coding-standards"} {
		if !strings.Contains(text, section) {
			t.Errorf("expected section %q in coding-guide text", section)
		}
	}
}

func TestCodingGuide_WithLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "coding-guide",
		Arguments: map[string]string{"language": "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text

	// Global standards should still be present.
	if !strings.Contains(text, "Platform Defaults") {
		t.Error("expected 'Platform Defaults' section in coding-guide with language")
	}
	// Per-language section should be present.
	if !strings.Contains(text, "Go-Specific Standards") {
		t.Error("expected 'Go-Specific Standards' section when language=go")
	}
}

func TestCodingGuide_UnknownLanguage_ReturnsGlobalStandards(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	// Unknown language should not return an error — just global standards.
	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "coding-guide",
		Arguments: map[string]string{"language": "cobol"},
	})
	if err != nil {
		t.Fatalf("unexpected error for unknown language: %v", err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	if !strings.Contains(text, "Platform Defaults") {
		t.Error("expected global standards even for unknown language")
	}
	// No per-language section should appear.
	if strings.Contains(text, "Cobol-Specific Standards") {
		t.Error("did not expect language-specific section for unknown language")
	}
}

func TestCodingGuide_LanguageAlias(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "coding-guide",
		Arguments: map[string]string{"language": "golang"},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	if !strings.Contains(text, "Go-Specific Standards") {
		t.Error("expected 'Go-Specific Standards' for alias 'golang'")
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

func TestScaffoldGuide_WithFramework(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	for _, framework := range []string{"nextjs", "html"} {
		t.Run(framework, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "scaffold-guide",
				Arguments: map[string]string{"framework": framework},
			})
			if err != nil {
				t.Fatal(err)
			}

			if len(res.Messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(res.Messages))
			}

			text := res.Messages[0].Content.(*gomcp.TextContent).Text

			if !strings.Contains(text, "iaf://scaffold/"+framework) {
				t.Errorf("expected scaffold URI in text for framework %q", framework)
			}
			if !strings.Contains(text, "push_code") {
				t.Error("expected 'push_code' instructions in scaffold-guide")
			}
			if !strings.Contains(text, "test.example.com") {
				t.Error("expected base domain in scaffold-guide")
			}
		})
	}
}

func TestScaffoldGuide_NoFramework(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "scaffold-guide",
	})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text

	// Both scaffolds should be described.
	for _, uri := range []string{"iaf://scaffold/nextjs", "iaf://scaffold/html"} {
		if !strings.Contains(text, uri) {
			t.Errorf("expected %q in scaffold-guide with no framework argument", uri)
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

func TestLoggingGuide_Overview(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{Name: "logging-guide"})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	for _, s := range []string{"JSON Lines", "stdout", "Required Fields", "iaf://org/logging-standards"} {
		if !strings.Contains(text, s) {
			t.Errorf("expected %q in logging-guide overview", s)
		}
	}
}

func TestLoggingGuide_PerLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	libs := map[string]string{
		"go":     "slog",
		"nodejs": "pino",
		"python": "python-json-logger",
		"java":   "logback",
		"ruby":   "semantic_logger",
	}
	for lang, expectedLib := range libs {
		t.Run(lang, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "logging-guide",
				Arguments: map[string]string{"language": lang},
			})
			if err != nil {
				t.Fatal(err)
			}
			text := res.Messages[0].Content.(*gomcp.TextContent).Text
			if !strings.Contains(text, expectedLib) {
				t.Errorf("expected library %q in %s logging guide", expectedLib, lang)
			}
			if !strings.Contains(text, "iaf://org/logging-standards") {
				t.Errorf("expected standards reference in %s logging guide", lang)
			}
		})
	}
}

func TestLoggingGuide_UnsupportedLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	_, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "logging-guide",
		Arguments: map[string]string{"language": "cobol"},
	})
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
}

func TestMetricsGuide_Overview(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{Name: "metrics-guide"})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	for _, s := range []string{"/metrics", "http_requests_total", "RED", "iaf://org/metrics-standards"} {
		if !strings.Contains(text, s) {
			t.Errorf("expected %q in metrics-guide overview", s)
		}
	}
}

func TestMetricsGuide_PerLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	libs := map[string]string{
		"go":     "prometheus/client_golang",
		"nodejs": "prom-client",
		"python": "prometheus_client",
		"java":   "micrometer",
		"ruby":   "prometheus-client",
	}
	for lang, expectedLib := range libs {
		t.Run(lang, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "metrics-guide",
				Arguments: map[string]string{"language": lang},
			})
			if err != nil {
				t.Fatal(err)
			}
			text := res.Messages[0].Content.(*gomcp.TextContent).Text
			if !strings.Contains(text, expectedLib) {
				t.Errorf("expected library %q in %s metrics guide", expectedLib, lang)
			}
		})
	}
}

func TestTracingGuide_Overview(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{Name: "tracing-guide"})
	if err != nil {
		t.Fatal(err)
	}

	text := res.Messages[0].Content.(*gomcp.TextContent).Text
	for _, s := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "Auto-Instrumentation", "trace_id", "iaf://org/tracing-standards"} {
		if !strings.Contains(text, s) {
			t.Errorf("expected %q in tracing-guide overview", s)
		}
	}
}

func TestTracingGuide_PerLanguage(t *testing.T) {
	cs := setupServer(t)
	ctx := context.Background()

	tests := []struct {
		lang          string
		expectedLib   string
		autoAvailable bool
	}{
		{"go", "go.opentelemetry.io/otel", false},
		{"nodejs", "@opentelemetry", true},
		{"python", "opentelemetry-sdk", true},
		{"java", "opentelemetry-sdk", true},
		{"ruby", "opentelemetry-sdk", true},
	}
	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			res, err := cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      "tracing-guide",
				Arguments: map[string]string{"language": tc.lang},
			})
			if err != nil {
				t.Fatal(err)
			}
			text := res.Messages[0].Content.(*gomcp.TextContent).Text
			if !strings.Contains(text, tc.expectedLib) {
				t.Errorf("expected library %q in %s tracing guide", tc.expectedLib, tc.lang)
			}
			if tc.autoAvailable && !strings.Contains(text, "Auto-Instrumentation") {
				t.Errorf("expected auto-instrumentation section for %s", tc.lang)
			}
		})
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
