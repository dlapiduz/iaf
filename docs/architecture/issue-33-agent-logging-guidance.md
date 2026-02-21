# Architecture: Agent Logging Guidance — MCP Resources and Prompts (Issue #33)

## Technical Design

### Approach

Deliver the platform's logging standard to agents through a `iaf://org/logging-standards` resource (structured JSON, embedded file, same pattern as `iaf://org/metrics-standards`) and a `logging-guide` prompt (parameterized by language, same pattern as `metrics-guide`). No new K8s objects, no controller changes — logging is automatic once agents write to stdout in the correct format. This depends on issue #31 (Loki infrastructure) being deployed, but the MCP resource and prompt can ship independently.

### Changes Required

**New file: `config/org-standards/logging-standards.json`** (embedded default):
```json
{
  "version": "1.0",
  "output": {
    "target": "stdout",
    "format": "ndjson",
    "description": "One JSON object per line (JSON Lines / NDJSON). Do not log to files. Kubernetes captures stdout automatically; Grafana Alloy ships it to Loki."
  },
  "requiredFields": [
    {"name": "time",  "type": "string", "format": "RFC3339", "example": "2026-02-19T14:23:01Z"},
    {"name": "level", "type": "string", "values": ["debug","info","warn","error"], "example": "info"},
    {"name": "msg",   "type": "string", "description": "Human-readable message", "example": "request completed"}
  ],
  "recommendedFields": [
    {"name": "app",         "description": "IAF application name — matches Application CR name"},
    {"name": "req_id",      "description": "Request ID from X-Request-ID header"},
    {"name": "method",      "description": "HTTP method for request logs"},
    {"name": "path",        "description": "HTTP path for request logs"},
    {"name": "status",      "description": "HTTP status code (integer)"},
    {"name": "duration_ms", "description": "Request duration in milliseconds (float)"},
    {"name": "error",       "description": "Error message; only on level=error"},
    {"name": "trace_id",    "description": "OTel trace ID for trace-log correlation (see iaf://org/tracing-standards)"},
    {"name": "span_id",     "description": "OTel span ID for trace-log correlation"}
  ],
  "prohibited": [
    "API tokens, passwords, or any value from a Kubernetes Secret",
    "Full request or response bodies (unless in debug mode with explicit opt-in)",
    "PII: names, emails, phone numbers, IP addresses unless required and documented"
  ],
  "collection": {
    "how": "Automatic — Grafana Alloy DaemonSet tails all pod stdout and ships to Loki",
    "query": "Use app_logs MCP tool; operators use Grafana Explore with namespace filter"
  }
}
```
Overridable via `IAF_LOGGING_STANDARDS_FILE` env var.

**New file: `internal/mcp/resources/logging_standards.go`**:
- `RegisterLoggingStandards(server *gomcp.Server, deps *tools.Dependencies)`
- URI: `iaf://org/logging-standards`; MIMEType: `application/json`
- Load from `deps.LoggingStandardsLoader` (optional override) or embedded default
- Pattern identical to `metrics_standards.go`

**New file: `internal/mcp/prompts/logging_guide.go`**:
- `RegisterLoggingGuide(server *gomcp.Server, deps *tools.Dependencies)`
- `language` argument (optional); same alias map as `language_guide.go`
- `loggingGuides` map per language:

  | Language | Library | Notes |
  |----------|---------|-------|
  | Go | `log/slog` (stdlib) | `slog.NewJSONHandler(os.Stdout, nil)` |
  | Node.js | `pino` | NDJSON to stdout by default |
  | Python | `structlog` + `python-json-logger` | configure `ProcessorFormatter` for JSON output |
  | Java | `logback` + `logstash-logback-encoder` | `<appender class="ch.qos.logback.core.ConsoleAppender">` |
  | Ruby | `semantic_logger` | `SemanticLogger.add_appender(io: $stdout, formatter: :json)` |

- Each language entry includes:
  1. Install command
  2. Initialization snippet (copy-paste ready — ~5 lines)
  3. Example: log HTTP request with all recommended fields
  4. Example: log an error with `error` field
  5. `LOG_LEVEL` env var support (bonus guidance per open question)
  6. Explicit note: "Log to stdout only. Do not configure file appenders or log shippers."
  7. "Do not log secrets, tokens, or env var values" warning
  8. Verify: `app_logs <your-app>` after deploy

**Updated `internal/mcp/server.go`**:
- Call `resources.RegisterLoggingStandards(server, deps)`
- Call `prompts.RegisterLoggingGuide(server, deps)`

**Updated `tools.Dependencies`** (`internal/mcp/tools/deps.go`):
```go
type Dependencies struct {
    // ... existing fields ...
    LoggingStandardsLoader *orgstandards.Loader  // nil = use embedded default
}
```

**Updated `config/org-standards/coding-standards.json`** (issue #1 file):
- Add `"logging"` section pointing to `iaf://org/logging-standards`:
  ```json
  "logging": {
    "standard": "iaf://org/logging-standards",
    "summary": "Log to stdout in JSON Lines format. Use the logging-guide prompt for language-specific setup."
  }
  ```

**Updated `internal/mcp/prompts/deploy_guide.go`**:
- Add a "Logging" section to the deploy-guide text:
  ```
  ## Logging
  Log to stdout in JSON Lines format. Logs are automatically collected by the platform.
  - Get the full guide: use the `logging-guide` prompt
  - See the standard: read resource `iaf://org/logging-standards`
  ```

**Updated `internal/mcp/prompts/metrics_guide.go`** (issue #30 file):
- Add note: "Request logs complement RED metrics — log every request AND emit `http_request_duration_seconds`."

**Updated `internal/mcp/prompts/language_guide.go`**:
- Each language's `bestPractices` field: append "Use structured JSON logging (see `logging-guide` prompt)."

**Test files**:

`internal/mcp/resources/logging_standards_test.go` (or extend `resources_test.go`):
- `TestLoggingStandards_ReturnsValidJSON`: resource returns parseable JSON with `requiredFields`, `prohibited`, `output` keys

`internal/mcp/prompts/logging_guide_test.go`:
- Table-driven test for each language: verify response contains the library name and the word `stdout`
- `TestLoggingGuide_NoLanguage`: returns generic guidance (fallback behavior — same as `language-guide`)

### Data / API Changes

- New MCP resource: `iaf://org/logging-standards`
- New MCP prompt: `logging-guide`
- No K8s object changes, no CRD changes, no REST API changes.

### Multi-tenancy & Shared Resource Impact

`iaf://org/logging-standards` is global and read-only — no tenant data. No shared resource impact.

### Security Considerations

- The standard explicitly **prohibits logging secrets**. This is a guardrail (instruction to agents), not enforcement. Enforcement requires log scanning — out of scope, but the prohibition must be present and prominent in the resource JSON and the guide.
- The `prohibited` list in the standards JSON is what agents read before writing logging code. It is the first line of defense against accidental secret leakage into Loki.
- `trace_id` and `span_id` fields in recommended fields: these are non-sensitive correlation IDs. Including them does not leak tenant data.

### Resource & Performance Impact

- New MCP resource and prompt: in-memory, negligible.
- No additional K8s objects.

### Migration / Compatibility

- No breaking changes — additive only.
- `LOG_LEVEL` env var guidance in the prompt is informational; no platform enforcement.
- Dependencies: can ship independently of issue #31 (the resource and prompt are useful without Loki, since agents benefit from knowing the format even before log aggregation is deployed).

### Open Questions for Developer

1. **`LoggingStandardsLoader` wiring**: add to `Dependencies` struct and wire in `cmd/mcpserver/main.go` from `IAF_LOGGING_STANDARDS_FILE` env var. Follow exact same pattern as `CodingStandardsLoader` from issue #1.
2. **`logging-guide` fallback language**: when `language` argument is not provided, return a language-agnostic overview with references to run `logging-guide` with a specific language. Same pattern as `language-guide`.
3. **`structlog` vs `python-json-logger` for Python**: use `python-json-logger` (`pip install python-json-logger`) as the primary recommendation — simpler API. Mention `structlog` as an alternative for those wanting richer context binding.
4. **`MetricsStandardsLoader` and `LoggingStandardsLoader`**: these follow the same pattern. Consider a generic `StandardsLoader` type parameterized by file path, or keep them as separate `*orgstandards.Loader` instances — the simpler approach, given there are only a few standards files.
