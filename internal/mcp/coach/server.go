package coach

import (
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverInstructions = `You are connected to the IAF Coach MCP server — a developer guidance service for building applications on the IAF platform.

AVAILABLE PROMPTS:
- language-guide: Per-language buildpack-compatible app structure (go, nodejs, python, java, ruby)
- coding-guide:   Organisation coding standards and best practices
- scaffold-guide: Get a ready-to-deploy scaffold for a UI framework
- logging-guide:  Per-language structured logging setup
- metrics-guide:  Per-language RED method metrics setup
- tracing-guide:  Per-language OpenTelemetry tracing setup

AVAILABLE RESOURCES:
- iaf://org/coding-standards    — Machine-readable organisation coding standards (JSON)
- iaf://org/logging-standards   — Platform logging standard (JSON)
- iaf://org/metrics-standards   — Platform metrics standard (JSON)
- iaf://org/tracing-standards   — Platform tracing standard (JSON)
- iaf://languages/{language}    — Structured language specification (JSON)
- iaf://scaffold/{framework}    — UI scaffold file map (nextjs or html) ready for push_code`

// NewServer creates and configures the coach MCP server.
// If deps.OrgStandards is nil, a default (empty) loader is used.
func NewServer(deps *Dependencies) *gomcp.Server {
	if deps.OrgStandards == nil {
		deps.OrgStandards = orgstandards.New("", nil)
	}

	server := gomcp.NewServer(
		&gomcp.Implementation{
			Name:    "iaf-coach",
			Version: "0.1.0",
		},
		&gomcp.ServerOptions{
			Instructions: serverInstructions,
		},
	)

	// Prompts
	RegisterLanguageGuide(server, deps)
	RegisterCodingGuide(server, deps)
	RegisterScaffoldGuide(server, deps)
	RegisterLoggingGuide(server, deps)
	RegisterMetricsGuide(server, deps)
	RegisterTracingGuide(server, deps)

	// Resources
	RegisterOrgStandards(server, deps)
	RegisterLanguageResources(server, deps)
	RegisterScaffoldResource(server, deps)
	RegisterLoggingStandards(server, deps)
	RegisterMetricsStandards(server, deps)
	RegisterTracingStandards(server, deps)

	return server
}
