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
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - application name to delete"`
}

func RegisterDeleteApp(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "delete_app",
		Description: "Delete an application and all its associated Kubernetes resources (deployment, service, ingress route, build). Requires session_id from the register tool and the application name. This action is irreversible.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input DeleteAppInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		app := &iafv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      input.Name,
				Namespace: namespace,
			},
		}

		if err := deps.Client.Delete(ctx, app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("deleting application: %w", err)
		}

		// Clean up stored source
		_ = deps.Store.Delete(namespace, input.Name)

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
