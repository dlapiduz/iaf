package coach

import (
	"context"
	_ "embed"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/security-standards.json
var defaultSecurityStandards []byte

// RegisterSecurityStandards registers the iaf://org/security-standards resource
// that exposes the platform security coding standard as JSON.
// The operator may override the defaults by setting deps.SecurityStandardsFile.
func RegisterSecurityStandards(server *gomcp.Server, deps *Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/security-standards",
		Name:        "org-security-standards",
		Description: "Platform security coding standard — input validation, secret handling, output encoding, dependency hygiene, and OWASP Top 10 references.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.SecurityStandardsFile, defaultSecurityStandards)
		if err != nil {
			return nil, fmt.Errorf("loading security standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
