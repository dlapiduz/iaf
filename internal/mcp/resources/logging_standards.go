package resources

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/logging-standards.json
var defaultLoggingStandards []byte

// RegisterLoggingStandards registers the iaf://org/logging-standards resource
// that exposes the platform's structured logging standard as JSON.
func RegisterLoggingStandards(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/logging-standards",
		Name:        "org-logging-standards",
		Description: "Platform logging standard — stdout-only, JSON Lines format, required fields (time/level/msg), prohibited content (secrets, PII), and trace correlation fields.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.LoggingStandardsFile, defaultLoggingStandards)
		if err != nil {
			return nil, fmt.Errorf("loading logging standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
