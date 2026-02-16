package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type PushCodeInput struct {
	Name     string               `json:"name" jsonschema:"application name"`
	Files    map[string]string    `json:"files" jsonschema:"map of file paths to file contents"`
	Port     int32                `json:"port,omitempty" jsonschema:"container port (default: 8080)"`
	Env      []iafv1alpha1.EnvVar `json:"env,omitempty" jsonschema:"environment variables"`
}

func RegisterPushCode(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "push_code",
		Description: "Push source code files to deploy as an application. The platform will automatically detect the language and build a container. Provide a map of file paths to their contents.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input PushCodeInput) (*gomcp.CallToolResult, any, error) {
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		if len(input.Files) == 0 {
			return nil, nil, fmt.Errorf("files map is required")
		}

		// Store source files
		blobURL, err := deps.Store.StoreFiles(input.Name, input.Files)
		if err != nil {
			return nil, nil, fmt.Errorf("storing source files: %w", err)
		}

		port := input.Port
		if port == 0 {
			port = 8080
		}

		// Check if application already exists
		var existing iafv1alpha1.Application
		err = deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: deps.Namespace}, &existing)
		if err == nil {
			// Update existing application
			existing.Spec.Blob = blobURL
			existing.Spec.Image = ""
			existing.Spec.Git = nil
			existing.Spec.Port = port
			if input.Env != nil {
				existing.Spec.Env = input.Env
			}
			if err := deps.Client.Update(ctx, &existing); err != nil {
				return nil, nil, fmt.Errorf("updating application: %w", err)
			}
		} else if apierrors.IsNotFound(err) {
			// Create new application
			app := &iafv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      input.Name,
					Namespace: deps.Namespace,
				},
				Spec: iafv1alpha1.ApplicationSpec{
					Blob:     blobURL,
					Port:     port,
					Replicas: 1,
					Env:      input.Env,
				},
			}
			if err := deps.Client.Create(ctx, app); err != nil {
				return nil, nil, fmt.Errorf("creating application: %w", err)
			}
		} else {
			return nil, nil, fmt.Errorf("checking application: %w", err)
		}

		host := fmt.Sprintf("%s.%s", input.Name, deps.BaseDomain)
		result := map[string]any{
			"name":    input.Name,
			"status":  "building",
			"files":   len(input.Files),
			"message": fmt.Sprintf("Source code uploaded and build started for %q. The build typically takes about 2 minutes. Use the app_status tool with name %q to check build progress and see when it transitions to Running. Once running, it will be available at http://%s.", input.Name, input.Name, host),
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
