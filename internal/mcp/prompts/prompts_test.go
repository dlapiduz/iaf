package prompts_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dlapiduz/iaf/internal/mcp/prompts"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupServer(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	deps := &tools.Dependencies{
		BaseDomain: "test.example.com",
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	prompts.RegisterDeployGuide(server, deps)
	prompts.RegisterLanguageGuide(server, deps)

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

	if len(res.Prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(res.Prompts))
	}

	names := map[string]bool{}
	for _, p := range res.Prompts {
		names[p.Name] = true
	}
	for _, expected := range []string{"deploy-guide", "language-guide"} {
		if !names[expected] {
			t.Errorf("expected prompt %q in listing", expected)
		}
	}
}
