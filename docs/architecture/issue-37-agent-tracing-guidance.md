# Architecture: Agent Tracing Guidance — MCP Resources and Prompts (Issue #37)

## Technical Design

### Approach

Deliver the platform's tracing standard through a `iaf://org/tracing-standards` resource (structured JSON, embedded file) and a `tracing-guide` prompt (parameterized by language). Additionally, add `traceExploreUrl` to `app_status` when `IAF_TEMPO_URL` is configured, and update `logging-guide` examples to include trace-log correlation code. Follows the established pattern from issues #30 and #33. Depends on issue #36 (Tempo + OTel Collector deployed).

### Changes Required

**New file: `config/org-standards/tracing-standards.json`** (embedded default):
```json
{
  "version": "1.0",
  "sdk": "OpenTelemetry",
  "exporter": "OTLP",
  "endpointSource": "OTEL_EXPORTER_OTLP_ENDPOINT environment variable — injected automatically by the IAF platform. Never hardcode this value.",
  "serviceNameSource": "OTEL_SERVICE_NAME environment variable — set to your IAF application name automatically.",
  "requiredSpanAttributes": [
    {"name": "http.method",               "description": "HTTP method: GET, POST, etc."},
    {"name": "http.route",                "description": "Route template: /users/:id, not /users/123"},
    {"name": "http.status_code",          "description": "HTTP response status code (integer)"},
    {"name": "http.response_content_length", "description": "Response body size in bytes (optional but recommended)"}
  ],
  "traceLogCorrelation": {
    "description": "Add trace_id and span_id to every structured log line to enable Grafana trace-to-log correlation.",
    "requiredLogFields": ["trace_id", "span_id"],
    "howToExtract": "Use the OTel SDK's span context API to read the current trace and span IDs at log time."
  },
  "autoInstrumentation": {
    "nodejs":  "Available — @opentelemetry/auto-instrumentations-node",
    "python":  "Available — opentelemetry-instrument CLI wrapper",
    "java":    "Available — opentelemetry-javaagent.jar (-javaagent flag)",
    "go":      "Not available — manual SDK required",
    "ruby":    "Available — opentelemetry-instrumentation-all gem"
  },
  "securityNote": "Span attributes must never contain secrets, tokens, or PII. Traces are stored and queryable by platform operators.",
  "verification": "Use app_status to get the traceExploreUrl field (when available) to verify traces are flowing."
}
```
Overridable via `IAF_TRACING_STANDARDS_FILE` env var.

**New file: `internal/mcp/resources/tracing_standards.go`**:
- `RegisterTracingStandards(server *gomcp.Server, deps *tools.Dependencies)`
- URI: `iaf://org/tracing-standards`; MIMEType: `application/json`
- Pattern identical to `logging_standards.go` and `metrics_standards.go`

**New file: `internal/mcp/prompts/tracing_guide.go`**:
- `RegisterTracingGuide(server *gomcp.Server, deps *tools.Dependencies)`
- `language` argument; same alias map
- `tracingGuides` map per language:

  **Node.js** (auto-instrumentation path, then manual):
  ```
  npm install @opentelemetry/auto-instrumentations-node @opentelemetry/exporter-trace-otlp-http
  # startup: NODE_OPTIONS="--require @opentelemetry/auto-instrumentations-node/register" node app.js
  # Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
  ```
  Trace-log correlation: `pino-opentelemetry` or manual extraction via `trace.getActiveSpan()?.spanContext()`.

  **Python** (auto-instrumentation path, then manual):
  ```
  pip install opentelemetry-distro opentelemetry-exporter-otlp
  opentelemetry-bootstrap -a install
  # startup: opentelemetry-instrument python app.py
  ```
  Trace-log correlation: `opentelemetry-instrumentation-logging` (auto-injects `trace_id` into stdlib `logging`).

  **Java** (auto-instrumentation via javaagent):
  ```
  # Download opentelemetry-javaagent.jar
  # JVM flag: -javaagent:/path/to/opentelemetry-javaagent.jar
  # Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
  ```
  Trace-log correlation: logback MDC is auto-populated when using javaagent.

  **Go** (manual only):
  ```go
  import "go.opentelemetry.io/otel"
  import "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
  // Init: reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME from env
  exp, _ := otlptracehttp.New(ctx)
  tp := trace.NewTracerProvider(trace.WithBatcher(exp))
  otel.SetTracerProvider(tp)
  ```
  Trace-log correlation: `span.SpanContext().TraceID().String()` + `span.SpanContext().SpanID().String()`.

  **Ruby** (auto-instrumentation via gem):
  ```ruby
  gem 'opentelemetry-sdk', 'opentelemetry-instrumentation-all', 'opentelemetry-exporter-otlp'
  # In config/initializers/opentelemetry.rb:
  OpenTelemetry::SDK.configure { |c| c.use_all }
  # Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
  ```
  Trace-log correlation: `OpenTelemetry::Trace.current_span.context.trace_id.unpack1('H*')`.

- Each language guide ends with:
  - "Do not hardcode `OTEL_EXPORTER_OTLP_ENDPOINT`. The platform injects it automatically."
  - "Never add secrets, tokens, or PII as span attributes."
  - "Verify: after deploying, call `app_status` and check the `traceExploreUrl` field."

**Updated `internal/mcp/tools/status.go`** (or wherever `app_status` response is built):

```go
type AppStatusOutput struct {
    // ... existing fields ...
    TraceExploreURL string `json:"traceExploreUrl,omitempty"`
}
```

When `deps.TempoURL != ""`:
```go
// Generate Grafana Explore deep link pre-filtered to this app's service.name
// URL format: {grafanaURL}/explore?orgId=1&left={"datasource":"Tempo","queries":[{"query":"{service.name=\"<appName>\"}","queryType":"traceql"}],"range":{"from":"now-1h","to":"now"}}
output.TraceExploreURL = buildTraceExploreURL(deps.TempoURL, app.Name)
```

`buildTraceExploreURL(grafanaURL, appName string) string`:
- `appName` is already validated (K8s name rules from issue #14) — use directly in the URL without additional sanitization, but URL-encode the JSON parameter string.
- The URL is a Grafana Explore deep link; `grafanaURL` comes from `IAF_TEMPO_URL` (platform config, not agent input).

**New field `TempoURL string` on `tools.Dependencies`** — from `IAF_TEMPO_URL` env var.

**Updated `internal/mcp/server.go`**:
- `resources.RegisterTracingStandards(server, deps)`
- `prompts.RegisterTracingGuide(server, deps)`

**Updated `config/org-standards/logging-standards.json`** (issue #33 file):
- `trace_id` and `span_id` promoted from `recommendedFields` to their own `correlationFields` section with note: "Add these when tracing is enabled (OTel SDK present). Required for Grafana trace-to-log correlation."

**Updated `config/org-standards/coding-standards.json`** (issue #1 file):
- Add `"tracing"` section pointing to `iaf://org/tracing-standards`

**Updated `internal/mcp/prompts/deploy_guide.go`**:
- Add tracing section: "Tracing: the platform injects `OTEL_EXPORTER_OTLP_ENDPOINT` automatically. Use `tracing-guide` prompt to add OTel to your app."

**Test files**:

`internal/mcp/resources/tracing_standards_test.go`:
- `TestTracingStandards_ReturnsValidJSON`: verify required keys present

`internal/mcp/prompts/tracing_guide_test.go`:
- Table-driven: each language guide references correct package name and `OTEL_EXPORTER_OTLP_ENDPOINT`
- `TestTracingGuide_NoHardcodedEndpoints`: verify no guide contains hardcoded `otel-collector.monitoring` (guides must use env vars)

`internal/mcp/tools/status_test.go` (extend existing):
- `TestAppStatus_TraceExploreURL_Set`: `deps.TempoURL = "http://grafana.localhost"` → response contains `traceExploreUrl`
- `TestAppStatus_TraceExploreURL_Absent`: `deps.TempoURL = ""` → `traceExploreUrl` absent from response

### Data / API Changes

- New MCP resource: `iaf://org/tracing-standards`
- New MCP prompt: `tracing-guide`
- `app_status` response: new optional `traceExploreUrl` field
- `tools.Dependencies`: new `TempoURL string` field
- No CRD changes, no K8s object changes.

### Multi-tenancy & Shared Resource Impact

`iaf://org/tracing-standards` is global and read-only — no tenant data. `traceExploreUrl` is generated server-side from the app's validated name and namespace — no cross-tenant leakage. The Grafana Explore link opens a view scoped to this app's `service.name`; it relies on Grafana authentication to prevent unauthorized access.

### Security Considerations

- **`traceExploreUrl` generation**: `appName` comes from the validated `Application` CR name (K8s name rules). `grafanaURL` comes from `IAF_TEMPO_URL` — platform config, not agent input. The JSON parameter is URL-encoded. No injection risk.
- **Guide content**: every language guide explicitly warns "Never add secrets, tokens, or PII as span attributes." This is the tracing equivalent of the logging prohibition.
- **Auto-instrumentation scope**: the guide recommends auto-instrumentation (zero code changes). Auto-instrumentation traces HTTP headers, SQL queries, and outbound requests — these can contain sensitive data. Agents must review what their OTel SDK auto-instruments and disable sensitive instrumentation if needed (e.g., `OTEL_INSTRUMENTATION_HTTP_CAPTURE_HEADERS=false`). Add this note to the guide.

### Resource & Performance Impact

- New MCP resource and prompt: in-memory, negligible.
- `buildTraceExploreURL` is a string operation per `app_status` call — negligible.

### Migration / Compatibility

- `traceExploreUrl` is optional — existing `app_status` callers unaffected.
- `TempoURL = ""` = no URL in response = zero behavior change.
- Depends on issue #36 (Tempo deployed) for the `traceExploreUrl` to actually work, but the code can ship independently.

### Open Questions for Developer

1. **Grafana Explore URL format**: the URL format for Tempo datasource explore links changed between Grafana 10 and 11. Confirm the exact format for the kube-prometheus-stack Grafana version pinned in issue #29. Use the TraceQL format: `{service.name="<appName>"}`.
2. **`IAF_TEMPO_URL` value**: this should be the Grafana base URL (e.g., `http://grafana.localhost`), not the Tempo backend URL. The explore link is a Grafana UI link, not a Tempo API link. Name the env var accordingly to avoid confusion with `IAF_LOKI_URL` (which points to the Loki API).
3. **Auto-instrumentation and `Dockerfile`**: for Node.js and Python, auto-instrumentation requires modifying the startup command (add `--require` or `opentelemetry-instrument`). Since IAF uses Cloud Native Buildpacks, agents can set the startup command via the `Procfile`. Add this note to the Node.js and Python guides.
4. **`TestTracingGuide_NoHardcodedEndpoints` test**: use a regex or string search for `otel-collector.monitoring` in the guide text to catch accidental hardcoding. This is a correctness requirement, not just a style check.
