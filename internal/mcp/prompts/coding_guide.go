package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterCodingGuide registers the coding-guide prompt that provides
// organisation coding standards, optionally merged with per-language guidance.
// Unknown language values return global standards without error.
func RegisterCodingGuide(server *gomcp.Server, deps *tools.Dependencies) {
	loader := deps.OrgStandards
	if loader == nil {
		loader = orgstandards.New("", nil)
	}

	server.AddPrompt(&gomcp.Prompt{
		Name:        "coding-guide",
		Description: "Organisation coding standards and best practices, optionally merged with per-language guidance. Read iaf://org/coding-standards for the machine-readable version.",
		Arguments: []*gomcp.PromptArgument{
			{
				Name:        "language",
				Description: "Optional language (go, nodejs, python, java, ruby). Aliases like 'golang', 'node', 'py', 'rb' are accepted. Omit for global standards only.",
				Required:    false,
			},
		},
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		s := loader.Get()

		lang := strings.ToLower(strings.TrimSpace(req.Params.Arguments["language"]))
		// Canonicalize using the same alias map as language-guide.
		canonical := languageAliases[lang]

		var sb strings.Builder
		sb.WriteString("# Organisation Coding Standards\n\n")

		sb.WriteString("## Platform Defaults\n")
		sb.WriteString(fmt.Sprintf("- Health-check path: `%s`\n", s.HealthCheckPath))
		sb.WriteString(fmt.Sprintf("- Logging format: `%s`\n", s.LoggingFormat))
		sb.WriteString(fmt.Sprintf("- Default port: `%d`\n", s.DefaultPort))
		sb.WriteString(fmt.Sprintf("- Env var naming: `%s`\n", s.EnvVarNaming))

		if len(s.RequiredEnvVars) > 0 {
			sb.WriteString("\n## Required Environment Variables\n")
			for _, v := range s.RequiredEnvVars {
				sb.WriteString(fmt.Sprintf("- `%s`\n", v))
			}
		}

		if len(s.BestPractices) > 0 {
			sb.WriteString("\n## Best Practices\n")
			for _, p := range s.BestPractices {
				sb.WriteString(fmt.Sprintf("- %s\n", p))
			}
		}

		// Per-language section — only when a recognised language is supplied.
		if canonical != "" {
			if langStd, ok := s.PerLanguage[canonical]; ok {
				title := strings.ToUpper(canonical[:1]) + canonical[1:]
				sb.WriteString(fmt.Sprintf("\n## %s-Specific Standards\n", title))
				if len(langStd.Notes) > 0 {
					sb.WriteString("\n### Notes\n")
					for _, n := range langStd.Notes {
						sb.WriteString(fmt.Sprintf("- %s\n", n))
					}
				}
				if len(langStd.ApprovedLibraries) > 0 {
					sb.WriteString("\n### Approved Libraries\n")
					for category, lib := range langStd.ApprovedLibraries {
						sb.WriteString(fmt.Sprintf("- **%s**: %s\n", category, lib))
					}
				}
			}
		}

		sb.WriteString("\n## Full Standards Reference\n")
		sb.WriteString("Read `iaf://org/coding-standards` for the machine-readable JSON version of these standards.\n")

		return &gomcp.GetPromptResult{
			Description: "Organisation coding standards and best practices.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: sb.String()},
				},
			},
		}, nil
	})
}
