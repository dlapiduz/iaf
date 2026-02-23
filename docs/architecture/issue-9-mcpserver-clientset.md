# Technical Design: Standalone mcpserver Log Streaming (#9)

## Approach

The bug is a single-line omission: `cmd/mcpserver/main.go` calls `iafmcp.NewServer`
without a clientset, so `NewServer` falls back to the degraded `RegisterAppLogs`. The
`internal/mcp/server.go` variadic API already handles both paths correctly. No changes
to the server wiring are needed — only the `cmd/mcpserver` entrypoint and a test.

---

## Changes Required

### `cmd/mcpserver/main.go`

After the existing `k8s.NewClient(cfg.KubeConfig)` call (which already fetches the
REST config internally), fetch the REST config a second time and build a
`kubernetes.Clientset`. Handle failure as a soft degradation — not a fatal error — so
the MCP server still starts in kubeconfig-less environments (e.g., a developer machine
with no `~/.kube/config` and no `KUBECONFIG` set).

```go
// Attempt to create a kubernetes clientset for log streaming.
// This is non-fatal: if kubeconfig is missing or invalid, the server
// starts in degraded mode and app_logs returns a redirect message.
var clientset kubernetes.Interface
restCfg, err := k8s.GetConfig(cfg.KubeConfig)
if err != nil {
    logger.Warn("log streaming: degraded (could not get REST config)", "error", err)
} else {
    cs, err := kubernetes.NewForConfig(restCfg)
    if err != nil {
        logger.Warn("log streaming: degraded (could not create clientset)", "error", err)
    } else {
        clientset = cs
        logger.Info("log streaming: enabled")
    }
}

server := iafmcp.NewServer(k8sClient, sessions, store, cfg.BaseDomain, clientset)
```

Note: `clientset` is typed as `kubernetes.Interface` (not `*kubernetes.Clientset`) so
it can be passed as the variadic `kubernetes.Interface` parameter. Passing a non-nil
interface that wraps a nil pointer would not work correctly; the approach above keeps
`clientset` as `nil` on failure, which the `NewServer` variadic check handles: `if
len(clientset) > 0 && clientset[0] != nil`.

Add import: `"k8s.io/client-go/kubernetes"`.

### No changes to `internal/mcp/server.go`

The variadic `clientset ...kubernetes.Interface` parameter already selects between
`RegisterAppLogsWithClientset` and `RegisterAppLogs`. This is correct and requires no
modification.

### New tests: `internal/mcp/server_test.go`

Add two tests using the existing `setupIntegrationServer` helper as a model. These tests
verify the observable difference between degraded and enabled modes by calling `app_logs`
and inspecting the response text.

**Test structure (both tests share setup steps 1-3):**
1. Create a fake `k8sClient` with `iafv1alpha1` + `corev1` schemes (same as existing
   helper)
2. Create a source store, session store, and call `NewServer` (with or without
   clientset)
3. Connect via in-memory transport, get a `ClientSession`
4. Call `register` → capture `session_id`
5. Pre-create a fake `Application` CR in the session namespace using the fake K8s
   client directly (so `app_logs` can find it without needing a running controller)
6. Call `app_logs` with `session_id` and the app name
7. Assert on the response text

**`TestAppLogs_DegradedWithoutClientset`**
- Call `NewServer(k8sClient, sessions, store, "test.example.com")` — no clientset
- After registering, create a fake Application CR in the session namespace
- Call `app_logs` with valid session_id + app name
- Assert response text contains `"requires a kubernetes clientset"`

**`TestAppLogs_EnabledWithClientset`**
- Create `fakeClientset := k8sfake.NewSimpleClientset()` (from
  `k8s.io/client-go/kubernetes/fake`)
- Call `NewServer(k8sClient, sessions, store, "test.example.com", fakeClientset)`
- After registering, create a fake Application CR in the session namespace
- Call `app_logs`
- Assert response text does NOT contain `"requires a kubernetes clientset"`
- Assert response text contains `"No pods found"` (since no pods exist in
  `fakeClientset`)

The second assertion (`"No pods found"`) confirms that `RegisterAppLogsWithClientset`
was registered, because the degraded handler never reaches the pod listing code.

---

## Data / API Changes

None. The `app_logs` tool schema is unchanged. The only difference is runtime
behaviour: real log content (or `"No pods found"`) instead of the redirect message.

---

## Multi-tenancy & Shared Resource Impact

No shared resource impact. The clientset reads pods only within the session namespace
(the namespace is resolved from the session_id before the pod list call). This is
already scoped correctly in `RegisterAppLogsWithClientset` via `client.InNamespace(namespace)`.

---

## Security Considerations

**RBAC scope**: `RegisterAppLogsWithClientset` calls:
- `deps.Client.List(pods, InNamespace(namespace))` — uses the controller-runtime client
  (already has `pods: get;list` from the existing RBAC marker)
- `clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, opts).Stream(ctx)` — uses the
  `kubernetes.Clientset` and requires `pods/log: get`

The RBAC marker `+kubebuilder:rbac:groups="",resources=pods/log,verbs=get` is already
present on the `ApplicationReconciler`. If the standalone mcpserver runs under the same
service account as the controller (current deployment setup), no new RBAC is needed.

If the mcpserver runs under a separate service account (a future deployment topology),
the RBAC marker would need to be duplicated or the annotations placed on a shared role.
For now: document that mcpserver requires `pods: get;list` and `pods/log: get` on the
namespaces it manages — these are already granted by the current RBAC.

The clientset is built from the same REST config as the controller-runtime client, so it
inherits the same credentials and permissions. No privilege escalation.

---

## Resource & Performance Impact

Negligible. One additional REST config fetch at startup.

---

## Migration / Compatibility

The standalone mcpserver previously returned a redirect message for all `app_logs` calls
regardless of input. After this fix, it returns real pod logs (or `"No pods found"`) for
apps with running pods. This is strictly an improvement. No API contract is broken.

---

## Open Questions (resolved)

**Q1 — In-cluster or local kubeconfig?** Both are supported. `k8s.GetConfig` already
tries in-cluster first, then falls back to `~/.kube/config` (and `KUBECONFIG`). The
mcpserver will work correctly in both deployment modes without any conditional logic.

**Q2 — Fatal or soft degradation?** Soft degradation. The mcpserver can serve all other
tools (deploy, status, list, etc.) even without log streaming. A fatal exit would
penalise environments where kubeconfig is not set up yet. The degraded `app_logs` message
already tells agents to use the REST API endpoint, which is a workable fallback.

---

## Open Questions for Developer

1. After this change, the startup log will show either `"log streaming: enabled"` or
   `"log streaming: degraded"`. Should the degraded message include the specific error
   string (e.g., `"no such file or directory"` for missing kubeconfig)? The design above
   includes `"error", err` in the slog call — confirm this is acceptable for the log
   format (it is, since this is a startup-time warning and the error is informational).

2. The `TestAppLogs_EnabledWithClientset` test creates a fake Application CR directly
   via the fake client after calling `register`. The session namespace is of the form
   `iaf-<id>`. Confirm that the test can pre-create the Application CR in the correct
   namespace by reading the `session_id` from the `register` tool response and
   extracting the namespace from the session store, or by pre-creating the Application
   with a known name in the namespace returned by `register`.
