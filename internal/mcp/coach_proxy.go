package mcp

import (
	"context"
	"log/slog"
	"net/http"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerRoundTripper injects a Bearer token into every outbound request.
// The token value is never logged.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(req)
}

// RegisterCoachProxy connects to the coach MCP server at coachURL and registers
// forwarding handlers on server for every prompt and resource the coach exposes.
// The forwarding closures share a single ClientSession for the platform lifetime.
//
// If the coach is unreachable or enumeration fails, a warning is logged and the
// function returns nil — the platform continues serving its local coaching content.
// This is always a graceful degradation, never a fatal error.
//
// ctx should be the platform's root context; cancelling it will close the coach
// connection. A platform restart is required if IAF_COACH_URL changes or if the
// coach goes down and needs to be re-connected.
func RegisterCoachProxy(ctx context.Context, server *gomcp.Server, coachURL, coachToken string) error {
	httpClient := &http.Client{
		Transport: &bearerRoundTripper{
			token: coachToken,
			base:  http.DefaultTransport,
		},
	}

	client := gomcp.NewClient(
		&gomcp.Implementation{Name: "iaf-platform-proxy", Version: "0.1.0"},
		nil,
	)
	cs, err := client.Connect(ctx, &gomcp.StreamableClientTransport{
		Endpoint:   coachURL,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		slog.Warn("coach proxy: could not connect to coach; local coaching content will be used",
			"url", coachURL, "error", err)
		return nil
	}

	promptCount := 0
	for p, err := range cs.Prompts(ctx, nil) {
		if err != nil {
			slog.Warn("coach proxy: error iterating prompts", "error", err)
			break
		}
		prompt := p
		server.AddPrompt(prompt, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
			return cs.GetPrompt(ctx, &gomcp.GetPromptParams{
				Name:      req.Params.Name,
				Arguments: req.Params.Arguments,
			})
		})
		promptCount++
	}

	resourceCount := 0
	for r, err := range cs.Resources(ctx, nil) {
		if err != nil {
			slog.Warn("coach proxy: error iterating resources", "error", err)
			break
		}
		resource := r
		server.AddResource(resource, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
			return cs.ReadResource(ctx, &gomcp.ReadResourceParams{URI: req.Params.URI})
		})
		resourceCount++
	}

	slog.Info("coach proxy: registered coach prompts and resources",
		"url", coachURL, "prompts", promptCount, "resources", resourceCount)
	return nil
}
