# Architecture: kube-prometheus-stack + Platform Instrumentation (Issue #29)

## Technical Design

### Approach

Deploy kube-prometheus-stack (Prometheus Operator + Prometheus + Alertmanager + Grafana + node-exporter + kube-state-metrics) via **Helmfile** — a declarative wrapper around Helm that makes installs and upgrades a single `helmfile apply` command. The IAF apiserver, mcpserver, and controller each expose a `/metrics` endpoint; `ServiceMonitor` CRs tell Prometheus where to scrape. The `Application` CRD gains an optional `spec.metrics` field; when enabled, the controller creates a `ServiceMonitor` in the app's namespace. Grafana dashboards are provisioned via ConfigMap. This design also covers the Helmfile structure that issues #31 (Loki) and #36 (Tempo) will extend.

### Changes Required

**New file: `config/helmfile.yaml`** — root Helmfile, installed by `make setup-local`:
```yaml
repositories:
  - name: prometheus-community
    url: https://prometheus-community.github.io/helm-charts

releases:
  - name: kube-prometheus-stack
    namespace: monitoring
    chart: prometheus-community/kube-prometheus-stack
    version: "69.x"          # pin major+minor; patch auto-upgradeable
    values:
      - helm-values/kube-prometheus-stack.yaml
```

**New file: `config/helm-values/kube-prometheus-stack.yaml`** — local-cluster values:
```yaml
prometheus:
  prometheusSpec:
    # Scrape ServiceMonitors in ALL namespaces (needed for agent namespace monitors)
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorNamespaceSelector: {}
    serviceMonitorSelector: {}
    retention: 7d
    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits: { memory: 1Gi }

grafana:
  adminPassword: "iaf-dev-grafana"   # dev only; rotate in prod
  ingress:
    enabled: false   # we provision a Traefik IngressRoute separately
  sidecar:
    dashboards:
      enabled: true
      searchNamespace: monitoring    # picks up dashboard ConfigMaps
    datasources:
      enabled: true

alertmanager:
  enabled: false   # not needed for local dev

nodeExporter:
  enabled: true

kubeStateMetrics:
  enabled: true
```

**New file: `config/deploy/grafana-ingressroute.yaml`** — Traefik IngressRoute for Grafana:
```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: grafana
  namespace: monitoring
spec:
  entryPoints: [web]
  routes:
    - match: Host(`grafana.localhost`)
      kind: Rule
      services:
        - name: kube-prometheus-stack-grafana
          port: 80
```

**Updated `Makefile`**:
- `setup-local` target: add `helmfile apply -f config/helmfile.yaml` after namespace creation
- Prerequisite check: `helmfile version` — fail clearly if helmfile not installed, print install link
- Add `make update-platform` target that runs `helmfile apply -f config/helmfile.yaml` for upgrades

**New file: `config/deploy/monitoring-namespace.yaml`** — ensures `monitoring` namespace exists before Helm install:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: monitoring
```

**Updated `api/v1alpha1/application_types.go`** — add `spec.metrics` field:
```go
type MetricsConfig struct {
    // +kubebuilder:default=false
    Enabled bool  `json:"enabled"`
    // Port defaults to spec.port if zero
    Port    int32 `json:"port,omitempty"`
    // +kubebuilder:default="/metrics"
    Path    string `json:"path,omitempty"`
}

type ApplicationSpec struct {
    // ... existing fields ...
    // +optional
    Metrics *MetricsConfig `json:"metrics,omitempty"`
}
```

**New file: `internal/k8s/servicemonitor.go`** — builds `ServiceMonitor` unstructured CR:
```go
var ServiceMonitorGVK = schema.GroupVersionKind{
    Group:   "monitoring.coreos.com",
    Version: "v1",
    Kind:    "ServiceMonitor",
}

func BuildServiceMonitor(app *iafv1alpha1.Application) *unstructured.Unstructured
```
- Selector: `matchLabels: {iaf.io/application: <name>}` (same labels the controller already sets on the Service)
- Endpoint: `port: "http"` (named port on the Service), `path: app.Spec.Metrics.Path`
- Owner reference: the `Application` CR → auto-deleted when app is deleted
- Label `release: kube-prometheus-stack` so Prometheus Operator picks it up

**Updated `internal/controller/application_controller.go`**:
- Add `reconcileServiceMonitor(ctx, app)` called after `reconcileService`
- If `app.Spec.Metrics != nil && app.Spec.Metrics.Enabled`: create/update `ServiceMonitor`
- If `app.Spec.Metrics == nil || !app.Spec.Metrics.Enabled`: delete `ServiceMonitor` if it exists (cleanup on disable)
- RBAC marker: `+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete`

**Updated `cmd/apiserver/main.go`**:
- Add `promhttp.Handler()` on a separate `IAF_METRICS_PORT` (default `9091`, not 9090 to avoid Prometheus port conflict)
- Start metrics server in a goroutine alongside the main Echo server
- Register custom metrics with `prometheus/client_golang` (see metrics list below)

**Updated `cmd/mcpserver/main.go`**:
- Same metrics server pattern on its own port (default `9092`)

**Updated `cmd/controller/main.go`**:
- controller-runtime already exposes `/metrics` via `ctrl.Manager`; ensure `BindMetricsAddress` is set (default `:8080` in ctrl — rename to `:9090`)
- Register custom IAF metrics on the controller-runtime registry

**New file: `internal/metrics/platform.go`** — central registry for platform metrics:
```go
var (
    SessionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "iaf_sessions_total",
        Help: "Total sessions created.",
    })
    SessionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "iaf_sessions_active",
        Help: "Currently active sessions.",
    })
    ApplicationsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "iaf_applications_by_phase",
        Help: "Application count by phase.",
    }, []string{"phase"})
    ReconcileDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "iaf_reconcile_duration_seconds",
        Help:    "Controller reconcile loop duration.",
        Buckets: prometheus.DefBuckets,
    })
    APIRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "iaf_api_request_duration_seconds",
        Help:    "REST API request duration.",
        Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
    }, []string{"method", "path", "status"})
    SourceStoreBytesTotal = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "iaf_source_store_bytes_total",
        Help: "Total bytes in the source store.",
    })
)

func Register(reg prometheus.Registerer) { ... }
```

**Updated `internal/middleware/metrics.go`** (new file) — Echo middleware that records `iaf_api_request_duration_seconds`

**New file: `config/grafana/dashboards/platform-health.json`** — Platform Health dashboard JSON:
- Panels: active sessions, apps by phase, API request rate, p99 latency, reconcile duration, source store bytes
- Provisioned via a ConfigMap with `grafana_dashboard: "1"` label (picked up by kube-prometheus-stack sidecar)

**New file: `config/grafana/dashboards/application-overview.json`** — Application Overview dashboard:
- Template variable: `namespace` (regex `iaf-.*`)
- Panels: pod CPU/memory (kube-state-metrics), replica count, HTTP request rate (when app metrics enabled)

**New file: `config/deploy/grafana-dashboards-configmap.yaml`** — wraps dashboards in ConfigMap:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: iaf-grafana-dashboards
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
```

**New file: `config/deploy/platform-servicemonitors.yaml`** — `ServiceMonitor` CRs for IAF platform components:
- One each for apiserver, mcpserver, controller, pointing to their metrics ports

**Test files**:
- `internal/k8s/servicemonitor_test.go` — `TestBuildServiceMonitor`: verify selector, labels, port, path
- `internal/controller/application_controller_test.go` — extend with `TestReconcile_MetricsEnabled`: confirm `ServiceMonitor` created; `TestReconcile_MetricsDisabled`: confirm `ServiceMonitor` absent
- `internal/metrics/platform_test.go` — `TestSessionsTotal`: counter increments on register

### Data / API Changes

**`ApplicationSpec`** — new optional field `metrics *MetricsConfig`.

**`ApplicationResponse`** (REST API) — no change needed (metrics config not surfaced in response yet; `metricsEndpoint` is a follow-on in issue #30).

### Multi-tenancy & Shared Resource Impact

- `ServiceMonitor` CRs are namespace-scoped to the agent's namespace. Prometheus Operator is configured with `serviceMonitorNamespaceSelector: {}` (all namespaces) — this is intentional; all agent apps report to the shared platform Prometheus.
- **Isolation**: Prometheus has read-only access to all namespaces for scraping, but agents cannot query Prometheus directly. Grafana is the only query interface, accessible to operators only.
- The Prometheus scrape permission (`ServiceMonitor` with matching labels) is namespace-scoped. An agent can enable/disable metrics for their own apps; they cannot create `ServiceMonitor` CRs targeting other namespaces (they don't have RBAC to create CRs in `monitoring`).

### Security Considerations

- **Grafana admin credentials**: default `iaf-dev-grafana` is for local dev only. `make setup-local` must print a warning: "Change the Grafana admin password before exposing to any network." Production should use `grafana.adminPassword` from a K8s Secret rather than a Helm value.
- **Prometheus `/metrics` exposure**: controller-runtime's `/metrics` exposes Go runtime stats and controller-runtime internals — no secrets exposed. Review before enabling external access.
- **`ServiceMonitor` label requirement**: the `release: kube-prometheus-stack` label on `ServiceMonitor` CRs is what allows Prometheus Operator to pick them up. If an agent could craft a CR with this label pointing to another app's port, they could cause scrape-rate increases but not access data. Acceptable risk.
- **Anonymous Grafana access**: `grafana.auth.anonymous.enabled` must be `false` (the default). Explicitly set in values file to prevent accidental override.
- **Needs security review**: flag `needs-security-review` label.

### Resource & Performance Impact

- kube-prometheus-stack adds ~4 pods to `monitoring` namespace; on a local Rancher Desktop cluster, set reduced resource requests in values (included above).
- `ServiceMonitor` per app: negligible — Prometheus Operator handles thousands of monitors. Each adds one scrape target to Prometheus.
- Custom metrics in `internal/metrics/platform.go`: all in-process; negligible CPU/memory.

### Migration / Compatibility

- `spec.metrics` is optional; existing `Application` CRs are unaffected.
- Helmfile is a new `make setup-local` prerequisite. Add to `README.md`/`docs/` with install instructions: `brew install helmfile` (macOS), or direct binary download.
- No CRD breaking changes (new field only).

### Open Questions for Developer

1. **`IAF_METRICS_PORT` default**: use `9091` for apiserver and `9092` for mcpserver to avoid conflict with Prometheus itself on `:9090`. Confirm with cluster network layout.
2. **Dashboard provisioning**: the kube-prometheus-stack sidecar watches for ConfigMaps with label `grafana_dashboard: "1"` in the `monitoring` namespace. Confirm label selector matches the chart version's sidecar config.
3. **`make run-helmfile` prerequisite check**: add a Makefile check that `helmfile` binary exists before calling it; print `brew install helmfile` if not found (macOS) or the GitHub releases URL.
4. **`ServiceMonitor` port reference**: the controller already creates a Service with a `containerPort` but does not name the port. Add `name: "http"` to the Service's port definition so the `ServiceMonitor` can reference it by name rather than number.
5. **controller-runtime metrics bind address**: the default is `:8080` which conflicts with the webhook server. Confirm the correct flag (`--metrics-bind-address`) and set to `:9090` in `config/deploy/platform.yaml`.
