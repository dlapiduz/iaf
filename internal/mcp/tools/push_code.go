package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type PushCodeInput struct {
	SessionID string               `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string               `json:"name" jsonschema:"required - application name (lowercase, hyphens allowed, becomes part of URL)"`
	Files     map[string]string    `json:"files" jsonschema:"required - map of file paths to file contents, e.g. {\"main.go\": \"package main...\", \"go.mod\": \"module app...\"}"`
	Port      int32                `json:"port,omitempty" jsonschema:"port your app listens on (default: 8080)"`
	Env       []iafv1alpha1.EnvVar `json:"env,omitempty" jsonschema:"environment variables as [{name, value}]"`
}

func RegisterPushCode(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "push_code",
		Description: `Upload source code and automatically build and deploy it as an application. Requires session_id from the register tool. The 'files' parameter is a JSON object mapping file paths to their contents, e.g. {"main.go": "package main\n...", "go.mod": "module myapp\n..."}. The platform auto-detects the language (Go, Node.js, Python, Java, Ruby) and builds a container. Your app must listen on the specified port (default 8080). Use app_status to monitor build progress (~2 min).`,
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input PushCodeInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		if len(input.Files) == 0 {
			return nil, nil, fmt.Errorf("files map is required")
		}

		// Store source files — append revision to URL so kpack detects changes
		blobURL, err := deps.Store.StoreFiles(namespace, input.Name, input.Files)
		if err != nil {
			return nil, nil, fmt.Errorf("storing source files: %w", err)
		}
		blobURL = blobURL + "?rev=" + strconv.FormatInt(time.Now().UnixNano(), 36)

		port := input.Port
		if port == 0 {
			port = 8080
		}

		// Check if application already exists
		var existing iafv1alpha1.Application
		err = deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: namespace}, &existing)
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
			// Check name availability before creating
			if err := deps.CheckAppNameAvailable(ctx, input.Name, namespace); err != nil {
				return nil, nil, err
			}
			// Create new application
			app := &iafv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      input.Name,
					Namespace: namespace,
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
			"message": fmt.Sprintf("Source code uploaded and build started for %q. IMPORTANT: The build takes about 2 minutes. Wait at least 90 seconds before checking status. Then use app_status with name %q to check progress. Do NOT poll repeatedly — check once after 90s, then once more after another 30s if still building. Once status is Running, the app will be available at http://%s.", input.Name, input.Name, host),
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
