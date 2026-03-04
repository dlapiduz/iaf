package coach

import (
	"context"
	_ "embed"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/github-standards.json
var defaultGitHubStandards []byte

// RegisterGitHubStandards registers the iaf://org/github-standards resource
// that exposes organisation GitHub workflow conventions as JSON.
// The operator may override the defaults by setting deps.GitHubStandardsFile.
func RegisterGitHubStandards(server *gomcp.Server, deps *Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/github-standards",
		Name:        "github-standards",
		Description: "Organisation GitHub workflow conventions — branching strategy, branch protection, required status checks, commit format, and CI template.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.GitHubStandardsFile, defaultGitHubStandards)
		if err != nil {
			return nil, fmt.Errorf("loading github standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
