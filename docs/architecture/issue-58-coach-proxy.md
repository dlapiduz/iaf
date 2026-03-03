# Architecture: Platform MCP Proxy to Coach Server (Issue #58)

## Approach

When `IAF_COACH_URL` is set, the platform MCP server connects to the coach as an MCP
client at startup, enumerates all coach prompts and resources, and registers forwarding
closures on the platform server. From an agent's perspective, the full set of prompts
and resources appears on the single platform endpoint — no agent-side configuration
changes needed.

The go-sdk `ClientSession` exposes `Prompts()` and `Resources()` iterators and
`GetPrompt()` / `ReadResource()` methods. The go-sdk has **no built-in proxy mode**;
the platform must enumerate at startup and register per-item forwarding closures. This
is the only viable approach with the current SDK version (1.3.0).

If `IAF_COACH_URL` is unset, the platform continues serving its local coaching content
(the fallback registrations from Issue #56). If the coach is unreachable at startup, the
platform logs a warning and continues with local coaching content — it does not fail.

## Changes Required

### New files

- `internal/mcp/coach_proxy.go` — `RegisterCoachProxy(ctx, server, coachURL, coachToken)` function

### Modified files

- `internal/config/config.go` — add `CoachURL string` and `CoachToken string` fields;
  map to `IAF_COACH_URL` / `IAF_COACH_TOKEN` env vars
- `cmd/apiserver/main.go` — after building the platform MCP server, call
  `iafmcp.RegisterCoachProxy(ctx, mcpServer, cfg.CoachURL, cfg.CoachToken)` if
  `cfg.CoachURL != ""`
- `internal/mcp/server.go` — update `serverInstructions` with a note about the coach
  when coach is configured (pass a `coachConfigured bool` param, or detect dynamically)
- `docs/architecture.md` — add component diagram: Agent → Platform MCP → Coach MCP

## Data / API Changes

### New config fields

```go
// internal/config/config.go additions
CoachURL   string `mapstructure:"coach_url"`   // IAF_COACH_URL
CoachToken string `mapstructure:"coach_token"` // IAF_COACH_TOKEN
```

Defaults: both empty strings (feature disabled).

### `RegisterCoachProxy` implementation outline

```go
// internal/mcp/coach_proxy.go
package mcp

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"

    gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerRoundTripper injects a Bearer token into every request.
type bearerRoundTripper struct {
    token string
    base  http.RoundTripper
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
    req = req.Clone(req.Context())
    req.Header.Set("Authorization", "Bearer "+b.token)
    return b.base.RoundTrip(req)
}

// RegisterCoachProxy connects to the coach MCP server and registers forwarding
// handlers for every prompt and resource the coach exposes. Safe to call
// concurrently with server startup; registers before the server accepts connections.
//
// If the coach is unreachable, RegisterCoachProxy logs a warning and returns nil
// (platform continues serving without coaching content from the remote coach).
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
        slog.Warn("coach proxy: could not connect to coach; coaching prompts/resources unavailable", "url", coachURL, "error", err)
        return nil // graceful degradation — not a fatal error
    }

    // Enumerate and forward prompts.
    for p, err := range cs.Prompts(ctx, nil) {
        if err != nil {
            slog.Warn("coach proxy: error iterating prompts", "error", err)
            break
        }
        prompt := p // capture loop var
        server.AddPrompt(prompt, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
            return cs.GetPrompt(ctx, &gomcp.GetPromptParams{
                Name:      req.Params.Name,
                Arguments: req.Params.Arguments,
            })
        })
    }

    // Enumerate and forward resources.
    for r, err := range cs.Resources(ctx, nil) {
        if err != nil {
            slog.Warn("coach proxy: error iterating resources", "error", err)
            break
        }
        resource := r // capture loop var
        server.AddResource(resource, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
            return cs.ReadResource(ctx, &gomcp.ReadResourceParams{URI: req.Params.URI})
        })
    }

    slog.Info("coach proxy: registered coaching prompts and resources", "url", coachURL)
    return nil
}
```

The `cs` (ClientSession) must remain open for the lifetime of the platform server.
Since the forwarding closures capture `cs`, the session stays live as long as the
platform process runs. The `ctx` passed to `RegisterCoachProxy` should be the platform's
root context (cancelled on shutdown).

### Auth: Bearer token on outbound requests

The platform injects `IAF_COACH_TOKEN` via `bearerRoundTripper`. The coach validates
this token using its standard Echo `middleware.Auth` middleware. No other credential
exchange is needed.

## Multi-tenancy & Shared Resource Impact

- The proxy connection is a single shared `ClientSession` from the platform process to
  the coach — not per-agent or per-session.
- Coaching prompts and resources are stateless and org-wide; sharing a single connection
  is correct.
- The coach receives no tenant/namespace context — it serves the same standards to all
  callers. This is intentional.
- Ingress hostname for the coach (`coach.iaf.localhost` by default) must be distinct
  from the platform hostname (`iaf.localhost`). No hostname collision with app names
  because the coach is a fixed system-level deployment, not an agent-deployed app.

## Security Considerations

- **Cluster-internal traffic**: the coach should be deployed in `iaf-system` namespace
  and its ingress host should not be exposed externally (or should require the same auth
  token). The platform-to-coach connection uses `IAF_COACH_TOKEN`; external agents
  cannot bypass this by connecting directly to the coach without knowing the token.
- **Token handling**: `IAF_COACH_TOKEN` must be treated as a secret (mount from K8s
  Secret, not ConfigMap). Never log the token value. The `bearerRoundTripper` adds it
  only to outbound headers — it is not visible in proxy responses.
- **TLS**: if the coach ingress uses TLS (recommended for production), the
  `StreamableClientTransport.HTTPClient` should use the default Go TLS stack, which
  validates certificates. No custom TLS config needed for cluster-internal self-signed
  certs if cert-manager issues them with the in-cluster CA.
- **Prompt/resource enumeration at startup only**: the platform does not re-enumerate
  coach capabilities at runtime. If new prompts/resources are added to the coach, a
  platform restart is required. This is documented as a known limitation.

## Resource & Performance Impact

- One persistent HTTP connection from the platform to the coach (SSE stream for server-
  initiated notifications — the SDK opens this automatically via `StreamableClientTransport`)
- Forwarding adds one HTTP round-trip per prompt/resource call. Latency budget: coaching
  calls are not on hot paths (humans/agents call them infrequently).
- Memory overhead is negligible (forwarding closures are tiny).

## Migration / Compatibility

- **Rollout order**: deploy coach (Issue #56) → set `IAF_COACH_URL` on platform → platform
  proxy registration happens at next restart. Until `IAF_COACH_URL` is set, the platform
  serves its local fallback coaching content.
- **Hot-reload**: not supported. Changing `IAF_COACH_URL` requires a platform restart.
  This is documented as a known limitation.
- **No agent-visible change**: prompt and resource names/URIs are preserved exactly as
  the coach exposes them. Agents see no difference.

## Open Questions for Developer

- The `cs.Prompts()` iterator (Go 1.23 range-over-func) requires Go 1.23+. Verify the
  project's minimum Go version. If < 1.23, use `cs.ListPrompts()` with pagination
  instead of the iterator form.
- Decide whether failed coach registration should block startup (return error) or
  always degrade gracefully. Recommendation: always degrade (return nil) with a warning
  log, since the platform is still fully functional without coaching.
- The coach `ClientSession` should have a keepalive or reconnect strategy if the coach
  restarts. The `StreamableClientTransport` handles reconnects for the SSE stream, but
  not for the MCP client session itself. Consider wrapping in a retry loop or accepting
  the platform needs a restart if the coach goes down.
