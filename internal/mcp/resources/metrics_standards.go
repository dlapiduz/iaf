package resources

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/metrics-standards.json
var defaultMetricsStandards []byte

// RegisterMetricsStandards registers the iaf://org/metrics-standards resource
// that exposes the platform's RED method metrics standard as JSON.
func RegisterMetricsStandards(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/metrics-standards",
		Name:        "org-metrics-standards",
		Description: "Platform metrics standard — RED method (http_requests_total, http_request_duration_seconds), /metrics endpoint, Prometheus text format, standard labels.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.MetricsStandardsFile, defaultMetricsStandards)
		if err != nil {
			return nil, fmt.Errorf("loading metrics standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
