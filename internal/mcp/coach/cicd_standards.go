package coach

import (
	"context"
	_ "embed"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/cicd-standards.json
var defaultCICDStandards []byte

// RegisterCICDStandards registers the iaf://org/cicd-standards resource
// that exposes the platform CI/CD pipeline standard as JSON.
// The operator may override the defaults by setting deps.CICDStandardsFile.
func RegisterCICDStandards(server *gomcp.Server, deps *Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/cicd-standards",
		Name:        "org-cicd-standards",
		Description: "Platform CI/CD pipeline standard — required stages, manual approval gates, environment promotion rules, and IAF deployment strategy mapping.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.CICDStandardsFile, defaultCICDStandards)
		if err != nil {
			return nil, fmt.Errorf("loading cicd standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
