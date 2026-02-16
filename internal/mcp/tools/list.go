package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ListAppsInput struct {
	Status string `json:"status,omitempty" jsonschema:"filter by status (Pending, Building, Deploying, Running, Failed)"`
}

func RegisterListApps(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "list_apps",
		Description: "List all deployed applications with their status, source type, and URLs. Optionally filter by status.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input ListAppsInput) (*gomcp.CallToolResult, any, error) {
		var list iafv1alpha1.ApplicationList
		if err := deps.Client.List(ctx, &list, client.InNamespace(deps.Namespace)); err != nil {
			return nil, nil, fmt.Errorf("listing applications: %w", err)
		}

		var apps []map[string]any
		for _, app := range list.Items {
			if input.Status != "" && string(app.Status.Phase) != input.Status {
				continue
			}

			entry := map[string]any{
				"name":              app.Name,
				"phase":             string(app.Status.Phase),
				"url":               app.Status.URL,
				"availableReplicas": app.Status.AvailableReplicas,
				"replicas":          app.Spec.Replicas,
			}

			if app.Spec.Image != "" {
				entry["sourceType"] = "image"
			} else if app.Spec.Git != nil {
				entry["sourceType"] = "git"
			} else if app.Spec.Blob != "" {
				entry["sourceType"] = "code"
			}

			if app.Status.BuildStatus != "" {
				entry["buildStatus"] = app.Status.BuildStatus
			}

			apps = append(apps, entry)
		}

		if apps == nil {
			apps = []map[string]any{}
		}

		result := map[string]any{
			"applications": apps,
			"total":        len(apps),
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
