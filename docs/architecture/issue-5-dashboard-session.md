# Technical Design: Web Dashboard Session Support + Credential Security (#5)

**Also carries `needs-security-review`.**

---

## Approach

Two parallel tracks: (1) backend — new admin endpoint gated by a separate
`IAF_ADMIN_TOKENS` env var, enforced at middleware level via a dedicated Echo group;
(2) frontend — settings page stores token + optional session ID in `localStorage`,
`fetchWithAuth` adds `X-IAF-Session` header when a session ID is present, and the
hardcoded `"iaf-dev-key"` fallback is removed.

The admin endpoint reads `Application` CRs across all namespaces and is the
primary data source for the operator dashboard view. The per-session scoped endpoint
remains for MCP agents and optional per-session dashboard view.

---

## Changes Required

### Backend

#### `internal/config/config.go`

Add:
```go
AdminTokens []string `mapstructure:"admin_tokens"`
```

Default: empty slice (not populated — admin endpoint is intentionally off by default).
Env var: `IAF_ADMIN_TOKENS` (comma-separated, same viper mechanism as `IAF_API_TOKENS`).

#### New file: `internal/api/handlers/admin.go`

```go
type AdminHandler struct {
    client client.Client
}

func NewAdminHandler(c client.Client) *AdminHandler

// List returns all Application CRs across all namespaces.
// No session required. Requires admin token (enforced at router level).
func (h *AdminHandler) ListApplications(c echo.Context) error
```

`ListApplications` calls `h.client.List(ctx, &list)` without `client.InNamespace` —
this lists across all namespaces. Returns the same `[]ApplicationResponse` shape as the
scoped handler. Includes the `Namespace` field in each response object so the operator
can see which session each app belongs to.

Add `Namespace string` to `ApplicationResponse` in `handlers/applications.go`:
```go
type ApplicationResponse struct {
    Namespace string `json:"namespace"`
    // ... existing fields ...
}
```

Update `toResponse` to populate `Namespace: app.Namespace`.

#### `internal/api/routes.go`

Add `adminTokens []string` parameter to `RegisterRoutes`. Mount admin routes on a
separate Echo group with its own auth middleware:

```go
func RegisterRoutes(
    e *echo.Echo,
    c client.Client,
    cs kubernetes.Interface,
    sessions *auth.SessionStore,
    store *sourcestore.Store,
    adminTokens []string,
) {
    // ... existing routes ...

    // Admin routes — separate token, no session required
    if len(adminTokens) > 0 {
        admin := e.Group("/api/v1/admin")
        admin.Use(middleware.Auth(adminTokens))
        adminHandler := handlers.NewAdminHandler(c)
        admin.GET("/applications", adminHandler.ListApplications)
    }
    // If adminTokens is empty, the /api/v1/admin group is not registered,
    // so all requests to it return 404, not 401.
}
```

Returning 404 (not 401) when admin tokens are not configured prevents information
leakage — callers cannot distinguish "wrong token" from "feature not enabled".

#### `internal/api/server.go`

The existing `middleware.Auth(tokens)` is applied globally and guards the `/api/v1`
routes with agent tokens. The new admin group adds its own auth layer on top. The two
token sets must be disjoint by convention (documented, not technically enforced at
this time).

No changes to `server.go` needed — group-level middleware overrides global for its
routes in Echo.

#### `cmd/apiserver/main.go`

Update `api.RegisterRoutes(...)` call to pass `cfg.AdminTokens`.

#### RBAC

The admin endpoint calls `client.List` across all namespaces. The existing RBAC marker
`+kubebuilder:rbac:groups=iaf.io,resources=applications,verbs=get;list;watch;create;update;patch;delete`
already grants cluster-wide list access (controller-runtime RBAC markers generate
ClusterRole bindings). No new RBAC marker needed.

---

### Frontend

#### New file: `web/src/app/settings/page.tsx`

A settings page with:
- **API Token** text field (`type="password"`) → stored as `iaf_api_key`
- **Session ID** text field (optional) → stored as `iaf_session_id`
- **Save** button: writes to `localStorage`, shows success toast
- **Clear** button: removes both keys from `localStorage`
- Status indicator: shows "Configured" / "Not configured" for each credential

#### `web/src/lib/api.ts`

**Remove the `"iaf-dev-key"` fallback:**

```ts
async function fetchWithAuth(url: string, options?: RequestInit) {
    const apiKey = typeof window !== "undefined"
        ? localStorage.getItem("iaf_api_key")
        : null;

    if (!apiKey) {
        throw new Error("NOT_CONFIGURED");
    }

    const sessionId = typeof window !== "undefined"
        ? localStorage.getItem("iaf_session_id")
        : null;

    const headers: Record<string, string> = {
        ...options?.headers as Record<string, string>,
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
    };

    if (sessionId) {
        headers["X-IAF-Session"] = sessionId;
    }

    const res = await fetch(url, { ...options, headers });
    // ... error handling unchanged
}
```

**New admin function:**

```ts
export async function listAllApplications(): Promise<Application[]> {
    return fetchWithAuth(`${API_BASE}/admin/applications`);
}
```

The `Application` type gains an optional `namespace?: string` field.

#### `web/src/app/page.tsx` (dashboard home)

Replace `listApplications()` call with `listAllApplications()`. Handle the
`"NOT_CONFIGURED"` error: if credentials are missing, render a banner linking to
`/settings` instead of showing empty stats.

```tsx
const { data: apps, error, isLoading } = useQuery({
    queryKey: ["all-applications"],
    queryFn: listAllApplications,
    retry: false,
});

if (error?.message === "NOT_CONFIGURED") {
    return <CredentialPrompt />;
}
```

`CredentialPrompt` is a simple component:
```
"Dashboard requires an admin API token. Configure your credentials in Settings."
[Go to Settings →]
```

#### `web/src/app/applications/page.tsx` and `/[name]/page.tsx`

Same treatment: use `listAllApplications` for the all-apps view. The per-app detail
page (`/applications/[name]`) needs to know the namespace. Since the admin response
includes `namespace`, pass it as a query parameter or store in the URL:
`/applications/[namespace]/[name]` or keep the current path and use a query param
`?namespace=iaf-abc123`.

Recommended: keep `[name]` path param, add `namespace` as query param:
`/applications/my-app?namespace=iaf-abc123`. The detail page reads both and calls the
scoped endpoint with `?session_id` resolved from the namespace.

**Note:** This requires either (a) a new admin endpoint for single-app reads by
namespace+name, or (b) the detail page derives the session ID from the namespace (not
clean). Simplest: add `GET /api/v1/admin/applications/{namespace}/{name}` as a
follow-up, or have the detail page call the admin list and filter client-side for the
MVP. Design as client-side filter for MVP; backend endpoint is deferred.

#### `web/src/app/layout.tsx`

Add "Settings" link to the sidebar nav.

---

## Data / API Changes

**New endpoint:** `GET /api/v1/admin/applications`
- Auth: `Authorization: Bearer <admin-token>` (from `IAF_ADMIN_TOKENS`)
- No session header required
- Response: `[{namespace, name, phase, url, ...ApplicationResponse fields}]`
- Only registered when `IAF_ADMIN_TOKENS` is non-empty; otherwise the path returns 404

**Modified `ApplicationResponse`:** adds `namespace string` field.

---

## Multi-tenancy & Shared Resource Impact

The admin endpoint intentionally crosses namespace boundaries — this is its purpose.
It only reads `Application` CRs (metadata + status), never Secrets, ConfigMaps, or pod
logs. The admin token must be kept out of agent hands (separate env var, different token
value). An agent with its session token cannot reach the admin endpoint.

---

## Security Considerations

**Token separation**: The admin group's `middleware.Auth(adminTokens)` checks only
`IAF_ADMIN_TOKENS` — the agent token is not accepted here. Conversely, the agent's
token can access `/api/v1/applications` but not `/api/v1/admin/...`. The two lists must
be disjoint by documentation and convention.

**404 vs 401 when admin not configured**: Returning 404 (no route registered) prevents
probing for the admin feature. An attacker cannot distinguish "not enabled" from "wrong
path".

**No `"iaf-dev-key"` in client-side code**: After this change, no credential appears in
the source. If `localStorage` is empty, the dashboard shows the `CredentialPrompt`
component. The token is only sent over the wire, never in JS source.

**`localStorage` is not a secrets vault**: This is documented in the settings UI. For
production operator deployments, teams should use a proper secrets manager or
short-lived tokens. The design does not use `sessionStorage` (which would clear on tab
close) to avoid constant re-entry — this is a usability tradeoff documented explicitly.

**CORS**: The existing `CORS: AllowOrigins: ["*"]` setting is acceptable for the
development deployment model. If the dashboard is deployed on a separate origin from
the API, `AllowOrigins` must be restricted to the dashboard origin. Flag this for
review if the production deployment topology changes.

**`needs-security-review` items:**
1. Confirm that the admin Echo group's auth middleware takes precedence over any route
   that might inadvertently match `/api/v1/admin/...` in the global middleware chain.
   In Echo, group-level middleware runs after global middleware — verify that the global
   `middleware.Auth(tokens)` does not reject admin calls with agent tokens before the
   group middleware runs. **Solution**: the global auth middleware validates against
   `IAF_API_TOKENS` (agent tokens). If an admin call uses a token that is not in
   `IAF_API_TOKENS`, it will be rejected by the global middleware before reaching the
   group middleware. **Fix**: the global auth middleware must skip `/api/v1/admin/...`
   routes (add a path skip, similar to `/health` and `/sources/`), and the group
   middleware handles those routes exclusively.

2. The `AdminHandler.ListApplications` reads across all namespaces. Confirm the
   controller-runtime client's RBAC allows cluster-wide `list` on `applications.iaf.io`.
   The existing RBAC marker grants this.

---

## Resource & Performance Impact

`ListApplications` without namespace filter performs a cluster-wide list. On a cluster
with hundreds of applications, this is a full scan. For the MVP, this is acceptable.
Add a `limit` query parameter and cursor-based pagination as a follow-up if needed.

---

## Migration / Compatibility

Existing users of the dashboard with `iaf_api_key` set in localStorage will continue to
work (their key will be read). Those relying on the `"iaf-dev-key"` fallback will see
the `CredentialPrompt` and need to configure their token in the settings page. This is
intentional — the fallback was a security risk.

---

## Open Questions (resolved by architect)

**Q1 — Separate `IAF_ADMIN_TOKENS` or role/scope claim?** Separate `IAF_ADMIN_TOKENS`.
Two separate lists is simpler to reason about, audit, and rotate independently.

**Q2 — Admin view only or both views?** Both: admin view as default (uses admin
endpoint), per-session view when `iaf_session_id` is set (uses scoped endpoint). The
`X-IAF-Session` header is included whenever session ID is configured, enabling
per-session filtering on the existing endpoint without a new UI page.

---

## Open Questions for Developer

1. **Global auth middleware skip for `/api/v1/admin/`**: The global `middleware.Auth`
   must be updated to skip `/api/v1/admin/` paths (add alongside the `/health`,
   `/ready`, `/sources/` skips). Confirm the skip pattern covers the full prefix.

2. **`ApplicationResponse.Namespace`**: Adding `namespace` to the response struct will
   affect the scoped endpoint responses too (the field will be populated there as well).
   Verify that existing callers (MCP `app_status`, etc.) of the REST API are not broken
   by the additional field. JSON additions are backwards-compatible.

3. **Settings page UX**: Determine whether the settings page should use a dedicated
   Next.js route (`/settings`) or a modal/drawer accessible from the layout. Route is
   simpler and more discoverable.

4. **Per-app detail page namespace routing**: The recommended approach (query param
   `?namespace=...`) means the detail page URL is not bookmarkable with just the app
   name. Evaluate whether to add `GET /api/v1/admin/applications/{namespace}/{name}`
   or defer the detail view to require a session ID.
