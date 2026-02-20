package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterOrgStandards registers the iaf://org/coding-standards resource that
// exposes organisation-level coding standards as structured JSON.
func RegisterOrgStandards(server *gomcp.Server, deps *tools.Dependencies) {
	loader := deps.OrgStandards
	if loader == nil {
		loader = orgstandards.New("", nil)
	}

	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/coding-standards",
		Name:        "org-coding-standards",
		Description: "Organisation coding standards — health-check path, logging format, env-var naming, approved libraries, and per-language best practices.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		data, err := json.MarshalIndent(loader.Get(), "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling org standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}
