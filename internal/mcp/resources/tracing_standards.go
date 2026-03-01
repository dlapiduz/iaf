package resources

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/tracing-standards.json
var defaultTracingStandards []byte

// RegisterTracingStandards registers the iaf://org/tracing-standards resource
// that exposes the platform's OpenTelemetry tracing standard as JSON.
func RegisterTracingStandards(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/tracing-standards",
		Name:        "org-tracing-standards",
		Description: "Platform tracing standard — OTel SDK, OTLP exporter, endpoint from OTEL_EXPORTER_OTLP_ENDPOINT env var, required span attributes, trace-log correlation, auto-instrumentation availability per language.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.TracingStandardsFile, defaultTracingStandards)
		if err != nil {
			return nil, fmt.Errorf("loading tracing standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
