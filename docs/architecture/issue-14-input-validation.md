# Technical Design: Input Validation (#14)

## Approach

Introduce a single `internal/validation` package that owns all user-input validation
rules shared across the MCP layer and the REST API layer. Both layers call the same
functions so validation is consistent and tested in one place. The sourceType
inconsistency between `app_status` and `list_apps` is fixed as a one-line change in
`status.go`; no new package is needed for that.

---

## Changes Required

### New file: `internal/validation/validation.go`

Contains two exported functions:

```go
// ValidateAppName returns a descriptive error if name is not a valid DNS label.
// Valid: non-empty, matches ^[a-z0-9][a-z0-9-]*$, max 63 chars,
// does NOT start with "kube-" or "iaf-" (reserved platform prefixes).
func ValidateAppName(name string) error

// ValidateEnvVarName returns a descriptive error if name is not a valid
// POSIX environment variable name: matches ^[A-Za-z_][A-Za-z0-9_]*$.
func ValidateEnvVarName(name string) error
```

Both use pre-compiled `regexp.MustCompile` package-level vars (compile once, reuse).

Error messages must be self-describing for agentic callers, e.g.:
- `"app name must match ^[a-z0-9][a-z0-9-]*$ and be 63 characters or less"`
- `"app name must not start with reserved prefix \"kube-\""`
- `"env var name \"3INVALID\" must match ^[A-Za-z_][A-Za-z0-9_]*$"`

### New file: `internal/validation/validation_test.go`

Table-driven tests covering:
- Valid app names pass
- Empty name → error
- Uppercase letters → error
- Leading hyphen → error
- Exceeds 63 chars → error
- `kube-system` → error (reserved prefix `kube-`)
- `iaf-controller` → error (reserved prefix `iaf-`)
- Valid env var names pass
- Name starting with digit → error
- Name with space → error
- Name with hyphen → error

### `internal/mcp/tools/deploy.go`

After the existing `input.Name == ""` check, call `validation.ValidateAppName(input.Name)`.
After resolving env vars, iterate `input.Env` and call `validation.ValidateEnvVarName(e.Name)`
for each entry; return tool error on first failure.

Remove the `input.Name == ""` check — `ValidateAppName` already covers the empty case.

### `internal/mcp/tools/push_code.go`

Same pattern: replace empty-name check with `ValidateAppName`. Add env var loop calling
`ValidateEnvVarName`. Both calls happen before `deps.Store.StoreFiles` so no side effects
occur on invalid input.

### `internal/mcp/tools/status.go`

Replace `"blob"` with `"code"` in the `app.Spec.Blob != ""` branch (line 61). This makes
`app_status` consistent with `list_apps`. Add `ValidateAppName` call after empty-name check.

### `internal/mcp/tools/delete.go`

Replace empty-name check with `ValidateAppName`.

### `internal/mcp/tools/logs.go` (both `RegisterAppLogs` and `RegisterAppLogsWithClientset`)

Replace empty-name check with `ValidateAppName` in both functions.

### `internal/api/handlers/applications.go` — `Create`

After `req.Name == ""` check, add `validation.ValidateAppName(req.Name)`, returning
`http.StatusBadRequest` with `{"error": "<message>"}` on failure.

Add env var validation loop over `req.Env` before `h.client.Create`.

### `internal/api/handlers/applications.go` — `Update`

The `Update` handler takes the name from the URL path param (`c.Param("name")`). The
path param is user-controlled via the HTTP request, so it must also be validated.
Add `validation.ValidateAppName(name)` before the `h.client.Get` call.

Add env var validation loop over `req.Env` when `req.Env != nil`.

### `internal/sourcestore/store.go` — `StoreFiles` path check

The existing path traversal check (`strings.HasPrefix(cleanPath, "..")`) is close but
has a subtle flaw: a filename literally named `..secret` (starts with `..` but isn't a
traversal) is incorrectly rejected, and the check relies on string prefix rather than
confirming the resolved path stays inside the target directory.

Replace the current check with the canonical approach:

```go
cleanPath := filepath.Clean(path)
fullPath := filepath.Join(appDir, cleanPath)
if !strings.HasPrefix(filepath.Clean(fullPath)+"/", filepath.Clean(appDir)+"/") {
    return "", fmt.Errorf("invalid file path %q: must not escape upload directory", path)
}
```

This is a correctness/security hardening; it has no effect on valid inputs.

---

## Data / API Changes

No CRD changes. No new API endpoints. No request/response schema changes — validation
errors are surfaced through the existing error return paths (MCP tool error / HTTP 400).

The `sourceType` field in `app_status` tool output changes from `"blob"` → `"code"` when
`Spec.Blob` is set. This is a **breaking change** for any caller that branches on
`sourceType == "blob"`, but because `"blob"` was an internal implementation detail never
documented as a stable value, and `list_apps` already returns `"code"`, this is a bugfix.

---

## Multi-tenancy & Shared Resource Impact

Validation happens entirely in-process before any K8s API call. No shared resources are
touched. App name validation is additive to the existing `CheckAppNameAvailable` check
(which prevents cross-session hostname collisions); both checks must pass.

Namespace and session identifiers are not user-controlled in the same way (they are
generated by the platform), so they are not in scope here.

---

## Security Considerations

- **Regex anchoring**: both regexes must use `^` and `$` anchors (or `regexp.MatchString`
  equivalent) to prevent partial matches on multi-line or embedded inputs.
- **Reserved prefix blocking**: `kube-` and `iaf-` prefixes prevent an agent from
  creating apps that could shadow or collide with platform namespaces and service accounts.
- **Path traversal hardening**: the `StoreFiles` improvement eliminates the corner case
  where `filepath.Clean` + prefix check diverge on edge-case filenames.
- **No `needs-security-review` escalation required**: this issue is input sanitisation
  with no secrets, RBAC changes, or cross-tenant data access. The security improvement
  to `StoreFiles` is contained in the sourcestore package.

---

## Resource & Performance Impact

Negligible. Pre-compiled regexes cost one allocation at startup. Each tool call adds at
most one regex match (O(n) in name length, bounded at 63 chars) and one loop over env
vars (typically < 20 entries).

---

## Migration / Compatibility

- Any agent passing names that are currently accepted by Kubernetes but fail the new
  regex (e.g. names with uppercase letters that K8s silently rejects anyway) will now
  receive a clean 400/tool error instead of an opaque K8s API error. This is strictly an
  improvement from the agent's perspective.
- The `sourceType: "blob"` → `"code"` change is a bugfix. Any agent checking for
  `"blob"` was already broken (since `list_apps` returned `"code"`).

No CRD migration needed. No rollout ordering constraints.

---

## Open Questions (resolved)

1. **Reserved names**: Yes, block `kube-` and `iaf-` prefixes. These are the only
   well-known platform-owned prefixes; no other prefixes need blocking at this time.
   The error message should explain the conflict: `"app name must not use reserved prefix
   \"iaf-\"; choose a different name"`.

2. **Package location**: `internal/validation` is the right home. Keeping validation
   in the tools package would duplicate logic for the REST layer.

---

## Open Questions for Developer

1. `ValidateEnvVarName` is only called at create/update time. The `Application` CRD spec
   does not currently enforce env var name format via a CEL validation rule or webhook.
   Should a `+kubebuilder:validation:Pattern` marker be added to `EnvVar.Name` in
   `api/v1alpha1/` so the K8s API server also enforces it (defence in depth)? This is
   low-risk but requires `make generate`. Recommend: yes, add it while you're here.

2. The `Update` handler in `handlers/applications.go` currently does not validate whether
   the updated `image` or `gitUrl` values are non-empty after stripping the other source
   type. Confirm whether that is in scope for this issue or deferred to a future
   validation pass.
