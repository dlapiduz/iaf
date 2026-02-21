# Architecture: Grafana Tempo + OpenTelemetry Collector for Distributed Tracing (Issue #36)

## Technical Design

### Approach

Add Grafana Tempo (single-binary, filesystem storage) and the OpenTelemetry Collector (central Deployment, not DaemonSet — traces are push-based via OTLP) to the existing Helmfile at `config/helmfile.yaml`, extending the stack from issues #29 and #31. The OTel Collector is exposed as a `ClusterIP` Service at `otel-collector.monitoring.svc.cluster.local:4317/4318`. The IAF controller injects `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, and `OTEL_RESOURCE_ATTRIBUTES` into every Deployment it creates (when `IAF_OTEL_ENDPOINT` is set). Platform components (apiserver, controller, mcpserver) are instrumented with the OTel Go SDK.

### Changes Required

**Updated `config/helmfile.yaml`** — add Tempo and OTel Collector releases:
```yaml
repositories:
  # ... existing repos (prometheus-community, grafana) ...
  - name: open-telemetry
    url: https://open-telemetry.github.io/opentelemetry-helm-charts

releases:
  # ... kube-prometheus-stack, loki, grafana-alloy (issues #29, #31) ...

  - name: tempo
    namespace: monitoring
    chart: grafana/tempo
    version: "1.x"
    values:
      - helm-values/tempo.yaml

  - name: opentelemetry-collector
    namespace: monitoring
    chart: open-telemetry/opentelemetry-collector
    version: "0.x"
    values:
      - helm-values/opentelemetry-collector.yaml
```

**New file: `config/helm-values/tempo.yaml`**:
```yaml
tempo:
  storage:
    trace:
      backend: local
      local:
        path: /var/tempo/traces
  ingester:
    trace_idle_period: 30s
    max_block_duration: 5m
  compactor:
    compaction:
      block_retention: 168h   # 7 days

persistence:
  enabled: true
  size: 5Gi

# Receive OTLP from the OTel Collector
tempo:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
        http:
          endpoint: 0.0.0.0:4318
```

**New file: `config/helm-values/opentelemetry-collector.yaml`**:
```yaml
mode: deployment     # central deployment, not DaemonSet — traces are push-based

# Expose OTLP endpoints as ClusterIP Service
service:
  enabled: true
  type: ClusterIP

config:
  receivers:
    otlp:
      protocols:
        grpc: { endpoint: "0.0.0.0:4317" }
        http: { endpoint: "0.0.0.0:4318" }

  processors:
    batch:
      timeout: 5s
      send_batch_size: 1000
    # Enrich spans with K8s pod metadata
    k8sattributes:
      auth_type: serviceAccount
      passthrough: false
      extract:
        metadata: [k8s.namespace.name, k8s.pod.name, k8s.node.name, k8s.pod.uid]
        labels:
          - tag_name: app.kubernetes.io/name
            key: app.kubernetes.io/name
            from: pod

  exporters:
    otlp:
      endpoint: tempo.monitoring.svc.cluster.local:4317
      tls:
        insecure: true   # in-cluster, no TLS needed

  service:
    pipelines:
      traces:
        receivers: [otlp]
        processors: [k8sattributes, batch]
        exporters: [otlp]

# RBAC for k8sattributes processor
clusterRole:
  create: true
  rules:
    - apiGroups: [""]
      resources: [pods, namespaces, nodes]
      verbs: [get, list, watch]
    - apiGroups: [apps]
      resources: [replicasets]
      verbs: [get, list, watch]
```

**New file: `config/deploy/tempo-datasource-configmap.yaml`** — Grafana datasource ConfigMap:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tempo-datasource
  namespace: monitoring
  labels:
    grafana_datasource: "1"
data:
  tempo-datasource.yaml: |
    apiVersion: 1
    datasources:
      - name: Tempo
        type: tempo
        url: http://tempo.monitoring.svc.cluster.local:3100
        access: proxy
        jsonData:
          tracesToLogsV2:
            datasourceUid: loki    # correlate traces → Loki logs
            filterByTraceID: true
            filterBySpanID: false
            customQuery: true
            query: '{namespace="${__span.tags["k8s.namespace.name"]}"}'
          tracesToMetrics:
            datasourceUid: prometheus
            queries:
              - name: "Request rate"
                query: 'sum(rate(http_requests_total{namespace="$namespace", app="$app"}[5m]))'
          serviceMap:
            datasourceUid: prometheus
          nodeGraph:
            enabled: true
```

**New file: `config/grafana/dashboards/traces-overview.json`** — Traces Overview dashboard:
- `namespace` variable (regex `iaf-.*`)
- Panels: request rate from spans, error rate, p50/p95/p99 latency histogram, recent traces list (Tempo panel)
- Wrapped in existing `config/deploy/grafana-dashboards-configmap.yaml`

**Updated `internal/controller/application_controller.go`** — `reconcileDeployment`:

Add OTel env var injection. Read `IAF_OTEL_ENDPOINT` platform env var at startup; inject into Deployments only if set:

```go
func (r *ApplicationReconciler) otelEnvVars(app *iafv1alpha1.Application) []corev1.EnvVar {
    if r.OTelEndpoint == "" {
        return nil
    }
    return []corev1.EnvVar{
        {Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: r.OTelEndpoint},
        {Name: "OTEL_SERVICE_NAME",            Value: app.Name},
        {Name: "OTEL_RESOURCE_ATTRIBUTES",
         Value: fmt.Sprintf("namespace=%s,app.version=%s", app.Namespace, sanitizedVersion(app))},
    }
}
```

Merge into the Deployment's container env vars **without overriding** any env var already defined by the app (agent-supplied env vars take precedence):
```go
// Merge: platform vars first, then app vars; app vars override
envVars = mergeEnvVars(r.otelEnvVars(app), appEnvVars)
```

`mergeEnvVars(platform, app []corev1.EnvVar) []corev1.EnvVar` — starts with platform vars, then appends app vars if their `Name` doesn't already exist. This ensures agents can opt out by setting `OTEL_EXPORTER_OTLP_ENDPOINT=""`.

New field on `ApplicationReconciler`:
```go
type ApplicationReconciler struct {
    // ... existing fields ...
    OTelEndpoint string   // from IAF_OTEL_ENDPOINT env var; empty = no injection
}
```

Wire in `cmd/controller/main.go`: `OTelEndpoint: os.Getenv("IAF_OTEL_ENDPOINT")`.

**New dependency: `go.opentelemetry.io/otel` + OTLP exporter** — for platform component instrumentation:

*`cmd/apiserver/main.go`*:
- Initialize OTel tracer provider with OTLP/HTTP exporter (endpoint from `OTEL_EXPORTER_OTLP_ENDPOINT`)
- Wrap Echo with `otelecho.Middleware("iaf-apiserver")` (package `go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho`)
- Each HTTP request creates a root span with `http.method`, `http.route`, `http.status_code` attributes
- Add `iaf.session_id` attribute from the `X-IAF-Session` header

*`cmd/controller/main.go`*:
- Create span per reconcile call: `ctx, span := tracer.Start(ctx, "reconcile", ...)`
- Attributes: `iaf.app_name`, `iaf.namespace`, `iaf.phase_from`, `iaf.phase_to`
- End span at return

*`cmd/mcpserver/main.go`*:
- Create span per MCP tool call: wrap handler invocation
- Attributes: `mcp.tool_name`, `iaf.session_id`

**Updated `config/deploy/platform.yaml`**:
- Add `IAF_OTEL_ENDPOINT: "http://otel-collector.monitoring.svc.cluster.local:4318"` to platform component env vars (commented out by default; operators uncomment to enable)

**Test files**:

`internal/controller/application_controller_test.go` (extend from issue #16):
- `TestReconcile_OTelEnvInjected`: `r.OTelEndpoint = "http://..."` → Deployment contains `OTEL_EXPORTER_OTLP_ENDPOINT`
- `TestReconcile_OTelEnvNotInjected`: `r.OTelEndpoint = ""` → Deployment does NOT contain `OTEL_EXPORTER_OTLP_ENDPOINT`
- `TestReconcile_OTelEnvNotOverridden`: app defines `OTEL_EXPORTER_OTLP_ENDPOINT` in its own env → platform value not added

### Data / API Changes

**`ApplicationReconciler`** — new `OTelEndpoint string` field (internal, not in CRD).
**Deployment** (K8s resource) — new env vars `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` (when `IAF_OTEL_ENDPOINT` is set).
**`app_status`** — gains `traceExploreUrl` field when `IAF_TEMPO_URL` is set (see issue #37 for details; this issue delivers the infrastructure, #37 delivers the MCP guidance).

### Multi-tenancy & Shared Resource Impact

- The OTel Collector is a **shared** central Deployment. All apps and platform components send traces to it. A misbehaving agent can flood it with high-cardinality spans, consuming Collector memory.
  - Mitigation: `batch` processor with `send_batch_size: 1000` and `timeout: 5s` limits throughput. If volume grows, add a `memorylimiter` processor.
- Tempo is a shared backend. Traces from all namespaces are stored together; isolation is by `k8s.namespace.name` resource attribute. Agents do not query Tempo directly (Grafana is the only interface).
- `OTEL_EXPORTER_OTLP_ENDPOINT` points to `otel-collector.monitoring` — accessible cluster-wide. This is intentional.

### Security Considerations

- **OTel Collector RBAC**: `k8sattributes` processor needs `get`/`list`/`watch` on `pods`, `namespaces`, `nodes`. This is a ClusterRole. Review carefully — `nodes` access is broader than needed if all apps run in namespaced pods. If possible, restrict to `pods` and `namespaces` only (test whether `k8sattributes` works without `nodes`).
- **OTLP endpoints not authenticated**: the OTel Collector's OTLP `4317`/`4318` ports accept spans from any pod. Spans are low-sensitivity (trace metadata, not log content). An agent can inject fake spans — acceptable risk at current scale; add authentication in a future iteration.
- **`OTEL_SERVICE_NAME` = `app.Name`**: app name is already validated (issue #14, k8s label rules). No additional sanitization needed.
- **`OTEL_RESOURCE_ATTRIBUTES` injection**: `app.Namespace` is a namespace name (already a validated K8s name); `sanitizedVersion()` uses the same label sanitizer from issue #30. No injection risk.
- **Platform span attributes**: `iaf.session_id` is added to API spans. Session IDs are non-sensitive identifiers (not tokens). Confirm the session ID stored in spans does not expose the original auth token.
- **Needs security review** label: add.

### Resource & Performance Impact

- Tempo (single-binary): ~256Mi memory, 5Gi PVC.
- OTel Collector (central Deployment): ~128Mi memory on a quiet cluster.
- OTel SDK in platform components: adds ~5-15ms per request for span creation/export (batched asynchronously — actual latency impact is negligible).
- Env var injection on Deployments: 3 additional env vars per Deployment — negligible.

### Migration / Compatibility

- `IAF_OTEL_ENDPOINT` unset = no env var injection = zero behavior change for existing Deployments.
- Existing apps already running when `IAF_OTEL_ENDPOINT` is first set will have env vars injected on next reconcile (triggering a rolling restart). This is expected and correct.
- Depends on issues #29 and #31 (Helmfile structure and Grafana instance) being deployed first.

### Open Questions for Developer

1. **OTel Collector mode — Deployment vs DaemonSet**: Deployment (central) is recommended for trace collection (push-based OTLP). Grafana Alloy DaemonSet already handles log collection. Confirm this is acceptable — if the team wants a single agent, consider whether Alloy can also handle OTLP trace forwarding (it can, via its `otelcol.receiver.otlp` component; evaluate as an alternative to a separate Collector).
2. **`mergeEnvVars` precedence**: agent-supplied vars override platform vars. The agent sets `OTEL_EXPORTER_OTLP_ENDPOINT: ""` to disable tracing — this is supported. Confirm the merge logic handles empty-string values correctly (should suppress injection, not override with empty).
3. **`otelecho` middleware**: `go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho` is community-maintained. Verify it is compatible with Echo v4 (the version used by IAF).
4. **Controller reconcile span context**: `ctrl.Request.Context()` is the parent context for reconcile. Wrap the full `Reconcile` method body in a span, not just sub-functions, to capture total reconcile duration in one trace.
5. **`IAF_OTEL_ENDPOINT` in `cmd/apiserver/main.go`**: the apiserver itself should also export its own traces to the Collector. Wire the same env var for the apiserver's OTel exporter configuration.
