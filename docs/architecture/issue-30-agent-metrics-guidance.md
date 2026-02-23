# Architecture: Agent Metrics Guidance — MCP Resources and Prompts (Issue #30)

## Technical Design

### Approach

Deliver the platform's metrics standard to agents through two MCP additions: a `iaf://org/metrics-standards` resource (structured JSON loaded from an embedded file, same pattern as issue #1's `iaf://org/coding-standards`) and a `metrics-guide` prompt (parameterized by language, same pattern as `language-guide`). Additionally, update `deploy_app`/`push_code` to accept a `metrics` parameter, update the controller to set `app.kubernetes.io/version` on Deployments, and add `metricsEndpoint` to `app_status` responses. These changes depend on issue #29 (`spec.metrics` CRD field) being implemented first.

### Changes Required

**New file: `config/org-standards/metrics-standards.json`** (embedded):
```json
{
  "version": "1.0",
  "method": "RED",
  "endpoint": {
    "path": "/metrics",
    "portSource": "same as spec.port unless spec.metrics.port is set",
    "format": "prometheus-text"
  },
  "requiredMetrics": [
    {
      "name": "http_requests_total",
      "type": "counter",
      "labels": ["method", "path", "status_code"],
      "description": "Total HTTP requests by method, path, and status code."
    },
    {
      "name": "http_request_duration_seconds",
      "type": "histogram",
      "buckets": [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5],
      "labels": ["method", "path"],
      "description": "HTTP request duration in seconds."
    }
  ],
  "standardLabels": {
    "app": "Set automatically from pod label app.kubernetes.io/name (set by IAF controller)",
    "version": "Set automatically from pod label app.kubernetes.io/version"
  },
  "registration": {
    "how": "Set spec.metrics.enabled=true in deploy_app or push_code",
    "verify": "Poll app_status for metricsEndpoint field"
  },
  "histogramBuckets": [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5]
}
```
This file is overridable by operators at `IAF_METRICS_STANDARDS_FILE` (same pattern as issue #1).

**New file: `internal/mcp/resources/metrics_standards.go`**:
- `RegisterMetricsStandards(server *gomcp.Server, deps *tools.Dependencies)`
- Mirrors the pattern in `internal/orgstandards/loader.go` (issue #1): use `deps.MetricsStandardsLoader` (a `*orgstandards.Loader`) to read content, or fall back to the embedded default if no override is configured
- URI: `iaf://org/metrics-standards`; MIMEType: `application/json`

**New file: `internal/mcp/prompts/metrics_guide.go`**:
- `RegisterMetricsGuide(server *gomcp.Server, deps *tools.Dependencies)`
- Same structure as `language_guide.go`: `language` argument; `metricsGuides` map keyed by language alias
- Content per language (hardcoded Markdown in the Go file — no embedded file needed):

  | Language | Client library | Minimal import |
  |----------|---------------|----------------|
  | Go | `github.com/prometheus/client_golang/prometheus` | `promhttp.Handler()` |
  | Node.js | `prom-client` | `npm install prom-client` |
  | Python | `prometheus_client` | `pip install prometheus-client` |
  | Java | `io.micrometer:micrometer-registry-prometheus` | Spring Boot Actuator auto-config |
  | Ruby | `prometheus-client` | `gem install prometheus-client` |

- Guide structure per language:
  1. Library installation
  2. Expose `/metrics` endpoint (minimal working code)
  3. Instrument HTTP requests with RED counters/histograms
  4. How to set `spec.metrics.enabled: true` via `deploy_app` or `push_code`
  5. Verify: `app_status` returns `metricsEndpoint`

**Updated `internal/mcp/tools/deploy.go`** (`DeployAppInput`):
```go
type MetricsInput struct {
    Enabled bool   `json:"enabled"`
    Port    int32  `json:"port,omitempty"`
    Path    string `json:"path,omitempty"`
}

type DeployAppInput struct {
    // ... existing fields ...
    Metrics *MetricsInput `json:"metrics,omitempty" jsonschema:"optional"`
}
```
- When `input.Metrics != nil`, set `app.Spec.Metrics = &iafv1alpha1.MetricsConfig{...}`
- Response struct: add `MetricsRegistered bool` field; set to `input.Metrics != nil && input.Metrics.Enabled`

**Updated `internal/mcp/tools/push_code.go`** — same `Metrics *MetricsInput` field addition.

**Updated `internal/mcp/tools/status.go`** (`AppStatusOutput` or equivalent map):
- When `app.Spec.Metrics != nil && app.Spec.Metrics.Enabled`, add `metricsEndpoint` to result:
  ```go
  metricsEndpoint = fmt.Sprintf("http://%s.%s%s", app.Name, deps.BaseDomain, metricsPath)
  ```

**Updated `internal/controller/application_controller.go`** — `reconcileDeployment`:
- Add `app.kubernetes.io/version` label to Deployment and pod template:
  ```go
  version := "unknown"
  if app.Status.LatestImage != "" {
      // extract tag from image reference, e.g. "registry/img:sha256-abc" → "sha256-abc"
      if parts := strings.SplitN(app.Status.LatestImage, ":", 2); len(parts) == 2 {
          version = parts[1]
      }
  }
  labels["app.kubernetes.io/version"] = sanitizeLabelValue(version)
  ```
- `sanitizeLabelValue(s string) string` — truncate to 63 chars, replace invalid chars with `-` (K8s label value rules: `[a-z0-9A-Z-._]`, max 63 chars, no leading/trailing `-._`)

**Updated `internal/mcp/server.go`**:
- Call `resources.RegisterMetricsStandards(server, deps)` and `prompts.RegisterMetricsGuide(server, deps)`

**Updated `internal/mcp/resources/resources_test.go`** — add `TestMetricsStandards` test.

**New file: `internal/mcp/prompts/metrics_guide_test.go`**:
- Table-driven: for each language (`go`, `nodejs`, `python`, `java`, `ruby`), call `metrics-guide` prompt and verify response contains library name and `/metrics` path

### Data / API Changes

**`DeployAppInput`** — new optional `metrics` field.
**`AppStatusOutput`** — new optional `metricsEndpoint` string field.
**`iaf://org/metrics-standards`** — new MCP resource.
**`metrics-guide`** — new MCP prompt.
**Deployment labels** — `app.kubernetes.io/version` added (non-breaking; new label only).

### Multi-tenancy & Shared Resource Impact

`iaf://org/metrics-standards` is a global read-only resource — no tenant data, safe for all agents to read. The `metricsEndpoint` URL is derived from the app's own name and the platform's `BaseDomain` — no cross-tenant information leakage. Prometheus scraping is the shared resource; the `ServiceMonitor` design in issue #29 ensures it is scoped to the correct namespace.

### Security Considerations

- **Label value sanitization**: `app.kubernetes.io/version` is derived from `app.Status.LatestImage`, which itself comes from the kpack build output (not directly from user input). Still, the image tag can contain arbitrary characters — `sanitizeLabelValue` is required to avoid rejected Deployment specs.
- **`metricsEndpoint` URL generation**: uses `app.Name` (already validated by issue #14) and platform `BaseDomain` — no injection risk.
- `iaf://org/metrics-standards` is read-only, platform-controlled content. Operators who update via `IAF_METRICS_STANDARDS_FILE` are trusted (same trust level as platform config).

### Resource & Performance Impact

- New MCP resource and prompt: in-memory, negligible.
- Label injection on Deployments: adds one label per Deployment update — negligible.

### Migration / Compatibility

- `metrics` field on `deploy_app`/`push_code` is optional — existing callers unaffected.
- Deployment label addition is non-breaking (new label only; existing workloads unaffected until next reconcile).
- Depends on issue #29 `spec.metrics` CRD field being deployed first.

### Open Questions for Developer

1. **`MetricsStandardsLoader` in `Dependencies`**: add `MetricsStandardsLoader *orgstandards.Loader` to `tools.Dependencies`. Wire it in `cmd/apiserver/main.go` and `cmd/mcpserver/main.go` using the same `IAF_METRICS_STANDARDS_FILE` env var pattern (or use a dedicated `IAF_METRICS_STANDARDS_FILE` var to allow separate operator overrides).
2. **Image tag extraction**: `strings.SplitN(image, ":", 2)` breaks for digest-only references like `registry/img@sha256:abc`. Handle the `@sha256:` case separately if needed.
3. **`metricsEndpoint` TLS**: once issue #12 (TLS) is implemented, the `metricsEndpoint` URL should use `https://` when TLS is enabled. For now, always `http://`.
4. **`deploy_app` response**: currently returns a string message. The `MetricsRegistered` bool should be added to the response struct (or map). Confirm the existing return type to avoid breaking the MCP schema.
