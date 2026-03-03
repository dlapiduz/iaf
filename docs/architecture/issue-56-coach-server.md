# Architecture: Coach Server — Standalone MCP App for Developer Guidance (Issue #56)

## Approach

Split the existing monolithic MCP server into two concerns: **platform operations** (deploy,
build, session management — requires K8s) and **developer coaching** (coding standards,
framework guidance, logging/metrics/tracing — stateless, read-only).

The coach server is a new binary (`cmd/coachserver`) backed by a new package
`internal/mcp/coach/`. It exposes coaching prompts and resources via the same
streamable-HTTP MCP transport the platform uses. Auth is a single Bearer token gated
by Echo middleware (same pattern as the REST API). No K8s client, no session store.

The platform binary continues to serve platform-operation tools and prompts. When the
coach is deployed, Issue #58 (proxy) forwards coaching requests through the platform
endpoint transparently, so agents see no change.

## Changes Required

### New files

- `cmd/coachserver/main.go` — binary entry point; reads COACH_* env vars, wires
  Echo + MCP server, starts HTTP listener
- `internal/mcp/coach/deps.go` — `CoachDependencies` struct (no K8s, no sessions)
- `internal/mcp/coach/server.go` — `NewCoachServer(deps)` that registers all coaching
  prompts and resources; returns `*gomcp.Server`
- `internal/mcp/coach/prompts/language_guide.go` — moved from `internal/mcp/prompts/`
- `internal/mcp/coach/prompts/coding_guide.go` — moved
- `internal/mcp/coach/prompts/scaffold_guide.go` — moved
- `internal/mcp/coach/prompts/logging_guide.go` — moved
- `internal/mcp/coach/prompts/metrics_guide.go` — moved
- `internal/mcp/coach/prompts/tracing_guide.go` — moved
- `internal/mcp/coach/prompts/aliases.go` — `languageAliases` map (currently
  duplicated across logging/metrics/tracing guides; extract into one place)
- `internal/mcp/coach/resources/org_standards.go` — moved
- `internal/mcp/coach/resources/logging_standards.go` — moved
- `internal/mcp/coach/resources/metrics_standards.go` — moved
- `internal/mcp/coach/resources/tracing_standards.go` — moved
- `internal/mcp/coach/resources/supported_languages.go` — moved
- `internal/mcp/coach/resources/scaffold.go` — moved
- `internal/mcp/coach/resources/standards_loader.go` — moved (shared helper)
- `internal/mcp/coach/resources/defaults/` — move embedded JSON defaults here
  (`logging-standards.json`, `metrics-standards.json`, `tracing-standards.json`)
- `deploy/coach/Dockerfile` — multi-stage build producing the coachserver binary
- `deploy/coach/values.yaml` — Helm/kustomize overrides for deploying coach as an
  IAF Application (env vars, resource limits)

### Modified files

- `internal/mcp/prompts/` — **delete** the 6 coaching prompts listed above; retain
  only `deploy_guide.go`, `services_guide.go`, `github_guide.go`, `prompts_test.go`
- `internal/mcp/resources/` — **delete** the 6 coaching resource files listed above;
  retain `platform_info.go`, `application_spec.go`, `data_catalog.go`,
  `github_standards.go`, `resources_test.go`
- `internal/mcp/server.go` — remove `Register*` calls for the deleted coaching
  prompts/resources; update `serverInstructions` to reference the coach when configured
- `internal/mcp/prompts/prompts_test.go` — update `TestListPrompts` count assertion
  (was 8, becomes 3 platform-only prompts + conditionally 1 github-guide)
- `internal/mcp/resources/resources_test.go` — update `TestListResources` count
  (was 9 static, becomes 4 platform-only resources + conditionally github-standards)
- `Makefile` — add `coachserver` to the `build` target
- `docs/architecture.md` — add two-domain architecture description

## Data / API Changes

### `CoachDependencies` struct

```go
// internal/mcp/coach/deps.go
package coach

import "github.com/dlapiduz/iaf/internal/orgstandards"

type Dependencies struct {
    BaseDomain           string
    OrgStandards         *orgstandards.Loader
    LoggingStandardsFile string
    MetricsStandardsFile string
    TracingStandardsFile string
    // Extended in follow-on issues (#59-62):
    // LicensePolicyFile    string
    // CICDStandardsFile    string
    // SecurityStandardsFile string
    // GitHubStandardsFile  string
}
```

### Coach server wiring

```go
// internal/mcp/coach/server.go
func NewCoachServer(deps *Dependencies) *gomcp.Server {
    server := gomcp.NewServer(...)
    prompts.RegisterLanguageGuide(server, deps)
    prompts.RegisterCodingGuide(server, deps)
    prompts.RegisterScaffoldGuide(server, deps)
    prompts.RegisterLoggingGuide(server, deps)
    prompts.RegisterMetricsGuide(server, deps)
    prompts.RegisterTracingGuide(server, deps)
    resources.RegisterOrgStandards(server, deps)
    resources.RegisterLanguageResources(server, deps)
    resources.RegisterScaffoldResource(server, deps)
    resources.RegisterLoggingStandards(server, deps)
    resources.RegisterMetricsStandards(server, deps)
    resources.RegisterTracingStandards(server, deps)
    return server
}
```

All `Register*` functions in `internal/mcp/coach/` take `(*gomcp.Server, *coach.Dependencies)`
following the same convention as platform tools.

### Coach config (env vars, read in `cmd/coachserver/main.go`)

| Env var                         | Default  | Purpose                              |
|----------------------------------|----------|--------------------------------------|
| `COACH_PORT`                    | `8082`   | HTTP listen port                     |
| `COACH_API_TOKEN`               | —        | Required Bearer token for auth       |
| `COACH_BASE_DOMAIN`             | —        | Passed to prompts that mention URLs  |
| `COACH_ORG_STANDARDS_FILE`      | —        | Override path for coding standards   |
| `COACH_LOGGING_STANDARDS_FILE`  | —        | Override path for logging standards  |
| `COACH_METRICS_STANDARDS_FILE`  | —        | Override path for metrics standards  |
| `COACH_TRACING_STANDARDS_FILE`  | —        | Override path for tracing standards  |

The coach binary does **not** use the shared `internal/config` package (that package
imports K8s deps). It reads env vars directly with `os.Getenv` and validates them at
startup.

### Auth implementation

Reuse `internal/middleware.Auth([]string{coachToken})` — the coach has one token.
The MCP handler is mounted at `/mcp` behind the Echo auth middleware. The health
endpoint `/health` is excluded from auth (same exclusion rule as the API server).

## Multi-tenancy & Shared Resource Impact

The coach has no K8s access, no tenant namespace, and no session store. It is entirely
stateless and read-only. No cross-tenant risk. The only "shared" aspect is that a single
coach deployment serves all tenants — this is intentional and acceptable because
standards are org-wide, not per-tenant.

## Security Considerations

- **Input validation**: Coach prompts accept only a `language` argument (a controlled
  enum). Validate against the canonical language list before use in any string rendering.
  Reject unknown values with a clear error.
- **File path validation**: The `standards_loader.go` `loadStandards` function already
  rejects paths containing `..`. This moves to the coach package unchanged.
- **File size cap**: The 1 MB cap in `loadStandards` is retained.
- **Version strings in framework standards** (Issue #59 extension): when standards JSON
  is loaded, validate that version strings contain only `[0-9a-zA-Z.\-+]` to prevent
  shell metacharacter injection if strings are used in generated build files.
- **`COACH_API_TOKEN`**: If empty, the coach must refuse to start (log error, exit 1).
  An unauthenticated coach leaks org standards (internal library and license policy
  details). Flag: `needs-security-review`.
- **TLS**: The coach is cluster-internal; TLS termination is handled by Traefik/ingress.
  No in-process TLS required.

## Resource & Performance Impact

- One additional Deployment in the cluster (stateless, single replica is fine)
- No K8s API calls — zero apiserver load from the coach
- Memory: orgstandards loader + embedded JSON (< 1 MB total)
- Coach can be deployed as an IAF Application (Dockerfile + `deploy/coach/values.yaml`)

## Migration / Compatibility

- The coaching prompts and resources disappear from the platform MCP endpoint when this
  issue is deployed **without** Issue #58 (proxy). Agents will get "not found" errors for
  `coding-guide`, `logging-guide`, etc.
- **Recommended rollout order**: deploy #56 (coach binary) → configure `IAF_COACH_URL`
  on the platform → deploy #58 (proxy). Until #58 is deployed, the platform should
  continue registering its own local copies of coaching prompts/resources.
- **Implementation option**: Keep both registrations in the platform as a transitional
  flag until #58 lands. A simple approach: if `IAF_COACH_URL` is not set, the platform
  re-registers the coaching content directly (copy the files). This preserves backward
  compatibility without requiring both issues to ship simultaneously.
- Test assertion counts will change: update both `prompts_test.go` and
  `resources_test.go` after the coaching content moves.

## Open Questions for Developer

- Confirm the rollout strategy above with the PM: should the platform keep local fallback
  coaching content until #58 ships, or are both issues batched into one release?
- The `languageAliases` map is currently repeated in each guide file — extract to
  `internal/mcp/coach/prompts/aliases.go` and import from there in all guides.
- The coach `AllowMethods` in CORS config should restrict to the same set as the platform.
