package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeleteAppInput struct {
	Name string `json:"name" jsonschema:"application name"`
}

func RegisterDeleteApp(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "delete_app",
		Description: "Delete a deployed application. This removes the application and all associated resources (deployment, service, ingress route, and kpack build).",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input DeleteAppInput) (*gomcp.CallToolResult, any, error) {
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		app := &iafv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      input.Name,
				Namespace: deps.Namespace,
			},
		}

		if err := deps.Client.Delete(ctx, app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("deleting application: %w", err)
		}

		// Clean up stored source
		_ = deps.Store.Delete(input.Name)

		result := map[string]any{
			"name":    input.Name,
			"status":  "deleted",
			"message": fmt.Sprintf("Application %q and all associated resources have been deleted.", input.Name),
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
