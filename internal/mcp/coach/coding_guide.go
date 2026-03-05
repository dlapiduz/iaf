package coach

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterCodingGuide registers the coding-guide prompt that provides
// organisation coding standards, optionally merged with per-language guidance.
// Unknown language values return global standards without error.
func RegisterCodingGuide(server *gomcp.Server, deps *Dependencies) {
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

				if len(langStd.ApprovedFrameworks) > 0 {
					sb.WriteString("\n### Approved Frameworks\n")
					for _, f := range langStd.ApprovedFrameworks {
						line := fmt.Sprintf("- **%s**", f.Name)
						if f.MinVersion != "" {
							line += fmt.Sprintf(" (>= %s)", f.MinVersion)
						}
						if f.Notes != "" {
							line += fmt.Sprintf(" — %s", f.Notes)
						}
						sb.WriteString(line + "\n")
					}
				}

				if len(langStd.ProhibitedFrameworks) > 0 {
					sb.WriteString("\n### Prohibited Frameworks\n")
					for _, f := range langStd.ProhibitedFrameworks {
						line := fmt.Sprintf("- ~~%s~~", f.Name)
						if f.Reason != "" {
							line += fmt.Sprintf(" — %s", f.Reason)
						}
						sb.WriteString(line + "\n")
					}
				}

				if len(langStd.ApprovedLibraries) > 0 {
					sb.WriteString("\n### Approved Libraries\n")
					for _, lib := range langStd.ApprovedLibraries {
						line := fmt.Sprintf("- **%s**", lib.Name)
						if lib.MinVersion != "" {
							line += fmt.Sprintf(" (>= %s)", lib.MinVersion)
						}
						if lib.Category != "" {
							line += fmt.Sprintf(" [%s]", lib.Category)
						}
						if lib.Notes != "" {
							line += fmt.Sprintf(" — %s", lib.Notes)
						}
						sb.WriteString(line + "\n")
					}
				}

				if len(langStd.ProhibitedLibraries) > 0 {
					sb.WriteString("\n### Prohibited Libraries\n")
					for _, lib := range langStd.ProhibitedLibraries {
						line := fmt.Sprintf("- ~~%s~~", lib.Name)
						if lib.Reason != "" {
							line += fmt.Sprintf(" — %s", lib.Reason)
						}
						sb.WriteString(line + "\n")
					}
				}
			}
		}

		sb.WriteString("\n## IAF Platform Requirements\n")
		sb.WriteString(`These rules are mandatory for all apps deployed on IAF:

**1. Write code to disk first**
Always write your source files to the local filesystem before calling push_code.
Do not pass code as inline strings. Agents should create a project directory, write all files, then upload.

**2. No in-memory state**
IAF apps are ephemeral — pods restart and lose all in-memory data (arrays, maps, global variables).
Use a persistent backing service instead:
- Call ` + "`provision_service`" + ` to create a managed PostgreSQL database
- Call ` + "`list_data_sources`" + ` to find pre-provisioned databases or APIs
- Call ` + "`attach_data_source`" + ` / ` + "`bind_service`" + ` to inject credentials as env vars

**3. 12-factor app principles**
- Config from env vars only (never hardcoded)
- Stateless processes — no local filesystem state between requests
- Explicit dependency manifests (package.json, go.mod, requirements.txt, Gemfile)
- Log to stdout in structured JSON (see logging-guide)

**4. Version control**
If ` + "`setup_github_repo`" + ` is available, create a repo and commit your code before deploying.
This provides a history, enables collaboration, and makes rollbacks possible.

**5. Authentication**
Required on any route that modifies data or accesses sensitive information.
Optional on read-only public endpoints — do not add unnecessary auth to health checks or public views.
`)

		sb.WriteString("\n## Security\n")
		sb.WriteString("Read `security-guide` (with optional `language` argument) for SQL injection prevention,\n")
		sb.WriteString("secret handling, and OWASP Top 10 guidance specific to your stack.\n")
		sb.WriteString("Read `iaf://org/security-standards` for the machine-readable security standard.\n")

		sb.WriteString("\n## Open-Source License Policy\n")
		sb.WriteString("Before adding any dependency, verify its SPDX license identifier is on the approved list.\n")
		sb.WriteString("Read `license-guide` for the check-before-add workflow and approved/prohibited license list.\n")
		sb.WriteString("Read `iaf://org/license-policy` for the machine-readable license policy.\n")

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
