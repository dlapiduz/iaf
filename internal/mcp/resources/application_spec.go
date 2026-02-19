package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterApplicationSpec registers the iaf://schema/application resource
// that provides the Application CRD spec reference.
func RegisterApplicationSpec(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://schema/application",
		Name:        "application-spec",
		Description: "Application CRD spec reference — all fields, types, defaults, and constraints.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		spec := map[string]any{
			"apiVersion": "iaf.io/v1alpha1",
			"kind":       "Application",
			"spec": map[string]any{
				"image": map[string]any{
					"type":        "string",
					"description": "Pre-built container image reference (e.g., 'nginx:latest'). Mutually exclusive with git and blob.",
					"optional":    true,
				},
				"git": map[string]any{
					"type":        "object",
					"description": "Git repository to build from using kpack. Mutually exclusive with image and blob.",
					"optional":    true,
					"fields": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "Git repository URL.",
							"required":    true,
						},
						"revision": map[string]any{
							"type":        "string",
							"description": "Branch, tag, or commit to build.",
							"default":     "main",
							"optional":    true,
						},
					},
				},
				"blob": map[string]any{
					"type":        "string",
					"description": "URL to a source code archive (tarball). Set by the platform when source is uploaded via push_code. Mutually exclusive with image and git.",
					"optional":    true,
				},
				"port": map[string]any{
					"type":        "integer",
					"description": "Container port the application listens on.",
					"default":     8080,
					"constraints": "1-65535",
				},
				"replicas": map[string]any{
					"type":        "integer",
					"description": "Desired number of pod replicas.",
					"default":     1,
					"constraints": "minimum 0",
				},
				"env": map[string]any{
					"type":        "array",
					"description": "Environment variables for the application container.",
					"optional":    true,
					"items": map[string]any{
						"name":  "string (required) — variable name",
						"value": "string (required) — variable value",
					},
				},
				"host": map[string]any{
					"type":        "string",
					"description": "Hostname for routing.",
					"default":     fmt.Sprintf("<name>.%s", deps.BaseDomain),
					"optional":    true,
				},
			},
			"status": map[string]any{
				"phase": map[string]any{
					"type":   "string",
					"enum":   []string{"Pending", "Building", "Deploying", "Running", "Failed"},
					"description": "Current lifecycle phase of the application.",
				},
				"url": map[string]any{
					"type":        "string",
					"description": "Routable URL for the application.",
				},
				"latestImage": map[string]any{
					"type":        "string",
					"description": "Most recently built or provided container image.",
				},
				"buildStatus": map[string]any{
					"type":        "string",
					"description": "kpack build status: Building, Succeeded, or Failed.",
				},
				"availableReplicas": map[string]any{
					"type":        "integer",
					"description": "Number of available pod replicas.",
				},
			},
			"constraints": map[string]any{
				"name":      "Must be a valid DNS label: lowercase alphanumeric and hyphens, 1-63 characters.",
				"source":    "Exactly one of image, git, or blob must be specified.",
				"namespace": "Per-session, assigned by the register tool.",
			},
		}

		data, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling application spec: %w", err)
		}

		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}
