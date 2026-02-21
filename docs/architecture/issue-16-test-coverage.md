# Architecture: Test Coverage for Controller and REST API Handlers (Issue #16)

## Technical Design

### Approach

Add test files for the two major zero-coverage areas: the controller reconciliation loop and the REST API handlers. Both use the controller-runtime **fake client** (not envtest) to avoid requiring K8s binaries in CI. API handler tests use `httptest` with Echo. Tests are table-driven with `t.Run` subtests. The namespace isolation cross-namespace test is added as a mandatory security regression test. No new production code changes; this design is test-only.

### Changes Required

**New file: `internal/controller/application_controller_test.go`**
- Package `controller` (whitebox — same package as controller)
- Imports: `testing`, `context`, `github.com/dlapiduz/iaf/api/v1alpha1`, `sigs.k8s.io/controller-runtime/pkg/client/fake`, `sigs.k8s.io/controller-runtime/pkg/reconcile`, `k8s.io/apimachinery/pkg/runtime`, `k8s.io/apimachinery/pkg/types`
- Helper `newScheme()` — registers `iafv1alpha1`, `appsv1`, `corev1`, and `unstructured` (for kpack/Traefik)
- Helper `newReconciler(objs ...client.Object) *ApplicationReconciler` — builds fake client with scheme, constructs reconciler with test values (`ClusterBuilder: "test-builder"`, `RegistryPrefix: "ttl.sh/test"`, `BaseDomain: "test.local"`)
- Test functions:

  ```
  TestReconcile_ImageDeploy          — image-based app → Deployment + Service + IngressRoute created, phase Running
  TestReconcile_GitSource_BuildStart — git-source app → kpack Image CR created, phase Building, requeue
  TestReconcile_GitSource_BuildDone  — kpack Image CR present with Succeeded status + latestImage → Deployment created
  TestReconcile_GitSource_BuildFailed— kpack Image CR present with Failed status → phase Failed
  TestReconcile_NoSource             — app with no image/git/blob → phase Failed
  TestReconcile_Idempotent           — reconciling twice with same app does not error (update path)
  ```

- For kpack Image CR: use `unstructured.Unstructured` with `kpack.io/v1alpha2/Image` GVK to simulate what the fake client returns. Set `status.latestImage` and `status.conditions[0].type/status/reason` fields to simulate build outcomes.
- Fake client note: `fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()` — the fake client supports `Create`, `Get`, `Update`, `List`, `Delete` but does NOT run webhooks or owner-reference cascade deletes. Test deletion by checking that objects are absent after calling `Delete` directly; do not test cascade via the reconciler (that is controller-runtime's responsibility, not ours).

**New file: `internal/api/handlers/applications_test.go`**
- Package `handlers_test` (blackbox — uses exported API only)
- Imports: `testing`, `net/http`, `net/http/httptest`, `encoding/json`, `strings`, `github.com/labstack/echo/v4`, `sigs.k8s.io/controller-runtime/pkg/client/fake`, `github.com/dlapiduz/iaf/internal/auth`, `github.com/dlapiduz/iaf/internal/sourcestore`

- Helper `setupHandlerTest(t) (*ApplicationHandler, *echo.Echo, *auth.SessionStore, client.Client)`:
  ```go
  func setupHandlerTest(t *testing.T) (*handlers.ApplicationHandler, *echo.Echo, *auth.SessionStore, client.Client) {
      scheme := runtime.NewScheme()
      iafv1alpha1.AddToScheme(scheme)
      k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
      sessions := auth.NewSessionStore("")  // in-memory, no file
      store := sourcestore.NewStore(t.TempDir())
      h := handlers.NewApplicationHandler(k8sClient, sessions, store)
      e := echo.New()
      return h, e, sessions, k8sClient
  }
  ```

- Helper `makeRequest(method, path, body string, headers map[string]string) (*httptest.ResponseRecorder, echo.Context)` — wraps `httptest.NewRequest` + `httptest.NewRecorder` + `echo.New().NewContext`.

- Test cases (table-driven under `TestApplicationHandler`):

  | Subtest | Endpoint | Setup | Expected |
  |---------|----------|-------|----------|
  | `CreateApp_OK` | `POST /api/v1/applications` | valid session, image app | 201, CR created |
  | `CreateApp_MissingSession` | `POST /api/v1/applications` | no session header | 400, error message |
  | `CreateApp_NoSource` | `POST /api/v1/applications` | valid session, no image/gitUrl | 400 |
  | `ListApps_OK` | `GET /api/v1/applications` | 2 apps in namespace | 200, array len=2 |
  | `ListApps_CrossNamespace` | `GET /api/v1/applications` | session → ns-a; apps in ns-b only | 200, empty array (isolation) |
  | `GetApp_OK` | `GET /api/v1/applications/:name` | app exists | 200, correct name |
  | `GetApp_NotFound` | `GET /api/v1/applications/:name` | no such app | 404 |
  | `DeleteApp_OK` | `DELETE /api/v1/applications/:name` | app exists | 204 |
  | `UploadSource_OK` | `POST /api/v1/applications/:name/source` | app exists, JSON body | 200, blob field set |

- The `ListApps_CrossNamespace` test is the mandatory isolation regression:
  ```go
  // Create session bound to "ns-a"
  sessionID := sessions.Register("ns-a")
  // Pre-create an app in "ns-b" directly via k8sClient
  k8sClient.Create(ctx, appInNsB)
  // Call handler with session for "ns-a"
  // Response must be empty array — never returns ns-b resources
  ```

**New file: `internal/api/handlers/logs_test.go`** (companion to issue #15 test design)
- Tests for `LogsHandler.GetLogs` covering multi-pod selection and `pod_name` validation — listed in issue #15 design; included here for completeness since both test files are in the same package and depend on `internal/k8s/pods.go` from issue #15.
- This test file should be implemented together with `logs.go` changes.

**New file: `internal/middleware/auth_test.go`**
- Tests for `Auth` middleware:
  - Valid token → passes through (200)
  - Missing Authorization header → 401
  - Non-Bearer format → 401
  - Invalid token → 401
  - `/health` path → no auth required (200)
  - `/sources/foo` path → no auth required (200)

### Data / API Changes

None — this is test-only. No production code changes.

### Multi-tenancy & Shared Resource Impact

The `ListApps_CrossNamespace` test codifies the namespace isolation invariant as a regression test. If someone removes `client.InNamespace(namespace)` from `handlers/applications.go`, this test will fail.

### Security Considerations

- **Fake client behavior**: the controller-runtime fake client does NOT enforce RBAC. The namespace isolation test works because `List` with `client.InNamespace("ns-a")` filters results by namespace label selector — this is controller-runtime fake client behavior, not real K8s RBAC. The test validates that the handler passes the correct namespace to the List call.
- **No real secrets or tokens** in test fixtures — use test values like `"test-session-id"`, `"test-token"`.
- **`auth.SessionStore` initialization**: issue #16 asks for missing-session-ID → 400 test. The `NewSessionStore("")` (empty path) must work without creating a file — check `internal/auth/session.go` for file-backed initialization. If it always creates a file, the test helper may need to use `t.TempDir()` for the session store path too.

### Resource & Performance Impact

- Tests run entirely in-process; no K8s binaries, no ports, no network.
- Each test creates its own fake client and session store — no shared state between subtests.

### Migration / Compatibility

- No breaking changes; purely additive test files.
- `make test` will pick these up automatically (standard Go test discovery).

### Open Questions for Developer

1. **`auth.NewSessionStore` signature**: confirm whether `NewSessionStore("")` or `NewSessionStore(t.TempDir())` is needed for in-memory-only use. If the store always tries to write a file, use `t.TempDir()`.
2. **`sourcestore.NewStore` constructor**: confirm the constructor signature and whether it accepts a directory path directly.
3. **Fake client + `unstructured`**: `fake.NewClientBuilder().WithScheme(scheme)` — for kpack `Image` CRs (unstructured), you may need to register the GVK via `scheme.AddToGroupVersion` or use `WithObjects` with a pre-built unstructured object. Test this early; unstructured fake client behavior can be tricky.
4. **`reconcile.Request` construction**: use `reconcile.Request{NamespacedName: types.NamespacedName{Name: "myapp", Namespace: "ns-a"}}` — standard pattern.
5. **Phase assertions**: after reconcile, fetch the updated Application CR via `k8sClient.Get` and assert `app.Status.Phase`. The fake client's `Update`/`Status().Update()` calls are tracked in-memory.
6. **Controller `setRunning` calls `Status().Update()`**: make sure the test scheme includes all needed types so the fake client's status subresource update works. You may need `.WithStatusSubresource(&iafv1alpha1.Application{})` on the fake client builder.
