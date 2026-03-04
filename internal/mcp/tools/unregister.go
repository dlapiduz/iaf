package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/sessiongc"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type UnregisterInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID to unregister and clean up"`
}

// RegisterUnregisterTool registers the unregister MCP tool.
// It deletes all applications, source tarballs, and the session namespace,
// then removes the session from the store.
func RegisterUnregisterTool(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "unregister",
		Description: "Clean up a session and all its resources. Deletes all applications in the session namespace, removes source tarballs, deletes the Kubernetes namespace (cascading to all resources), and removes the session. This action is irreversible.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input UnregisterInput) (*gomcp.CallToolResult, any, error) {
		sess, ok := deps.Sessions.Lookup(input.SessionID)
		if !ok {
			return nil, nil, fmt.Errorf("session not found")
		}
		namespace := sess.Namespace

		// Collect app names before deletion for the summary.
		var appList iafv1alpha1.ApplicationList
		if err := deps.Client.List(ctx, &appList, client.InNamespace(namespace)); err != nil {
			slog.Warn("failed to list apps during unregister", "namespace", namespace, "error", err)
		}
		appNames := make([]string, 0, len(appList.Items))
		for _, app := range appList.Items {
			appNames = append(appNames, app.Name)
		}

		// Delegate to the session GC cleaner for consistent cleanup logic.
		cleaner := sessiongc.New(deps.Client, deps.Store, deps.Sessions, slog.Default())
		cleaner.CleanupSession(ctx, input.SessionID, namespace)

		result := map[string]any{
			"status":          "unregistered",
			"namespace":       namespace,
			"deletedApps":     appNames,
			"deletedAppCount": len(appNames),
			"message":         fmt.Sprintf("Session %s and namespace %s have been deleted.", input.SessionID, namespace),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
