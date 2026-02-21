package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	iafk8s "github.com/dlapiduz/iaf/internal/k8s"
	"github.com/dlapiduz/iaf/internal/validation"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	maxCredentialsPerSession = 20
	maxPasswordLen           = 4096
	maxSSHKeyLen             = 16 * 1024
)

// AddGitCredentialInput is the input for the add_git_credential tool.
type AddGitCredentialInput struct {
	SessionID    string `json:"session_id"    jsonschema:"required - session ID from the register tool"`
	Name         string `json:"name"          jsonschema:"required - credential name (DNS label: lowercase alphanumeric and hyphens)"`
	Type         string `json:"type"          jsonschema:"required - credential type: 'basic-auth' or 'ssh'"`
	GitServerURL string `json:"git_server_url" jsonschema:"required - server URL to associate credential with (https://... for basic-auth, git@<host> for ssh)"`
	// basic-auth fields
	Username string `json:"username,omitempty" jsonschema:"username (required for basic-auth)"`
	Password string `json:"password,omitempty" jsonschema:"password or token (required for basic-auth, max 4096 bytes)"`
	// ssh field
	PrivateKey string `json:"private_key,omitempty" jsonschema:"PEM-encoded SSH private key (required for ssh, max 16 KB)"`
}

// ListGitCredentialsInput is the input for the list_git_credentials tool.
type ListGitCredentialsInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID from the register tool"`
}

// DeleteGitCredentialInput is the input for the delete_git_credential tool.
type DeleteGitCredentialInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID from the register tool"`
	Name      string `json:"name"       jsonschema:"required - credential name to delete"`
}

// RegisterAddGitCredential registers the add_git_credential MCP tool.
func RegisterAddGitCredential(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "add_git_credential",
		Description: "Store a git credential (username/password or SSH key) in the session namespace so kpack can clone private repositories. Requires session_id. Credential material is never returned in any tool output. To rotate a credential, delete it and re-create it.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input AddGitCredentialInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}

		// Validate name as DNS label.
		if err := validation.ValidateAppName(input.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid credential name: %w", err)
		}

		// Validate type allowlist.
		if input.Type != "basic-auth" && input.Type != "ssh" {
			return nil, nil, fmt.Errorf("type must be 'basic-auth' or 'ssh'")
		}

		// Type-specific validation.
		switch input.Type {
		case "basic-auth":
			if err := validation.ValidateBasicAuthGitServerURL(input.GitServerURL); err != nil {
				return nil, nil, err
			}
			if input.Username == "" {
				return nil, nil, fmt.Errorf("username is required for basic-auth")
			}
			if input.Password == "" {
				return nil, nil, fmt.Errorf("password is required for basic-auth")
			}
			if len(input.Password) > maxPasswordLen {
				return nil, nil, fmt.Errorf("password must be %d bytes or fewer", maxPasswordLen)
			}
		case "ssh":
			if err := validation.ValidateSSHGitServerURL(input.GitServerURL); err != nil {
				return nil, nil, err
			}
			if input.PrivateKey == "" {
				return nil, nil, fmt.Errorf("private_key is required for ssh")
			}
			if len(input.PrivateKey) > maxSSHKeyLen {
				return nil, nil, fmt.Errorf("private_key must be %d bytes or fewer", maxSSHKeyLen)
			}
			if !strings.HasPrefix(input.PrivateKey, "-----BEGIN ") {
				return nil, nil, fmt.Errorf("private_key must be a PEM-encoded key (must start with '-----BEGIN ...')")
			}
		}

		// Enforce per-session credential count limit.
		var secretList corev1.SecretList
		if err := deps.Client.List(ctx, &secretList,
			client.InNamespace(namespace),
			client.MatchingLabels{iafk8s.LabelCredentialType: "git"},
		); err != nil {
			return nil, nil, fmt.Errorf("listing git credentials: %w", err)
		}
		if len(secretList.Items) >= maxCredentialsPerSession {
			return nil, nil, fmt.Errorf("credential limit reached: a session may have at most %d git credentials; delete an existing one before adding a new one", maxCredentialsPerSession)
		}

		// Build and create the Secret.
		secret := iafk8s.BuildGitCredentialSecret(
			namespace, input.Name, input.Type, input.GitServerURL,
			input.Username, input.Password, input.PrivateKey,
		)
		if err := deps.Client.Create(ctx, secret); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, nil, fmt.Errorf("credential %q already exists; delete it first to replace it", input.Name)
			}
			return nil, nil, fmt.Errorf("creating credential: %w", err)
		}

		// Patch the kpack SA. On failure, clean up the Secret before returning.
		if err := iafk8s.AddSecretToKpackSA(ctx, deps.Client, namespace, input.Name); err != nil {
			// Best-effort cleanup.
			_ = deps.Client.Delete(ctx, secret)
			return nil, nil, fmt.Errorf("registering credential with kpack service account: %w", err)
		}

		result := map[string]any{
			"name":           input.Name,
			"type":           input.Type,
			"git_server_url": input.GitServerURL,
			"created":        true,
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// RegisterListGitCredentials registers the list_git_credentials MCP tool.
func RegisterListGitCredentials(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "list_git_credentials",
		Description: "List all git credentials stored in the current session. Returns name, type, and server URL — never credential material.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input ListGitCredentialsInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}

		var secretList corev1.SecretList
		if err := deps.Client.List(ctx, &secretList,
			client.InNamespace(namespace),
			client.MatchingLabels{iafk8s.LabelCredentialType: "git"},
		); err != nil {
			return nil, nil, fmt.Errorf("listing git credentials: %w", err)
		}

		type credInfo struct {
			Name         string `json:"name"`
			Type         string `json:"type"`
			GitServerURL string `json:"git_server_url"`
			CreatedAt    string `json:"created_at"`
		}
		creds := make([]credInfo, 0, len(secretList.Items))
		for _, s := range secretList.Items {
			credType := credTypeFromSecretType(s.Type)
			serverURL := s.Annotations[iafk8s.AnnotationGitServer]
			var createdAt string
			if !s.CreationTimestamp.IsZero() {
				createdAt = s.CreationTimestamp.UTC().Format(time.RFC3339)
			}
			creds = append(creds, credInfo{
				Name:         s.Name,
				Type:         credType,
				GitServerURL: serverURL,
				CreatedAt:    createdAt,
			})
		}

		result := map[string]any{
			"credentials": creds,
			"total":       len(creds),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// RegisterDeleteGitCredential registers the delete_git_credential MCP tool.
func RegisterDeleteGitCredential(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "delete_git_credential",
		Description: "Delete a git credential from the current session. Also removes it from the kpack service account so it will no longer be used for builds.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input DeleteGitCredentialInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		// Fetch the Secret and verify it is a git credential (label guard).
		secret := &corev1.Secret{}
		if err := deps.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: input.Name}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("credential %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting credential: %w", err)
		}
		if secret.Labels[iafk8s.LabelCredentialType] != "git" {
			return nil, nil, fmt.Errorf("secret %q is not a git credential managed by IAF", input.Name)
		}

		// Remove from kpack SA before deleting the Secret.
		if err := iafk8s.RemoveSecretFromKpackSA(ctx, deps.Client, namespace, input.Name); err != nil {
			return nil, nil, fmt.Errorf("removing credential from kpack service account: %w", err)
		}

		if err := deps.Client.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: input.Name, Namespace: namespace},
		}); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("credential %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("deleting credential: %w", err)
		}

		result := map[string]any{
			"name":    input.Name,
			"deleted": true,
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// credTypeFromSecretType converts a Kubernetes secret type to the IAF credential type string.
func credTypeFromSecretType(t corev1.SecretType) string {
	switch t {
	case corev1.SecretTypeBasicAuth:
		return "basic-auth"
	case corev1.SecretTypeSSHAuth:
		return "ssh"
	default:
		return string(t)
	}
}
