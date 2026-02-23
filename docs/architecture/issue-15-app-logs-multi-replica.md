# Architecture: app_logs Multi-Replica Support and Consistent Pod Selection (Issue #15)

## Technical Design

### Approach

Fix pod selection in both the MCP tool (`RegisterAppLogsWithClientset`) and the REST API handler (`handlers/logs.go`) to consistently use the **most recently started pod** (sort by `CreationTimestamp` descending). Add an optional `pod_name` parameter that, when provided, fetches logs from a specific pod after validating it belongs to the app via label selector (preventing cross-app log access). Return `availablePods` in the response so agents can make targeted follow-up calls. Both the MCP tool and REST API handler are updated together since they share the same bug pattern.

### Changes Required

**`internal/mcp/tools/logs.go`**:

1. Update `AppLogsInput`:
   ```go
   type AppLogsInput struct {
       SessionID string `json:"session_id" ...`
       Name      string `json:"name" ...`
       Lines     int64  `json:"lines,omitempty" ...`
       BuildLogs bool   `json:"build_logs,omitempty" ...`
       PodName   string `json:"pod_name,omitempty" jsonschema:"optional - specific pod name to get logs from; if omitted, uses most recently started pod"`
   }
   ```

2. Add helper `selectPod(pods []corev1.Pod, podName string, labelKey string) (*corev1.Pod, []string, error)`:
   - `allPodNames`: collects all pod names from the pod list (capped at 10 — see open questions)
   - If `podName` is provided:
     - Scan the pod list for a pod with `Name == podName` **and** the expected label (`labelKey: appName`) present — validates the pod belongs to this app; if not found, return `fmt.Errorf("pod %q not found for application %q", podName, appName)`
   - If `podName` is not provided:
     - Sort pods by `CreationTimestamp` descending (`sort.Slice` on `pod.CreationTimestamp.Time`)
     - Return `pods[0]`

3. In `RegisterAppLogsWithClientset` handler:
   - Replace `pod := podList.Items[len(podList.Items)-1]` with call to `selectPod`
   - Pass `labelKey` and `input.Name` for the namespace isolation check
   - Add `availablePods` to result map: list of pod names (all pods matching the selector, capped at 10)
   - Build logs path already uses last item — change to use `selectPod` with sort-by-creation-desc (same behaviour, now consistent and documented)

4. Update `RegisterAppLogs` (degraded, no clientset):
   - Add `PodName` to input struct (accepted but ignored)
   - Response message can mention `pod_name` is not supported without a clientset

**`internal/api/handlers/logs.go`**:

1. `GetLogs` — replace `pod := podList.Items[0]` with most-recently-started-pod selection:
   ```go
   // Sort descending by creation timestamp
   sort.Slice(podList.Items, func(i, j int) bool {
       return podList.Items[i].CreationTimestamp.After(podList.Items[j].CreationTimestamp.Time)
   })
   pod := podList.Items[0]
   ```
   - Add `pod_name` query parameter: `podName := c.QueryParam("pod_name")`
   - If `podName != ""`, validate it belongs to the app using the same label check as the MCP tool
   - Return `availablePods` in response: `[]string{pod.Name, ...}` (all pod names, capped at 10)

2. `GetBuildLogs` — already uses `Items[len-1]`. Change to sort-by-creation-desc for consistency. Document this in comments.

**New `internal/k8s/pods.go`** (extract shared logic):
- `SelectMostRecentPod(pods []corev1.Pod) *corev1.Pod` — sort + return first
- `PodNames(pods []corev1.Pod, limit int) []string` — collect names up to limit
- `FindPodByName(pods []corev1.Pod, name, labelKey, labelValue string) (*corev1.Pod, error)` — scan + validate label

This avoids duplicating the same selection logic between the MCP tool and REST handler.

**Updated test file `internal/mcp/tools/logs_test.go`** (new or updated):
- `TestAppLogs_SinglePod`: existing behaviour unchanged
- `TestAppLogs_MultiPod_DefaultSelection`: returns most recently started pod (verify via `podName` in response)
- `TestAppLogs_MultiPod_SpecificPod`: `pod_name` returns that pod's logs
- `TestAppLogs_InvalidPodName`: pod not in app's label set → error
- `TestAppLogs_AvailablePods`: response includes `availablePods` list

**Updated test file `internal/api/handlers/logs_test.go`** (new):
- Same scenarios as MCP tool tests but via HTTP

### Data / API Changes

**`AppLogsInput`** — new optional field `pod_name`.

**MCP tool response** — new fields:
```json
{
  "name": "myapp",
  "logs": "...",
  "podName": "myapp-7d9b4c-xkqj2",
  "availablePods": ["myapp-7d9b4c-xkqj2", "myapp-7d9b4c-bpfq8"],
  "phase": "Running"
}
```

**REST API response** (`GET /api/v1/applications/:name/logs`):
```json
{
  "logs": "...",
  "pods": 2,
  "podName": "myapp-7d9b4c-xkqj2",
  "availablePods": ["myapp-7d9b4c-xkqj2", "myapp-7d9b4c-bpfq8"]
}
```

### Multi-tenancy & Shared Resource Impact

The `pod_name` validation (label check) is the critical isolation control:
- Agent requests logs from `pod_name: "other-agent-pod-xyz"` → not found in this app's pod list → returns error. No cross-app or cross-namespace log access.
- The validation must use `client.InNamespace(namespace)` + `client.MatchingLabels` — never skip the namespace filter.

### Security Considerations

- **`pod_name` injection**: validate against the pod list derived from the app's label selector, not via a direct `clientset.CoreV1().Pods(namespace).GetLogs(podName, ...)` call without validation. The validation step is mandatory.
- **Log content**: log content is user-app-generated; it may contain sensitive data. The platform does not filter log content — that is the agent/operator's responsibility. Document this in the prompt.
- No new RBAC changes needed (existing `pods/log get` permission covers any pod in the namespace).

### Resource & Performance Impact

- `sort.Slice` on O(replicas) pods: negligible.
- `availablePods` adds a small JSON field. Cap at 10 to bound response size.

### Migration / Compatibility

- `pod_name` is optional — existing callers unaffected.
- Pod selection change (from `Items[0]` → most-recent) is a **behaviour change**. For single-replica apps, behaviour is identical (one pod). For multi-replica apps, the returned pod may differ from before. This is the intended fix.
- No CRD changes, no env var changes.

### Open Questions for Developer

1. **`availablePods` cap**: 10 seems reasonable. Should it be configurable? No — hardcode 10 for simplicity.
2. **`availablePods` always vs on-demand**: issue asks whether to always return it or only when `pod_name` is not specified. Decision: always return it. It's a small field and always useful for agents debugging multi-replica apps.
3. **`sort` import**: add `"sort"` to imports in both files.
4. **`SelectMostRecentPod` edge case**: if all pods have the same `CreationTimestamp`, order is stable (sort preserves relative order for equal elements in Go's `sort.Slice` is not stable — use `sort.SliceStable`).
