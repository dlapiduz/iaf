# Technical Design: Controller Phase Correctness (#10 + #11)

Issues #10 and #11 share the same root cause and must be implemented together.

---

## Approach

The controller currently calls `setRunning` immediately after `reconcileDeployment`
succeeds, before any pods are ready. This conflates "Deployment object created" with
"application is serving traffic". The fix is straightforward: read the Deployment's
actual `Status.AvailableReplicas` after every reconcile pass and drive phase transitions
from that value. A new `setDeploying` helper holds the app in `Deploying` phase and
requeues at a bounded interval. A watch on kpack `Image` CRs (which already carry
owner references back to the `Application`) completes the event loop for build completion.

---

## Changes Required

### `internal/controller/application_controller.go`

**1. Change `reconcileDeployment` signature to return `*appsv1.Deployment`**

Instead of a second `r.Get` call for the Deployment, return the object that was just
created or fetched inside `reconcileDeployment`. This eliminates one API round-trip and
makes the status-reading logic cleaner.

New signature:
```go
func (r *ApplicationReconciler) reconcileDeployment(
    ctx context.Context, app *iafv1alpha1.Application, image string,
) (*appsv1.Deployment, error)
```

The function returns the `existing` Deployment (after create or update) and `nil` error
on success.

**2. Replace the end of `Reconcile` with a phase-aware helper**

Current code at the end of `Reconcile`:
```go
return r.setRunning(ctx, &app, deployImage)
```

Replace with:
```go
// Set Deploying before touching the Deployment (satisfies issue #10 AC)
if app.Status.Phase == iafv1alpha1.ApplicationPhaseBuilding ||
    app.Status.Phase == iafv1alpha1.ApplicationPhasePending ||
    app.Status.Phase == "" {
    if err := r.setDeployingPhaseOnly(ctx, &app); err != nil {
        return ctrl.Result{}, err
    }
}

dep, err := r.reconcileDeployment(ctx, &app, deployImage)
if err != nil {
    return r.setFailed(ctx, &app, fmt.Sprintf("deployment reconciliation failed: %v", err))
}

if err := r.reconcileService(ctx, &app); err != nil {
    return r.setFailed(ctx, &app, fmt.Sprintf("service reconciliation failed: %v", err))
}
if err := r.reconcileIngressRoute(ctx, &app); err != nil {
    return r.setFailed(ctx, &app, fmt.Sprintf("ingress route reconciliation failed: %v", err))
}

return r.reconcileStatus(ctx, &app, deployImage, dep)
```

**3. New helper: `setDeployingPhaseOnly`**

Sets only the phase field (no URL yet, no replica count). Used before Deployment is
created so observing agents see `Deploying` during the creation window.

```go
func (r *ApplicationReconciler) setDeployingPhaseOnly(
    ctx context.Context, app *iafv1alpha1.Application,
) error {
    app.Status.Phase = iafv1alpha1.ApplicationPhaseDeploying
    return r.Status().Update(ctx, app)
}
```

**4. New helper: `reconcileStatus`**

Central place for all end-of-reconcile status decisions. Replaces `setRunning`.

```go
func (r *ApplicationReconciler) reconcileStatus(
    ctx context.Context,
    app *iafv1alpha1.Application,
    image string,
    dep *appsv1.Deployment,
) (ctrl.Result, error) {
    host := app.Spec.Host
    if host == "" {
        host = fmt.Sprintf("%s.%s", app.Name, r.BaseDomain)
    }
    url := fmt.Sprintf("http://%s", host)

    available := dep.Status.AvailableReplicas

    // Always sync available replicas and URL
    app.Status.AvailableReplicas = available
    app.Status.URL = url
    app.Status.LatestImage = image

    if available >= 1 {
        // At least one pod is ready — mark Running
        app.Status.Phase = iafv1alpha1.ApplicationPhaseRunning
        meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
            Type:               "Ready",
            Status:             metav1.ConditionTrue,
            Reason:             "Deployed",
            Message:            "Application is running",
            LastTransitionTime: metav1.Now(),
        })
        if err := r.Status().Update(ctx, app); err != nil {
            return ctrl.Result{}, err
        }
        return ctrl.Result{}, nil
    }

    // No pods ready — hold in Deploying and requeue
    app.Status.Phase = iafv1alpha1.ApplicationPhaseDeploying
    meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
        Type:               "Ready",
        Status:             metav1.ConditionFalse,
        Reason:             "Deploying",
        Message:            "Waiting for pods to become available",
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Update(ctx, app); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

**Important detail**: `setDeployingPhaseOnly` updates status once (before Deployment
creation), and `reconcileStatus` updates it a second time (after reading actual
availability). Two status writes per pass is acceptable because both are cheap
subresource updates. However, after `setDeployingPhaseOnly` updates the `app` object
(including its `ResourceVersion`), subsequent status writes use the refreshed version —
this is safe because controller-runtime's fake and real clients both update `ResourceVersion`
in the passed pointer after a successful write.

**5. Remove the old `setRunning` function**

It is fully replaced by `reconcileStatus`. Delete it.

**6. `SetupWithManager`: add kpack `Image` watch**

kpack `Image` CRs already carry `OwnerReferences` pointing to the `Application`
(confirmed in `internal/k8s/kpack.go:BuildKpackImage`). The controller-runtime
`handler.EnqueueRequestForOwner` can map kpack Image events back to their Application
owner.

```go
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
    kpackImage := &unstructured.Unstructured{}
    kpackImage.SetGroupVersionKind(iafk8s.KpackImageGVK)

    return ctrl.NewControllerManagedBy(mgr).
        For(&iafv1alpha1.Application{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.Service{}).
        Watches(
            kpackImage,
            handler.EnqueueRequestForOwner(
                mgr.GetScheme(),
                mgr.GetRESTMapper(),
                &iafv1alpha1.Application{},
                handler.OnlyControllerOwner(),
            ),
        ).
        Named("application").
        Complete(r)
}
```

Add import: `"sigs.k8s.io/controller-runtime/pkg/handler"`.

A label-selector predicate filtering on `iaf.io/application` label exists can be added
optionally to avoid triggering reconciliation for kpack Images not created by IAF, but
since IAF controls the session namespaces, any kpack Image in those namespaces is
IAF-owned. Keep it simple; skip the predicate for now.

---

## Data / API Changes

No CRD field changes. The `ApplicationPhaseDeploying` constant already exists in
`api/v1alpha1/application_types.go` — it just needs to be used.

Observable changes to agent-facing output:
- `app_status` can now return `phase: Deploying` (previously the phase jumped directly
  from `Building` to `Running` or was briefly set to `Running` with `availableReplicas: 0`)
- `availableReplicas` in `app_status` reflects real pod availability on every reconcile pass
- `url` is now set during `Deploying` phase (not just `Running`), so agents can record
  the URL before the app is ready rather than polling until Running and then re-reading

The `iaf://schema/application` MCP resource should be updated to document `Deploying` as:
> `Deploying` — image is resolved; Kubernetes Deployment is created; waiting for at
> least one pod replica to become available. The app URL is set and DNS is active but
> the application may not yet be accepting connections.

---

## Multi-tenancy & Shared Resource Impact

The kpack Image watch is namespace-scoped via the manager's cache (controller-runtime
default). No cross-namespace event leakage.

The `RequeueAfter: 10 * time.Second` is bounded and well below any reasonable API rate
limit. The Deployment ownership watch means most transitions (Building→Deploying→Running,
Running→Deploying on crash) are event-driven, not timer-driven.

---

## Security Considerations

None. This change is entirely within the reconcile loop. No new RBAC, secrets, or
external callers. The kpack Image RBAC marker already grants `kpack.io/images:
get;list;watch;create;update;patch;delete` — the new watch requires only `list;watch`,
which is already covered.

---

## Resource & Performance Impact

- **One extra status write per reconcile pass** when phase transitions through
  `setDeployingPhaseOnly`. This is at most one extra subresource write per build cycle,
  not per second. Negligible.
- **RequeueAfter: 10s** during `Deploying` is bounded. It stops as soon as the Deployment
  ownership watch fires (typically within a few seconds of pod readiness).
- **kpack Image watch**: Each kpack Image status update triggers one reconcile of the
  owning Application. Build workflows produce O(10) such updates total. Negligible.

---

## Migration / Compatibility

No CRD changes. Existing `Application` CRs already have `phase: Running` set with
`availableReplicas: 0` in some cases. On the first reconcile after this deploy, if the
Deployment's `AvailableReplicas` is currently 0, those apps will transition to
`phase: Deploying`. This is a correct state correction, not a regression.

Agents that only check for `phase: Running` before hitting the URL will now wait for
actual readiness — this is the desired behavior.

---

## Open Questions (resolved by architect)

**#10 Q1 — Set `Failed` after timeout?** No. Keep the app in `Deploying` indefinitely.
A crash-looping pod stays in `Deploying`, not `Failed`. Agents can inspect
`app_logs` to understand why. A configurable timeout + `Failed` transition is deferred
to a separate issue. Using `Failed` for a pod that is still trying would be misleading.

**#10 Q2 — `Running` requires `>= 1` or all desired replicas?** `>= 1`. This matches
standard K8s readiness semantics (apps are often rolling-updated with partial
availability). Requiring all replicas would cause permanent `Deploying` for apps where
the desired count exceeds cluster capacity.

**#11 Q1 — Label selector predicate on kpack Image watch?** No, skip it for now. IAF
owns the session namespaces; any kpack Image in those namespaces is IAF-managed.

**#11 Q2 — Race between kpack Image status update and controller read?** Not a problem.
The watch fires after the kpack status is already written. The reconcile reads the
current Image status and gets the completed `latestImage`. No race.

**#11 — Does `.Owns(&appsv1.Deployment{})` trigger on status changes?** In
controller-runtime v0.23.1, `Owns` uses `ResourceVersionChangedPredicate` by default.
Deployment status updates change `resourceVersion`, so yes, they trigger reconciliation.
The developer should add a test to confirm this assumption.

---

## Open Questions for Developer

1. After `setDeployingPhaseOnly` calls `r.Status().Update(ctx, app)`, the `app` pointer
   is updated with the new `ResourceVersion`. The subsequent call to
   `r.Status().Update(ctx, app)` inside `reconcileStatus` uses this updated pointer.
   Verify in tests that no `Conflict` errors occur when running the full reconcile cycle
   with the fake client.

2. The new `Watches(kpackImage, ...)` call requires the kpack CRD to be installed in the
   cluster for the manager to start successfully. If kpack is not installed, the watch
   registration will fail at startup. Consider wrapping the Watches call in an existence
   check, or documenting that kpack is a hard prerequisite (which it already effectively
   is, since the controller creates kpack Images). Recommend: document as prerequisite
   rather than adding a runtime check.

3. The `iaf://schema/application` MCP resource — confirm which file defines it and update
   the `Deploying` phase description there.

---

## New Test File: `internal/controller/application_controller_test.go`

Use the controller-runtime fake client. Tests do not require `envtest`.

Required test cases (table-driven where appropriate):

| # | Description | Setup | Expected result |
|---|---|---|---|
| 1 | Image app, Deployment just created (0 available) | App with Spec.Image set; fake Deployment with AvailableReplicas=0 | Phase=Deploying, Result.RequeueAfter=10s |
| 2 | Image app, Deployment has 1 available | App with Spec.Image; fake Deployment with AvailableReplicas=1 | Phase=Running, Result=empty |
| 3 | Running app, Deployment drops to 0 | App with Phase=Running; Deployment updated to AvailableReplicas=0 | Phase=Deploying, RequeueAfter=10s |
| 4 | kpack app, build still running | App with Spec.Blob; kpack Image with Ready=Unknown | Phase=Building, RequeueAfter=5s |
| 5 | kpack app, build complete, Deployment 0 available | kpack Image Ready=True with latestImage; Deployment AvailableReplicas=0 | Phase=Deploying, RequeueAfter=10s |
| 6 | kpack app, build complete, Deployment 1 available | kpack Image Ready=True; Deployment AvailableReplicas=1 | Phase=Running |
| 7 | availableReplicas always synced | Any pass with Deployment.AvailableReplicas=2 | app.Status.AvailableReplicas=2 |

For kpack Image tests: use the fake client with the unstructured kpack Image object
(set `GVK`, `status.conditions`, `status.latestImage`).
