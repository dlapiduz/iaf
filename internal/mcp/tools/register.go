package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dlapiduz/iaf/internal/auth"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type RegisterInput struct {
	Name string `json:"name,omitempty" jsonschema:"optional friendly name for your workspace (e.g. 'my-project')"`
}

func RegisterRegisterTool(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "register",
		Description: "CALL THIS FIRST. Creates a new session and returns a session_id that is required by every other tool. You only need to call this once — store the session_id and pass it to all subsequent tool calls. Optionally provide a friendly name for your workspace.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input RegisterInput) (*gomcp.CallToolResult, any, error) {
		sess, err := deps.Sessions.Register(input.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("registering session: %w", err)
		}

		if err := auth.EnsureNamespace(ctx, deps.Client, sess.Namespace); err != nil {
			return nil, nil, fmt.Errorf("creating namespace: %w", err)
		}

		result := map[string]any{
			"session_id": sess.ID,
			"namespace":  sess.Namespace,
			"message":    "Session created. IMPORTANT: Store this session_id and include it in ALL subsequent tool calls as the session_id parameter.",
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
