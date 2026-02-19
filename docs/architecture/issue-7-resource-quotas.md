# Technical Design: Resource Quotas and LimitRange per Agent Namespace (#7)

**Also carries `needs-security-review`.**

---

## Approach

Extend `EnsureNamespace` to create a `ResourceQuota` and a `LimitRange` in the new
namespace immediately after namespace creation — within the same function, before
returning. This guarantees no window exists where an agent can create resources before
the quota is in place. All limit values are configurable via env vars with documented
defaults.

---

## Changes Required

### `internal/config/config.go`

Add quota configuration fields:

```go
// Namespace quotas
QuotaMaxApps    int    `mapstructure:"quota_max_apps"`    // max Application CRs
QuotaMaxCPU     string `mapstructure:"quota_max_cpu"`     // total CPU requests, e.g. "4"
QuotaMaxMemory  string `mapstructure:"quota_max_memory"`  // total memory requests, e.g. "4Gi"
QuotaMaxStorage string `mapstructure:"quota_max_storage"` // total PVC storage, e.g. "10Gi"
```

Defaults:
```go
v.SetDefault("quota_max_apps", 10)
v.SetDefault("quota_max_cpu", "4")
v.SetDefault("quota_max_memory", "4Gi")
v.SetDefault("quota_max_storage", "10Gi")
```

Env vars: `IAF_QUOTA_MAX_APPS`, `IAF_QUOTA_MAX_CPU`, `IAF_QUOTA_MAX_MEMORY`,
`IAF_QUOTA_MAX_STORAGE`.

CPU and memory are stored as strings because they use Kubernetes resource quantity
notation (`"4"`, `"4Gi"`) which `resource.MustParse` handles.

### `internal/auth/namespace.go`

`EnsureNamespace` currently creates the namespace and service account. Extend it to
also create `ResourceQuota` and `LimitRange` after the namespace is confirmed to exist.

**New signature with quota config:**

```go
type NamespaceConfig struct {
    QuotaMaxApps    int
    QuotaMaxCPU     string
    QuotaMaxMemory  string
    QuotaMaxStorage string
}

func EnsureNamespace(ctx context.Context, c client.Client, namespace string, cfg NamespaceConfig) error
```

All existing callers (the `register` MCP tool in `register.go` and any tests) must be
updated to pass a `NamespaceConfig`. Add `NamespaceConfig` to `Dependencies` so all MCP
tools can pass it through.

**ResourceQuota creation (inside `EnsureNamespace`):**

```go
quota := &corev1.ResourceQuota{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "iaf-quota",
        Namespace: namespace,
        Labels:    map[string]string{"app.kubernetes.io/managed-by": "iaf"},
    },
    Spec: corev1.ResourceQuotaSpec{
        Hard: corev1.ResourceList{
            corev1.ResourceCPU:            resource.MustParse(cfg.QuotaMaxCPU),
            corev1.ResourceMemory:         resource.MustParse(cfg.QuotaMaxMemory),
            corev1.ResourceRequestsStorage: resource.MustParse(cfg.QuotaMaxStorage),
            // Application CR count via object count quota
            corev1.ResourceName("count/applications.iaf.io"): resource.MustParse(
                fmt.Sprintf("%d", cfg.QuotaMaxApps),
            ),
        },
    },
}
if err := c.Create(ctx, quota); err != nil && !apierrors.IsAlreadyExists(err) {
    return fmt.Errorf("creating resource quota in %q: %w", namespace, err)
}
```

**LimitRange creation (inside `EnsureNamespace`):**

```go
lr := &corev1.LimitRange{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "iaf-limits",
        Namespace: namespace,
        Labels:    map[string]string{"app.kubernetes.io/managed-by": "iaf"},
    },
    Spec: corev1.LimitRangeSpec{
        Limits: []corev1.LimitRangeItem{
            {
                Type: corev1.LimitTypeContainer,
                Default: corev1.ResourceList{
                    corev1.ResourceCPU:    resource.MustParse("500m"),
                    corev1.ResourceMemory: resource.MustParse("512Mi"),
                },
                DefaultRequest: corev1.ResourceList{
                    corev1.ResourceCPU:    resource.MustParse("100m"),
                    corev1.ResourceMemory: resource.MustParse("128Mi"),
                },
            },
        },
    },
}
if err := c.Create(ctx, lr); err != nil && !apierrors.IsAlreadyExists(err) {
    return fmt.Errorf("creating limit range in %q: %w", namespace, err)
}
```

Add imports: `corev1 "k8s.io/api/core/v1"`, `"k8s.io/apimachinery/pkg/api/resource"`.

### `internal/mcp/tools/deps.go`

Add `NSConfig auth.NamespaceConfig` to `Dependencies`:

```go
type Dependencies struct {
    Client     client.Client
    Store      *sourcestore.Store
    BaseDomain string
    Sessions   *auth.SessionStore
    SessionTTL time.Duration
    NSConfig   auth.NamespaceConfig
}
```

### `internal/mcp/tools/register.go`

Pass `deps.NSConfig` to `auth.EnsureNamespace`:

```go
if err := auth.EnsureNamespace(ctx, deps.Client, sess.Namespace, deps.NSConfig); err != nil {
    return nil, nil, fmt.Errorf("creating namespace: %w", err)
}
```

### `internal/mcp/server.go`

Populate `NSConfig` in `NewServer` from the config passed in. Update `NewServer` to
accept a `NamespaceConfig` parameter or extract it from the existing config pattern.
Currently `NewServer` takes individual params — add `nsConfig auth.NamespaceConfig`:

```go
func NewServer(
    k8sClient client.Client,
    sessions *auth.SessionStore,
    store *sourcestore.Store,
    baseDomain string,
    nsConfig auth.NamespaceConfig,
    clientset ...kubernetes.Interface,
) *gomcp.Server
```

### `cmd/apiserver/main.go` and `cmd/mcpserver/main.go`

Pass the quota config when constructing the MCP server:

```go
nsConfig := auth.NamespaceConfig{
    QuotaMaxApps:    cfg.QuotaMaxApps,
    QuotaMaxCPU:     cfg.QuotaMaxCPU,
    QuotaMaxMemory:  cfg.QuotaMaxMemory,
    QuotaMaxStorage: cfg.QuotaMaxStorage,
}
server := iafmcp.NewServer(k8sClient, sessions, store, cfg.BaseDomain, nsConfig, clientset)
```

### RBAC

Add RBAC markers to the controller (which owns the service account used by both
controller and MCP servers):

```
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=create;get;list;watch
// +kubebuilder:rbac:groups="",resources=limitranges,verbs=create;get;list;watch
```

Run `make generate` to regenerate RBAC YAML.

**Note**: `update` and `patch` verbs are intentionally omitted. The quota and limit range
are platform-managed and should not be modified after creation (that would require a
re-`EnsureNamespace` call). If quota values change (via env var update), new namespaces
get the new values; existing namespaces are not retroactively updated. This is acceptable
for MVP.

### `internal/mcp/resources/` — `iaf://platform` resource

Update the platform-info resource to document the default quota limits so agents can
plan accordingly:

```json
{
  "quotas": {
    "maxApps": 10,
    "maxCPU": "4 cores",
    "maxMemory": "4Gi",
    "maxStorage": "10Gi"
  },
  "limits": {
    "defaultCPURequest": "100m",
    "defaultCPULimit": "500m",
    "defaultMemoryRequest": "128Mi",
    "defaultMemoryLimit": "512Mi"
  }
}
```

---

## Data / API Changes

No CRD changes. No MCP tool schema changes. The quota values are surfaced in the
`iaf://platform` resource.

When an agent exceeds the app quota, `deploy_app` or `push_code` will receive a
Kubernetes API error: `"exceeded quota: count/applications.iaf.io=10, requested: 1,
used: 10, limited: 10"`. This is moderately machine-readable but could be cleaner.
See "Open Questions for Developer" below.

---

## Multi-tenancy & Shared Resource Impact

`ResourceQuota` is scoped to the namespace — it limits only that tenant. The `LimitRange`
ensures containers without explicit resource requests get sensible defaults, preventing
runaway resource consumption from apps that don't specify limits.

The quota is applied before `EnsureNamespace` returns, and `EnsureNamespace` is called
before the `session_id` is returned to the agent. There is no window in which the agent
can call `deploy_app` before the quota is in place.

---

## Security Considerations

**Atomicity**: The Kubernetes API does not provide multi-resource transactions. Quota
creation happens after namespace creation, in three sequential API calls (namespace, SA,
quota, limitrange). If any call fails after the namespace is created, the namespace
exists without a quota. `EnsureNamespace` returns an error in this case, the `register`
tool returns an error to the agent, and the session is rolled back (or the agent retries).

On retry, `EnsureNamespace` is idempotent (`IsAlreadyExists` is ignored for all four
resources). A second call succeeds and applies the missing quota/limitrange.

**Window before quota**: Between namespace creation and quota creation, a concurrent
process could create resources in the namespace. In practice this is not possible because
the agent doesn't have the session namespace name until `register` returns — and
`register` only returns after `EnsureNamespace` completes. No window exists from the
agent's perspective.

**Quota bypass**: The `count/applications.iaf.io` quota is enforced by the Kubernetes
API admission controller. An agent cannot bypass it by manipulating the CRD directly
without API access. The `push_code` tool checks `CheckAppNameAvailable` before creating
the Application CR — the quota error from the K8s API is a second enforcement layer.

**`needs-security-review` items:**
1. Confirm that `count/applications.iaf.io` is a valid `ResourceQuota` hard limit in
   the cluster's version of Kubernetes (it requires the `application.iaf.io` CRD to be
   installed — which it is).
2. The `LimitRange` defaults apply to all containers in the namespace, including kpack
   build pods and IAF-managed app pods. Confirm that kpack build containers can function
   within the `500m` CPU limit and `512Mi` memory limit, or adjust the limits
   accordingly.
3. Consider whether the LimitRange `maxLimit` (maximum a container can request) should
   also be set to prevent a single container from claiming all quota CPU/memory. E.g.,
   `max: {cpu: "2", memory: "2Gi"}`. This is defense-in-depth.

---

## Resource & Performance Impact

Three additional K8s API calls at namespace creation time (quota + limitrange, plus
the existing namespace + SA = 4 total). All are fast write operations. No impact on
steady-state reconciliation.

---

## Migration / Compatibility

Existing namespaces do not get quotas retroactively — only newly registered sessions
get them. If an operator wants to backfill quotas on existing namespaces, they can apply
them manually via `kubectl`. Document this in the deployment guide.

---

## Open Questions (resolved)

**Q1 — ResourceQuota applied atomically with namespace?** No (Kubernetes doesn't support
this). Sequential creates with idempotent retry on failure. Acceptable because the agent
cannot act before `register` returns.

**Q2 — Quota via `ResourceQuota` or MCP tool check?** Both: `ResourceQuota` is the
authoritative enforcement (K8s admission); the MCP tool check (`CheckAppNameAvailable`)
is complementary. The K8s quota error should be caught and reformatted into a clean
error message.

---

## Open Questions for Developer

1. When the Application count quota is exceeded, the K8s API error from `client.Create`
   is a raw quota-exceeded message. Consider adding a `QuotaExceeded` detection in
   `deploy_app` and `push_code`: if `apierrors.IsForbidden(err)` and the error message
   contains `"exceeded quota"`, return a clean message: `"application quota exceeded:
   your session is limited to N apps. Use delete_app to remove an existing app first."`.

2. Verify kpack build pod resource requirements against the 500m CPU / 512Mi memory
   default limits from the `LimitRange`. If kpack build pods exceed these, builds will
   be OOM-killed or throttled. Consider setting a higher LimitRange `default` for
   build namespaces, or making the LimitRange defaults configurable via env vars.

3. The `resource.MustParse` calls in `EnsureNamespace` will panic if the config values
   are invalid quantity strings. Add validation of `QuotaMaxCPU`, `QuotaMaxMemory`, and
   `QuotaMaxStorage` in `config.Load()` using `resource.ParseQuantity` with error
   propagation rather than `MustParse`.
