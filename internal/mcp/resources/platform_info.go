package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterPlatformInfo registers the iaf://platform resource that exposes
// platform configuration as structured JSON.
func RegisterPlatformInfo(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://platform",
		Name:        "platform-info",
		Description: "IAF platform configuration — supported languages, base stack, deployment methods, defaults, and routing.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		info := map[string]any{
			"name":    "Intelligent Application Fabric",
			"version": "0.1.0",
			"namespace": deps.Namespace,
			"baseDomain": deps.BaseDomain,
			"routing": map[string]any{
				"ingress":   "traefik",
				"pattern":   fmt.Sprintf("<name>.%s", deps.BaseDomain),
				"protocol":  "http",
			},
			"supportedLanguages": []string{"go", "nodejs", "python", "java", "ruby"},
			"buildStack":         "Paketo Jammy LTS (Ubuntu 22.04)",
			"buildSystem":        "kpack with Cloud Native Buildpacks",
			"deploymentMethods": []map[string]string{
				{"method": "image", "description": "Deploy from a pre-built container image"},
				{"method": "git", "description": "Build and deploy from a git repository"},
				{"method": "source", "description": "Upload source code via push_code tool, then deploy"},
			},
			"defaults": map[string]any{
				"port":        8080,
				"replicas":    1,
				"gitRevision": "main",
			},
			"security": map[string]any{
				"containerUser": "non-root (buildpack default)",
				"baseOS":        "Ubuntu Jammy 22.04 LTS",
			},
		}

		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling platform info: %w", err)
		}

		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}
