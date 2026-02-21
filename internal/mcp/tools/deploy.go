package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	iafk8s "github.com/dlapiduz/iaf/internal/k8s"
	"github.com/dlapiduz/iaf/internal/validation"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeployAppInput struct {
	SessionID     string               `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name          string               `json:"name" jsonschema:"required - application name (lowercase, hyphens allowed, becomes part of URL)"`
	Image         string               `json:"image,omitempty" jsonschema:"container image to deploy (e.g. 'nginx:latest') - provide either image or git_url"`
	GitURL        string               `json:"git_url,omitempty" jsonschema:"git repository URL to build from (e.g. 'https://github.com/user/repo') - provide either image or git_url"`
	GitRevision   string               `json:"git_revision,omitempty" jsonschema:"git branch, tag, or commit (default: main)"`
	GitCredential string               `json:"git_credential,omitempty" jsonschema:"name of a git credential (from add_git_credential) to use when cloning a private repository"`
	Port          int32                `json:"port,omitempty" jsonschema:"port your app listens on (default: 8080)"`
	Replicas      int32                `json:"replicas,omitempty" jsonschema:"number of replicas (default: 1)"`
	Env           []iafv1alpha1.EnvVar `json:"env,omitempty" jsonschema:"environment variables as [{name, value}]"`
}

func RegisterDeployApp(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "deploy_app",
		Description: "Deploy an application from a pre-built container image or git repository. Requires session_id from the register tool. Provide either 'image' (e.g. 'nginx:latest') or 'git_url' (e.g. 'https://github.com/user/repo'). The app will be available at http://<name>.<base-domain> once running. Default port: 8080.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input DeployAppInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.Name); err != nil {
			return nil, nil, err
		}
		for _, e := range input.Env {
			if err := validation.ValidateEnvVarName(e.Name); err != nil {
				return nil, nil, err
			}
		}
		if input.Image == "" && input.GitURL == "" {
			return nil, nil, fmt.Errorf("either image or git_url is required")
		}

		// Validate git_credential if provided: the Secret must exist in the session namespace
		// and must be an IAF-managed git credential.
		if input.GitCredential != "" {
			credSecret := &corev1.Secret{}
			if err := deps.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: input.GitCredential}, credSecret); err != nil {
				if apierrors.IsNotFound(err) {
					return nil, nil, fmt.Errorf("git credential %q not found; create it with add_git_credential first", input.GitCredential)
				}
				return nil, nil, fmt.Errorf("looking up git credential: %w", err)
			}
			if credSecret.Labels[iafk8s.LabelCredentialType] != "git" {
				return nil, nil, fmt.Errorf("secret %q is not a git credential managed by IAF", input.GitCredential)
			}
		}

		if err := deps.CheckAppNameAvailable(ctx, input.Name, namespace); err != nil {
			return nil, nil, err
		}

		app := &iafv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      input.Name,
				Namespace: namespace,
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
			"message": fmt.Sprintf("Application %q created successfully. It will be available at https://%s once deployed (TLS enabled by default).", input.Name, host),
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
