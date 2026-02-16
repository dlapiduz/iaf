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

type DeployAppInput struct {
	Name        string               `json:"name" jsonschema:"application name"`
	Image       string               `json:"image,omitempty" jsonschema:"pre-built container image reference"`
	GitURL      string               `json:"git_url,omitempty" jsonschema:"git repository URL to build from"`
	GitRevision string               `json:"git_revision,omitempty" jsonschema:"git branch, tag, or commit (default: main)"`
	Port        int32                `json:"port,omitempty" jsonschema:"container port (default: 8080)"`
	Replicas    int32                `json:"replicas,omitempty" jsonschema:"number of replicas (default: 1)"`
	Env         []iafv1alpha1.EnvVar `json:"env,omitempty" jsonschema:"environment variables"`
}

func RegisterDeployApp(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "deploy_app",
		Description: "Deploy an application from a container image or git repository. Either image or git_url must be provided.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input DeployAppInput) (*gomcp.CallToolResult, any, error) {
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		if input.Image == "" && input.GitURL == "" {
			return nil, nil, fmt.Errorf("either image or git_url is required")
		}

		app := &iafv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      input.Name,
				Namespace: deps.Namespace,
			},
			Spec: iafv1alpha1.ApplicationSpec{
				Image:    input.Image,
				Port:     input.Port,
				Replicas: input.Replicas,
				Env:      input.Env,
			},
		}

		if input.GitURL != "" {
			revision := input.GitRevision
			if revision == "" {
				revision = "main"
			}
			app.Spec.Git = &iafv1alpha1.GitSource{
				URL:      input.GitURL,
				Revision: revision,
			}
		}

		if app.Spec.Port == 0 {
			app.Spec.Port = 8080
		}
		if app.Spec.Replicas == 0 {
			app.Spec.Replicas = 1
		}

		if err := deps.Client.Create(ctx, app); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, nil, fmt.Errorf("application %q already exists", input.Name)
			}
			return nil, nil, fmt.Errorf("creating application: %w", err)
		}

		host := fmt.Sprintf("%s.%s", input.Name, deps.BaseDomain)
		result := map[string]any{
			"name":    input.Name,
			"status":  "created",
			"message": fmt.Sprintf("Application %q created successfully. It will be available at http://%s once deployed.", input.Name, host),
		}
		if input.GitURL != "" {
			result["source"] = "git"
			result["buildRequired"] = true
		} else {
			result["source"] = "image"
			result["buildRequired"] = false
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
