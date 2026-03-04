package coach

import (
	"context"
	_ "embed"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed defaults/license-policy.json
var defaultLicensePolicy []byte

// RegisterLicensePolicy registers the iaf://org/license-policy resource that
// exposes the platform open-source license policy as JSON.
// The operator may override the defaults by setting deps.LicensePolicyFile.
func RegisterLicensePolicy(server *gomcp.Server, deps *Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://org/license-policy",
		Name:        "org-license-policy",
		Description: "Platform open-source license policy — approved and prohibited SPDX identifiers, plus operator notes.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		content, err := loadStandards(deps.LicensePolicyFile, defaultLicensePolicy)
		if err != nil {
			return nil, fmt.Errorf("loading license policy: %w", err)
		}
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
			},
		}, nil
	})
}
