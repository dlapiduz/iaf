# Architecture: Loki + Grafana Alloy for Log Aggregation (Issue #31)

## Technical Design

### Approach

Add Loki (single-binary, filesystem storage) and Grafana Alloy (DaemonSet log collector) to the existing Helmfile at `config/helmfile.yaml`, extending the observability stack from issue #29. Loki is provisioned as a Grafana datasource via ConfigMap. The `app_logs` MCP tool gains optional Loki integration: when `IAF_LOKI_URL` is set, it queries the Loki HTTP API for historical logs; otherwise it falls back to the existing K8s pod logs behavior unchanged. Platform components switch to structured JSON logging.

### Changes Required

**Updated `config/helmfile.yaml`** — add Loki and Alloy releases after kube-prometheus-stack:
```yaml
repositories:
  - name: grafana
    url: https://grafana.github.io/helm-charts
  # (prometheus-community already present from issue #29)

releases:
  # ... kube-prometheus-stack (issue #29) ...

  - name: loki
    namespace: monitoring
    chart: grafana/loki
    version: "6.x"
    values:
      - helm-values/loki.yaml

  - name: grafana-alloy
    namespace: monitoring
    chart: grafana/alloy
    version: "0.x"
    values:
      - helm-values/grafana-alloy.yaml
```

**New file: `config/helm-values/loki.yaml`**:
```yaml
loki:
  auth_enabled: false    # single-tenant mode; multi-tenant is a future concern
  commonConfig:
    replication_factor: 1
  storage:
    type: filesystem
  schemaConfig:
    configs:
      - from: "2024-01-01"
        store: tsdb
        object_store: filesystem
        schema: v13
        index:
          prefix: loki_index_
          period: 24h

singleBinary:
  replicas: 1
  persistence:
    enabled: true
    size: 5Gi

# Disable components not needed in single-binary mode
backend:
  replicas: 0
read:
  replicas: 0
write:
  replicas: 0
```

**New file: `config/helm-values/grafana-alloy.yaml`**:
```yaml
alloy:
  configMap:
    create: true
    content: |
      // Kubernetes pod log discovery
      discovery.kubernetes "pods" {
        role = "pod"
      }

      // Relabel: extract namespace, pod, container, app label
      discovery.relabel "pod_logs" {
        targets = discovery.kubernetes.pods.targets
        rule { source_labels = ["__meta_kubernetes_namespace"] target_label = "namespace" }
        rule { source_labels = ["__meta_kubernetes_pod_name"] target_label = "pod" }
        rule { source_labels = ["__meta_kubernetes_pod_container_name"] target_label = "container" }
        rule {
          source_labels = ["__meta_kubernetes_pod_label_app_kubernetes_io_name"]
          target_label  = "app"
        }
        rule {
          source_labels = ["__meta_kubernetes_namespace"]
          regex         = "iaf-.*"
          target_label  = "component"
          replacement   = "app"
        }
        rule {
          source_labels = ["__meta_kubernetes_namespace"]
          regex         = "iaf-system"
          target_label  = "component"
          replacement   = "platform"
        }
      }

      loki.source.kubernetes "pod_logs" {
        targets    = discovery.relabel.pod_logs.output
        forward_to = [loki.write.default.receiver]
      }

      loki.write "default" {
        endpoint {
          url = "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push"
        }
      }

# DaemonSet mode for node-level log access
controller:
  type: daemonset

# RBAC needed for pod log access
rbac:
  create: true
```

**New file: `config/deploy/loki-datasource-configmap.yaml`** — Grafana datasource ConfigMap:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: loki-datasource
  namespace: monitoring
  labels:
    grafana_datasource: "1"
data:
  loki-datasource.yaml: |
    apiVersion: 1
    datasources:
      - name: Loki
        type: loki
        url: http://loki.monitoring.svc.cluster.local:3100
        access: proxy
        isDefault: false
```

**New file: `config/grafana/dashboards/platform-logs.json`** — Platform Logs dashboard:
- Log panel per component (`apiserver`, `controller`, `mcpserver`) filtered by `component="platform"`
- Error rate panel: `sum(count_over_time({component="platform", level="error"}[5m]))`
- Wrapped in ConfigMap in `config/deploy/grafana-dashboards-configmap.yaml` (extend existing)

**Updated `internal/mcp/tools/logs.go`** — `RegisterAppLogsWithClientset`:

```go
// New optional input field:
type AppLogsInput struct {
    // ... existing fields ...
    Since   string `json:"since,omitempty" jsonschema:"optional - time duration (e.g. 1h, 30m) or RFC3339 timestamp for historical log queries via Loki"`
}
```

Add `LokiURL string` to `tools.Dependencies` (read from `IAF_LOKI_URL` env var).

When `deps.LokiURL != ""`:
1. Build LogQL query: `{namespace="<ns>", app="<appName>"}` — namespace from `deps.ResolveNamespace`
2. Parse `input.Since` as duration or RFC3339 timestamp → `start` parameter for Loki `/loki/api/v1/query_range`
3. Default `since`: `time.Now().Add(-5 * time.Minute)` (returns ~100 lines of recent logs)
4. Execute HTTP GET to `{deps.LokiURL}/loki/api/v1/query_range?query=...&start=...&limit=<lines>`
5. Parse response: `data.result[].values[]` (each value is `[timestamp, logLine]`) → join log lines
6. Return result with `source: "loki"` field added

When `deps.LokiURL == ""`: use existing K8s pod logs path (no change to current behavior).

**New file: `internal/loki/client.go`**:
```go
type Client struct { BaseURL string; HTTPClient *http.Client }
type QueryResult struct { Lines []string; Source string }
func (c *Client) QueryRange(ctx, namespace, appName, since string, limit int64) (*QueryResult, error)
```
This is testable in isolation — `RegisterAppLogsWithClientset` uses a `LokiQuerier` interface so tests can mock it.

**New file: `internal/loki/client_test.go`** — test with `httptest.NewServer` returning canned Loki JSON responses.

**Updated platform component logging** — structured JSON to stdout:

*`cmd/apiserver/main.go`*: switch from Echo's default logger to `slog.NewJSONHandler(os.Stdout, nil)`. Echo has a `Logger` interface — replace with a slog-backed adapter, or use `zerolog`/`slog` for all platform logs (pick `slog` — stdlib, no dependency).

*`cmd/controller/main.go`*: controller-runtime already supports `zap` logger; configure zap JSON output:
```go
ctrl.SetLogger(zap.New(zap.UseDevMode(false)))  // JSON in production mode
```

*`cmd/mcpserver/main.go`*: same slog pattern.

Minimum fields in every platform log line: `time`, `level`, `msg`, `component`.

**Tests** (new `internal/mcp/tools/logs_test.go` additions):
- `TestAppLogs_LokiEnabled`: mock Loki server returns canned response → tool returns log lines with `source: "loki"`
- `TestAppLogs_LokiSince_Duration`: `since: "1h"` → query `start` is ~1 hour ago
- `TestAppLogs_LokiSince_RFC3339`: `since: "2026-01-01T00:00:00Z"` → query `start` matches timestamp
- `TestAppLogs_LokiFallback`: `LokiURL == ""` → falls back to K8s pod logs (existing test coverage)
- `TestAppLogs_LokiNamespaceIsolation`: LogQL query always includes `namespace=<session-ns>` label

### Data / API Changes

**`AppLogsInput`** — new optional `since` field.
**MCP tool response** — new `source` field: `"loki"` or `"kubernetes"` (indicates which backend served the logs).
**`tools.Dependencies`** — new `LokiURL string` field.

### Multi-tenancy & Shared Resource Impact

- **Critical isolation control**: the Loki LogQL query MUST include `namespace="<session-namespace>"`. The namespace is resolved from the session ID (via `deps.ResolveNamespace`) — never from agent-supplied input. An agent cannot query another session's logs by specifying a different app name because the namespace filter limits results to their own namespace.
- Alloy collects logs from ALL namespaces (necessary for platform visibility). Operators have full log access. Agents access only their namespace's logs via `app_logs`.
- Loki uses single-tenant mode (`auth_enabled: false`) — all logs in one Loki instance, isolated only by namespace label at query time. This is acceptable for the current trust model (platform operators are trusted). A future multi-tenant Loki deployment would use per-tenant token auth.

### Security Considerations

- **Loki namespace isolation**: the `namespace` label in the LogQL query is the only cross-tenant guard. The implementation must use `deps.ResolveNamespace(sessionID)` — never accept a namespace from agent input.
- **Alloy RBAC**: Alloy DaemonSet needs `get`/`list`/`watch` on `pods` and `pods/log` cluster-wide. This is a highly privileged role. Document it explicitly. Ensure Alloy's ServiceAccount has no other permissions. Alloy pods must be in `monitoring` namespace, not accessible from agent namespaces.
- **Log content**: the platform does not filter log content passed through `app_logs`. Log lines may contain sensitive data if the app logs secrets. This is the app developer's responsibility; the `logging-guide` (issue #33) explicitly prohibits logging secrets.
- **Platform log format**: switch to structured JSON must never log token values (`Authorization` header values, `IAF_API_TOKENS`) — audit `cmd/apiserver/main.go` for any existing debug logging that might include request headers.
- **Needs security review** label: add.

### Resource & Performance Impact

- Loki (single-binary): ~256Mi memory on a quiet cluster; 5Gi PVC.
- Grafana Alloy (DaemonSet, one pod per node): ~64Mi memory per node on a local cluster.
- `app_logs` Loki path: one HTTP call per tool invocation — negligible vs. the K8s API call it replaces.

### Migration / Compatibility

- `since` field is optional — existing `app_logs` callers unaffected.
- `source` field in response is additive — existing callers that don't read it are unaffected.
- `IAF_LOKI_URL` unset = existing behavior exactly. No behavioral change until operator sets the env var.
- Depends on issue #29 (Helmfile structure) being deployed first.

### Open Questions for Developer

1. **`LokiQuerier` interface**: define `type LokiQuerier interface { QueryRange(ctx, ns, app, since string, limit int64) ([]string, error) }` in `internal/mcp/tools/logs.go` so the real Loki client and a mock can satisfy it. Wire real client when `deps.LokiURL != ""`.
2. **Loki response format**: the `/loki/api/v1/query_range` response wraps log lines as `[unixNano, logLine]` pairs. Parse carefully — unixNano is a string in Loki's JSON.
3. **`since` format**: support both `"1h"`, `"30m"` (Go duration syntax) and RFC3339 timestamps. `time.ParseDuration` for the first; `time.Parse(time.RFC3339, ...)` for the second. Return a clear error if neither parses.
4. **Log line ordering**: Loki returns results oldest-first by default. Consider reversing for `app_logs` to match the existing K8s pod logs behavior (newest lines last).
5. **Platform log migration**: switching `cmd/controller/main.go` to zap JSON mode changes all controller-runtime log output format. Verify no existing log parsing (e.g., in tests) depends on the current format.
