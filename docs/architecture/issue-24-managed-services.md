# Architecture: Managed Services — PostgreSQL via CloudNativePG (Issue #24)

## Technical Design

### Approach

Introduce a new `ManagedService` CRD (namespace-scoped, same namespace as the session) with a dedicated reconciler that provisions CloudNativePG `Cluster` CRs for PostgreSQL. Add six MCP tools (`provision_service`, `service_status`, `bind_service`, `unbind_service`, `deprovision_service`, `list_services`) and a `services-guide` prompt. CloudNativePG operator is installed via a separate **Helmfile** (`config/helmfile-services.yaml`) so the observability stack (`config/helmfile.yaml`) can be deployed independently. Credentials are K8s Secrets scoped to the session namespace and never returned in tool responses.

### Changes Required

**New file: `config/helmfile-services.yaml`** — services-specific Helmfile:
```yaml
repositories:
  - name: cloudnative-pg
    url: https://cloudnative-pg.github.io/charts

releases:
  - name: cloudnative-pg
    namespace: cnpg-system
    chart: cloudnative-pg/cloudnative-pg
    version: "0.23.x"    # pin; update via helmfile apply -f config/helmfile-services.yaml
    values:
      - helm-values/cloudnative-pg.yaml
```

**New file: `config/helm-values/cloudnative-pg.yaml`**:
```yaml
# Minimal values for local development; operator runs with default settings
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    memory: 256Mi
```

**Updated `Makefile`**:
- `setup-local` target: add `helmfile apply -f config/helmfile-services.yaml` after the main helmfile
- Add `make update-services` target for CloudNativePG upgrades independent of the observability stack

**New file: `api/v1alpha1/managedservice_types.go`**:
```go
// +groupName=iaf.io

package v1alpha1

// ManagedServicePhase represents the lifecycle phase of a managed service.
type ManagedServicePhase string

const (
    ManagedServicePhaseProvisioning ManagedServicePhase = "Provisioning"
    ManagedServicePhaseReady        ManagedServicePhase = "Ready"
    ManagedServicePhaseFailed       ManagedServicePhase = "Failed"
    ManagedServicePhaseDeleting     ManagedServicePhase = "Deleting"
)

// ServicePlan represents a provisioning tier.
// +kubebuilder:validation:Enum=micro;small;ha
type ServicePlan string

const (
    ServicePlanMicro ServicePlan = "micro"
    ServicePlanSmall ServicePlan = "small"
    ServicePlanHA    ServicePlan = "ha"
)

type ManagedServiceSpec struct {
    // Type is the service kind. Currently only "postgres".
    // +kubebuilder:validation:Enum=postgres
    Type string `json:"type"`

    // Plan controls sizing: micro, small, or ha.
    // +kubebuilder:default="micro"
    Plan ServicePlan `json:"plan"`

    // Version is the service version (e.g., "17" for PostgreSQL 17).
    // +optional
    // +kubebuilder:default="17"
    Version string `json:"version,omitempty"`

    // Database is the name of the database to create.
    // +optional
    // +kubebuilder:default="app"
    Database string `json:"database,omitempty"`
}

type ManagedServiceStatus struct {
    // Phase is the current lifecycle phase.
    // +optional
    Phase ManagedServicePhase `json:"phase,omitempty"`

    // ConnectionSecretRef is the name of the Secret containing connection details.
    // Only set when Phase == Ready.
    // +optional
    ConnectionSecretRef string `json:"connectionSecretRef,omitempty"`

    // Message provides additional context, especially when Phase == Failed.
    // +optional
    Message string `json:"message,omitempty"`

    // Conditions are standard K8s conditions.
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // BoundApps is the list of Application names currently bound to this service.
    // Used by deprovision_service to block deletion when apps are bound.
    // +optional
    BoundApps []string `json:"boundApps,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Plan",type=string,JSONPath=`.spec.plan`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

type ManagedService struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   ManagedServiceSpec   `json:"spec,omitempty"`
    Status ManagedServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ManagedServiceList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []ManagedService `json:"items"`
}
```
Register in `SchemeBuilder.Register(&ManagedService{}, &ManagedServiceList{})`.
Run `make generate` after adding this file.

**New file: `internal/k8s/cnpg.go`** — builds CloudNativePG `Cluster` unstructured CR:
```go
var CNPGClusterGVK = schema.GroupVersionKind{
    Group:   "postgresql.cnpg.io",
    Version: "v1",
    Kind:    "Cluster",
}

// PlanConfig captures sizing for a ManagedService plan.
type PlanConfig struct {
    Instances int32
    CPU       string
    Memory    string
    StorageGB int
}

var planConfigs = map[iafv1alpha1.ServicePlan]PlanConfig{
    iafv1alpha1.ServicePlanMicro: {Instances: 1, CPU: "100m", Memory: "256Mi", StorageGB: 1},
    iafv1alpha1.ServicePlanSmall: {Instances: 1, CPU: "500m", Memory: "512Mi", StorageGB: 5},
    iafv1alpha1.ServicePlanHA:    {Instances: 3, CPU: "1",    Memory: "1Gi",   StorageGB: 10},
}

func BuildCNPGCluster(svc *iafv1alpha1.ManagedService) *unstructured.Unstructured
func GetCNPGClusterStatus(obj *unstructured.Unstructured) (phase string, secretName string)
```

`BuildCNPGCluster` constructs:
```json
{
  "apiVersion": "postgresql.cnpg.io/v1",
  "kind": "Cluster",
  "metadata": {
    "name": "<svc.Name>",
    "namespace": "<svc.Namespace>",
    "ownerReferences": [...]
  },
  "spec": {
    "instances": <plan.Instances>,
    "imageName": "ghcr.io/cloudnative-pg/postgresql:<svc.Spec.Version>",
    "bootstrap": {
      "initdb": {"database": "<svc.Spec.Database>", "owner": "app"}
    },
    "storage": {
      "size": "<plan.StorageGB>Gi"
    },
    "resources": {
      "requests": {"cpu": "<plan.CPU>", "memory": "<plan.Memory>"},
      "limits": {"memory": "<plan.Memory>"}
    }
  }
}
```

Owner reference: `ManagedService` → `Cluster` (so `Cluster` is deleted when `ManagedService` is deleted).

`GetCNPGClusterStatus` reads `status.conditions` from the CloudNativePG `Cluster` object:
- `Ready == True` → phase `"Ready"`, secretName from `status.secretsResourceVersion.superuserSecret` (or the application user secret — see open questions)
- Otherwise → phase `"Provisioning"` or `"Failed"`

**New file: `internal/controller/managedservice_controller.go`**:
```go
type ManagedServiceReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=iaf.io,resources=managedservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=iaf.io,resources=managedservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *ManagedServiceReconciler) Reconcile(ctx, req) (ctrl.Result, error)
```

Reconcile logic:
1. Fetch `ManagedService`; if not found → return nil
2. Check if `ManagedService` is being deleted (DeletionTimestamp set): if `BoundApps` is non-empty, set phase `Failed` with message "cannot delete: apps still bound: [...]" and return error (blocking deletion via a finalizer)
3. Otherwise: create or update the CloudNativePG `Cluster` CR
4. Read `Cluster` status via `GetCNPGClusterStatus`
5. Mirror status to `ManagedService.Status.Phase` and `ConnectionSecretRef`
6. Requeue every 10 seconds while `Provisioning`

**Finalizer**: `iaf.io/managed-service-protection`. Add on creation; remove when `BoundApps` is empty before deletion.

**New file: `internal/mcp/tools/services.go`** — six tools:

```go
// provision_service
type ProvisionServiceInput struct {
    SessionID string      `json:"session_id"`
    Name      string      `json:"name"`       // validated: K8s name rules
    Type      string      `json:"type"`       // "postgres"
    Plan      string      `json:"plan"`       // "micro"|"small"|"ha"
    Version   string      `json:"version,omitempty"`
    Database  string      `json:"database,omitempty"`
}
// Creates ManagedService CR; returns {name, type, plan, message: "provisioning started; poll service_status"}

// service_status
type ServiceStatusInput struct {
    SessionID string `json:"session_id"`
    Name      string `json:"name"`
}
// Returns {name, type, plan, phase, message}
// ONLY when phase == "Ready": also returns {connectionEnvVars: ["DATABASE_URL","PGHOST","PGPORT","PGDATABASE","PGUSER","PGPASSWORD"]}
// NEVER returns actual credential values

// bind_service
type BindServiceInput struct {
    SessionID   string `json:"session_id"`
    ServiceName string `json:"service_name"`
    AppName     string `json:"app_name"`
}
// 1. Validates service is Ready
// 2. Validates app exists in session namespace
// 3. Copies connection Secret to app's namespace (same namespace — all in session ns)
//    Actually: both service and app are in the same session namespace; no copy needed.
//    Instead: patch Application.Spec.Env with the 6 env var references pointing to the existing Secret.
// 4. Adds app name to ManagedService.Status.BoundApps
// 5. Returns {bound: true, injectedEnvVars: [...names...]}

// unbind_service
type UnbindServiceInput struct {
    SessionID   string `json:"session_id"`
    ServiceName string `json:"service_name"`
    AppName     string `json:"app_name"`
}
// Removes env var references from Application.Spec.Env (matching the service's secret keys)
// Removes app from ManagedService.Status.BoundApps
// Does NOT delete the Secret

// deprovision_service
type DeprovisionServiceInput struct {
    SessionID string `json:"session_id"`
    Name      string `json:"name"`
}
// Fails with clear error if ManagedService.Status.BoundApps is non-empty
// Otherwise: deletes ManagedService CR; controller finalizer ensures CNPG Cluster is deleted too

// list_services
type ListServicesInput struct {
    SessionID string `json:"session_id"`
}
// Returns [{name, type, plan, phase, boundApps}] for all ManagedService CRs in session namespace
```

**Env var injection via `bind_service`**:

Rather than copying the Secret (both service and app are in the same namespace), `bind_service` patches `Application.Spec.Env` with `EnvFrom` or individual `EnvVar` references. However, `Application.Spec.Env` currently uses `[]EnvVar{Name, Value}` with literal values. To support Secret-backed env vars, a new type is needed:

```go
type EnvVar struct {
    Name  string `json:"name"`
    Value string `json:"value,omitempty"`
    // +optional
    SecretKeyRef *SecretKeyRef `json:"secretKeyRef,omitempty"`
}

type SecretKeyRef struct {
    Name string `json:"name"`
    Key  string `json:"key"`
}
```

The controller already maps `ApplicationSpec.Env` to `corev1.EnvVar`; extend `reconcileDeployment` to handle `SecretKeyRef`.

CloudNativePG generates a Secret named `<cluster-name>-app` with keys:
- `username`, `password`, `host`, `port`, `dbname`, `uri` (contains the full `DATABASE_URL`)

`bind_service` adds these 6 env vars to `Application.Spec.Env`:
```go
envVars := []iafv1alpha1.EnvVar{
    {Name: "DATABASE_URL", SecretKeyRef: &SecretKeyRef{Name: secretName, Key: "uri"}},
    {Name: "PGHOST",       SecretKeyRef: &SecretKeyRef{Name: secretName, Key: "host"}},
    {Name: "PGPORT",       SecretKeyRef: &SecretKeyRef{Name: secretName, Key: "port"}},
    {Name: "PGDATABASE",   SecretKeyRef: &SecretKeyRef{Name: secretName, Key: "dbname"}},
    {Name: "PGUSER",       SecretKeyRef: &SecretKeyRef{Name: secretName, Key: "username"}},
    {Name: "PGPASSWORD",   SecretKeyRef: &SecretKeyRef{Name: secretName, Key: "password"}},
}
```

Collision check: if `Application.Spec.Env` already contains any of these 6 names, return an error listing the conflicting names. Do not silently overwrite.

**New file: `internal/mcp/prompts/services_guide.go`**:
- Prompt: `services-guide`; no language argument needed
- Content: complete workflow walkthrough:
  1. `provision_service` — start provisioning
  2. `service_status` — poll until `phase == "Ready"` (include polling interval recommendation: 10s)
  3. `bind_service` — inject credentials into app
  4. App reads `DATABASE_URL` from env (standard for all supported languages)
  5. `unbind_service` — remove credentials before deprovisioning
  6. `deprovision_service` — clean up
- Includes example env var names and explicit statement: "credential values are injected as K8s Secret references — never logged or returned by tools"
- Cross-references `deploy-guide` for deploying the app after binding

**Updated `internal/mcp/resources/platform_info.go`**:
- Add `managedServices` section:
  ```json
  "managedServices": {
    "available": ["postgres"],
    "plans": {
      "micro":  {"instances": 1, "storage": "1Gi",  "memory": "256Mi"},
      "small":  {"instances": 1, "storage": "5Gi",  "memory": "512Mi"},
      "ha":     {"instances": 3, "storage": "10Gi", "memory": "1Gi"}
    },
    "versions": {"postgres": ["16", "17"]}
  }
  ```

**Updated `internal/mcp/server.go`**: register all service tools and the `services-guide` prompt.

**Test files**:

`internal/k8s/cnpg_test.go`:
- `TestBuildCNPGCluster_Micro`: verify instances=1, storage=1Gi
- `TestBuildCNPGCluster_HA`: verify instances=3
- `TestGetCNPGClusterStatus_Ready`: parse canned unstructured status object

`internal/mcp/tools/services_test.go`:
- `TestProvisionService_OK`: creates `ManagedService` CR
- `TestProvisionService_InvalidType`: returns error for unknown `type`
- `TestServiceStatus_Provisioning`: returns `phase: Provisioning`, no connection vars
- `TestServiceStatus_Ready`: returns `phase: Ready` with env var name list (no values)
- `TestBindService_OK`: patches `Application.Spec.Env` with 6 SecretKeyRef entries
- `TestBindService_EnvConflict`: returns error when app already has `DATABASE_URL`
- `TestDeprovisionService_Blocked`: returns error when `BoundApps` is non-empty
- `TestDeprovisionService_OK`: deletes CR when `BoundApps` is empty

`internal/controller/managedservice_controller_test.go`:
- `TestManagedServiceReconcile_CreatesCluster`: creates CNPG `Cluster` CR on first reconcile
- `TestManagedServiceReconcile_StatusMirrored`: `Cluster` Ready → `ManagedService` phase Ready
- `TestManagedServiceReconcile_DeletionBlocked`: deletion refused when `BoundApps` non-empty
- `TestManagedServiceReconcile_DeletionAllowed`: finalizer removed when `BoundApps` empty

### Data / API Changes

**New CRD**: `ManagedService` (group `iaf.io/v1alpha1`, namespace-scoped).
**Updated `EnvVar`**: new optional `SecretKeyRef` field — backward compatible (omitempty).
**`iaf://platform` resource**: new `managedServices` section.
**New MCP tools**: 6 tools.
**New MCP prompt**: `services-guide`.

### Multi-tenancy & Shared Resource Impact

- `ManagedService` and `Cluster` CRs are in the session namespace — full isolation. No cross-namespace access.
- CloudNativePG operator runs in `cnpg-system` and watches `Cluster` CRs in all namespaces. This is the operator's standard behavior; it does not grant cross-namespace access to agents.
- `bind_service` references a Secret by name in the same namespace — no cross-namespace Secret copy needed. The `SecretKeyRef` in the pod spec references the same namespace's Secret.
- `BoundApps` list on `ManagedService.Status`: an agent can only modify `ManagedService` CRs in their own namespace (enforced by `deps.ResolveNamespace`). No cross-namespace `BoundApps` injection possible.
- CloudNativePG is a **shared** operator — all sessions use the same operator installation. The operator itself is not a multi-tenancy risk since it only acts on `Cluster` CRs in each namespace and writes Secrets to those same namespaces.

### Security Considerations

**Highest-risk feature. Needs security review.**

- **Credential never in tool response**: `service_status` returns only the env var names, never values. `bind_service` patches the Application with `SecretKeyRef` — the actual Secret value is read by the kubelet, never by the MCP tools. This is a hard design invariant — review all code paths in `services.go` to confirm no credential values appear in return values or logs.
- **Secret name validation**: `ConnectionSecretRef` is set from `GetCNPGClusterStatus` which reads the CloudNativePG-generated secret name from the `Cluster` status. Validate that this name belongs to the session namespace before referencing it in `Application.Spec.Env` — prevent a theoretical path where a malformed CNPG status could inject a Secret from another namespace.
- **Deletion guard**: `deprovision_service` must check `BoundApps` BEFORE deleting. Losing the database out from under a running app corrupts all bound apps. The finalizer adds a server-side guard independent of the MCP tool check.
- **Plan resource limits**: `micro` plan (256Mi memory, 1Gi storage) must not be used for production. Document that `ha` plan is required for production workloads. Platform operators should enforce storage quotas per namespace (issue #7) to prevent agents from over-provisioning.
- **`ha` plan backup**: WAL archiving is not configured in this design. Document clearly: `ha` plan provides HA via streaming replication (surviving node failures) but does NOT provide point-in-time recovery. Backup is out of scope; operators must configure it separately.
- **RBAC for controller**: the `ManagedServiceReconciler` needs `get/list/watch/create/update/patch/delete` on `postgresql.cnpg.io/clusters`. Add RBAC markers. Also needs `get/list/watch` on Secrets (to read CloudNativePG-generated connection Secrets). Use `get`/`list` only — the controller does not need to write Secrets (CloudNativePG writes them).
- **App name in env var collision check**: `bind_service` checks for name collisions in `Application.Spec.Env`. The check must be case-sensitive (env var names are case-sensitive on Linux). Check for exact match — `DATABASE_URL` and `database_url` are different.
- **CloudNativePG operator privilege**: CloudNativePG operator runs with cluster-wide permissions on `Cluster` CRs. Review its RBAC before `make setup-local`. Do not grant it `secrets *` — it needs only to read/write Secrets in namespaces where it creates Clusters.

### Resource & Performance Impact

- CloudNativePG operator (`cnpg-system`): ~128Mi memory.
- `micro` plan: 1 CNPG pod + 1Gi PVC per provisioned service — agents must be aware of cluster storage capacity.
- `ha` plan: 3 CNPG pods + 3× 10Gi PVC — significant; require explicit plan choice.
- Finalizer on `ManagedService`: blocks garbage collection until `BoundApps` is empty. This is intentional — it is a safety mechanism, not a bug.

### Migration / Compatibility

- New CRD — `make generate` required after adding `managedservice_types.go`.
- `EnvVar.SecretKeyRef` is additive (omitempty) — existing `Application` CRs unaffected.
- CloudNativePG is a new `make setup-local` prerequisite — install instructions in README.
- Helmfile install order: `config/helmfile.yaml` (observability) runs before `config/helmfile-services.yaml` (services). They are independent; either can fail without affecting the other.

### Open Questions for Developer

1. **CloudNativePG secret key names**: CloudNativePG generates a Secret named `<cluster-name>-app` for the application user. Confirm the exact key names (`username`, `password`, `host`, `port`, `dbname`, `uri`) by testing against CloudNativePG 0.23. The key names have changed across CNPG versions.
2. **`SecretKeyRef` in `Application.Spec.Env`**: this is a breaking addition to the `EnvVar` type schema. Run `make generate` and `make install-crds` to update the CRD after adding the field. Confirm the controller's `reconcileDeployment` handles `SecretKeyRef` by mapping to `corev1.EnvVar.ValueFrom.SecretKeyRef`.
3. **`BoundApps` concurrency**: `bind_service` and `unbind_service` modify `ManagedService.Status.BoundApps` via a status update. Use optimistic concurrency (retry on conflict) — standard controller-runtime pattern.
4. **Finalizer placement**: add the finalizer in `Reconcile` on first creation (when `DeletionTimestamp` is nil and finalizer not yet set). Remove in `Reconcile` when `DeletionTimestamp` is set and `BoundApps` is empty. Do not add the finalizer in the MCP tool handler — only the controller manages finalizers.
5. **CNPG availability check**: before allowing `provision_service`, verify the `postgresql.cnpg.io/v1/Cluster` CRD exists in the cluster (check via `k8sClient.RESTMapper().ResourceFor(...)`). Return a clear error if not found: `"managed services are not available on this platform instance"`. This implements graceful degradation (issue open question #4).
6. **`services-guide` prompt + `deploy-guide` cross-reference**: ensure `deploy-guide` mentions managed services in its "persistent data" section: "for databases, use `provision_service` — do not deploy a database as an Application".
