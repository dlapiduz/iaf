package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterGitHubStandards registers the iaf://org/github-standards resource
// that exposes org GitHub workflow conventions as structured JSON.
// Call only when GitHub is configured (deps.GitHub != nil).
func RegisterGitHubStandards(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/github-standards",
		Name:        "github-standards",
		Description: "Organisation GitHub workflow conventions — branching strategy, branch protection, required status checks, commit format, and CI template.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		standards := map[string]any{
			"defaultBranch":        "main",
			"branchNamingPattern":  "^(feat|fix|chore|docs|test)/[a-z0-9][a-z0-9-]*$",
			"requiredReviewers": map[string]int{
				"solo-agent":   0,
				"multi-agent":  1,
				"human-review": 1,
			},
			"requiredStatusChecks": []string{"CI / ci"},
			"commitMessageFormat":  "Conventional Commits (https://www.conventionalcommits.org)",
			"ciTemplate":           ".github/workflows/ci.yml",
			"iafIntegration": map[string]string{
				"deployBranch": "main",
				"deployMethod": "git",
			},
		}

		data, err := json.MarshalIndent(standards, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling github standards: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}
