package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type AppStatusInput struct {
	Name string `json:"name" jsonschema:"application name"`
}

func RegisterAppStatus(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "app_status",
		Description: "Get the current status of a deployed application, including phase, URL, build status, and replica count.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input AppStatusInput) (*gomcp.CallToolResult, any, error) {
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: deps.Namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		result := map[string]any{
			"name":              app.Name,
			"phase":             string(app.Status.Phase),
			"url":               app.Status.URL,
			"latestImage":       app.Status.LatestImage,
			"buildStatus":       app.Status.BuildStatus,
			"availableReplicas": app.Status.AvailableReplicas,
			"replicas":          app.Spec.Replicas,
			"port":              app.Spec.Port,
		}

		// Add source info
		if app.Spec.Image != "" {
			result["sourceType"] = "image"
			result["image"] = app.Spec.Image
		} else if app.Spec.Git != nil {
			result["sourceType"] = "git"
			result["gitUrl"] = app.Spec.Git.URL
			result["gitRevision"] = app.Spec.Git.Revision
		} else if app.Spec.Blob != "" {
			result["sourceType"] = "blob"
		}

		if len(app.Status.Conditions) > 0 {
			conditions := make([]map[string]string, 0, len(app.Status.Conditions))
			for _, c := range app.Status.Conditions {
				conditions = append(conditions, map[string]string{
					"type":    c.Type,
					"status":  string(c.Status),
					"reason":  c.Reason,
					"message": c.Message,
				})
			}
			result["conditions"] = conditions
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
