package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

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

// RegisterCoachProxy starts a background goroutine that connects to the coach
// MCP server at coachURL and registers forwarding handlers on server for every
// prompt and resource the coach exposes. It retries with exponential backoff
// (up to 30 s) until the coach is reachable or ctx is cancelled.
//
// This is intentionally non-blocking and always returns nil — the platform
// serves without coaching content until the coach becomes available.
//
// ctx should be the platform's root context; cancelling it stops retries and
// closes any established coach connection.
func RegisterCoachProxy(ctx context.Context, server *gomcp.Server, coachURL, coachToken string) error {
	go func() {
		backoff := 2 * time.Second
		const maxBackoff = 30 * time.Second
		for {
			if err := connectAndRegister(ctx, server, coachURL, coachToken); err == nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < maxBackoff {
					backoff *= 2
				}
			}
		}
	}()
	return nil
}

// connectAndRegister attempts a single connection to the coach, enumerates its
// prompts and resources, and registers forwarding closures on server.
// Returns nil on success, non-nil on any failure (caller retries).
func connectAndRegister(ctx context.Context, server *gomcp.Server, coachURL, coachToken string) error {
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
		slog.Warn("coach proxy: could not connect to coach, will retry",
			"url", coachURL, "error", err)
		return err
	}

	promptCount := 0
	for p, err := range cs.Prompts(ctx, nil) {
		if err != nil {
			slog.Warn("coach proxy: error iterating prompts", "error", err)
			return err
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
			return err
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
