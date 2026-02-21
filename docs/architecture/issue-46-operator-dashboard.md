# Architecture: Human Operator Dashboard — Unified Observability Entry Point (Issue #46)

## Technical Design

### Approach

Extend the existing Next.js dashboard with three focused changes: (1) add observability signal indicators and Grafana deep-links to the app detail page, (2) add a `/platform` page with platform health summary backed by a new `GET /api/v1/platform` REST endpoint, and (3) add an Observability section to the sidebar navigation that conditionally shows Grafana links. All Grafana URLs are generated server-side from `IAF_GRAFANA_URL` (never from user input) and exposed to the frontend via a new `GET /api/v1/config` endpoint. No Grafana panels are embedded — operators follow links. Prerequisite: issue #5 (session/auth fix) must be merged first.

### Changes Required

**New REST endpoint: `GET /api/v1/config`** (backend):

**New file: `internal/api/handlers/config.go`**:
```go
type ConfigHandler struct {
    GrafanaURL string   // from IAF_GRAFANA_URL env var
    BaseDomain string
}

type ConfigResponse struct {
    GrafanaURL          string `json:"grafanaURL,omitempty"`        // empty if not configured
    BaseDomain          string `json:"baseDomain"`
    ObservabilityEnabled bool  `json:"observabilityEnabled"`         // true when GrafanaURL != ""
}

func (h *ConfigHandler) Get(c echo.Context) error
```

This endpoint requires a valid Bearer token (existing auth middleware) but does NOT require a session ID. The response contains no tenant data — just platform configuration that the frontend needs to render Grafana links. Exposed at `GET /api/v1/config`.

**New REST endpoint: `GET /api/v1/platform`** (backend):

**New file: `internal/api/handlers/platform.go`**:
```go
type PlatformHandler struct {
    client   client.Client
    sessions *auth.SessionStore
    // no store needed
}

type PlatformResponse struct {
    Sessions struct {
        Total  int `json:"total"`
        Active int `json:"active"`    // future: distinguish active from stale
    } `json:"sessions"`
    Applications struct {
        Total    int            `json:"total"`
        ByPhase  map[string]int `json:"byPhase"`
    } `json:"applications"`
    GrafanaDashboardURL string `json:"grafanaDashboardURL,omitempty"`
}

func (h *PlatformHandler) Get(c echo.Context) error
```

Implementation:
1. Require admin token (new check — see Security section)
2. `client.List` all `ApplicationList` across all namespaces (`client.InNamespace("")`) → count by phase
3. `sessions.Count()` → add `Total()` and `Active()` methods to `auth.SessionStore` (count entries in session store)
4. Return `PlatformResponse`

Exposed at `GET /api/v1/platform`.

**Updated `internal/api/routes.go`**:
```go
config := handlers.NewConfigHandler(GrafanaURL, baseDomain)
e.GET("/api/v1/config", config.Get)   // no session required, just auth token

platform := handlers.NewPlatformHandler(c, sessions)
api.GET("/platform", platform.Get)    // admin only
```

**Updated `internal/api/handlers/applications.go`** (REST API response):
- `ApplicationResponse` gains new optional fields:
  ```go
  MetricsEndpoint   string `json:"metricsEndpoint,omitempty"`    // from spec.metrics (issue #30)
  GrafanaMetricsURL string `json:"grafanaMetricsURL,omitempty"`  // pre-computed link, nil if Grafana not configured
  GrafanaLogsURL    string `json:"grafanaLogsURL,omitempty"`
  GrafanaTracesURL  string `json:"grafanaTracesURL,omitempty"`
  ```
  These are computed in `toResponse(app, grafanaURL string)` — helper takes the Grafana URL (from handler config), builds the links if non-empty.

Link formats (when `grafanaURL` is set):
- **Metrics** → `{grafanaURL}/d/app-overview?var-namespace={app.Namespace}&var-app={app.Name}`
- **Logs** → `{grafanaURL}/explore?orgId=1&left={"datasource":"Loki","queries":[{"expr":"{namespace=\"{ns}\",app=\"{name}\"}"}],"range":{"from":"now-1h","to":"now"}}`
- **Traces** → `{grafanaURL}/explore?orgId=1&left={"datasource":"Tempo","queries":[{"query":"{service.name=\"{name}\"}","queryType":"traceql"}],"range":{"from":"now-1h","to":"now"}}`

URL-encode the `left` parameter. App name and namespace come from the `Application` CR (validated K8s names) — not from user input.

`ApplicationHandler` needs `GrafanaURL string` field; wire from `IAF_GRAFANA_URL` env var in `routes.go`.

**Updated `web/src/lib/api.ts`** (frontend):
```typescript
export interface PlatformConfig {
  grafanaURL?: string;
  baseDomain: string;
  observabilityEnabled: boolean;
}

export interface PlatformHealth {
  sessions: { total: number; active: number };
  applications: { total: number; byPhase: Record<string, number> };
  grafanaDashboardURL?: string;
}

export async function getPlatformConfig(): Promise<PlatformConfig> {
  return fetchWithAuth(`${API_BASE}/config`);
}

export async function getPlatformHealth(): Promise<PlatformHealth> {
  return fetchWithAuth(`${API_BASE}/platform`);
}
```

`Application` interface gains:
```typescript
metricsEndpoint?: string;
grafanaMetricsURL?: string;
grafanaLogsURL?: string;
grafanaTracesURL?: string;
```

**Updated `web/src/app/applications/[name]/page.tsx`**:
- Add "Observability" section below the Details grid
- Fetch app data (already fetched); observability fields come from the existing `getApplication` call — no new API call needed
- Component `ObservabilityLinks` renders three rows (Metrics, Logs, Traces):
  - If URL is present: `<a href={url} target="_blank">Open in Grafana →</a>` with a colored status dot (green = configured)
  - If URL is absent but `observabilityEnabled` (from config): amber dot + "Not configured for this app — enable metrics in deploy_app"
  - If `observabilityEnabled` is false (no Grafana): gray dot + "Observability not configured" (hidden entirely if cleaner)
- Also add an "Observability" tab alongside "Application Logs" / "Build Logs" that shows the three links in a more prominent layout

**New file: `web/src/app/platform/page.tsx`** — `/platform` route:
```tsx
"use client";
export default function PlatformPage() {
  const { data: health } = useQuery({ queryKey: ["platform-health"], queryFn: getPlatformHealth });
  // ... render summary cards: active sessions, apps by phase, Grafana link
}
```
- Cards: Total Sessions, Total Applications, Applications by Phase (mini table: Pending/Building/Running/Failed counts)
- "Open Platform Dashboard in Grafana" button (only when `health.grafanaDashboardURL` is set)
- Error handling: if 403 (non-admin token), show "Platform health requires admin access"

**Updated `web/src/app/layout.tsx`** — sidebar navigation:
```tsx
// Add new nav section:
<li className="mt-4 pt-4 border-t border-gray-800">
  <Link href="/platform" ...>Platform Health</Link>
</li>

{config?.observabilityEnabled && (
  <li>
    <a href={config.grafanaURL} target="_blank" ...>Grafana</a>
  </li>
)}
```

The layout fetches `getPlatformConfig()` via a React Query `useQuery` at the layout level (or a context provider) so observability links are conditionally shown across all pages.

**New file: `web/src/components/observability-links.tsx`** — reusable component used on the app detail page.

**Admin token check** (`internal/api/handlers/platform.go`):
```go
// Read IAF_ADMIN_TOKENS env var (comma-separated, same format as IAF_API_TOKENS)
// GET /api/v1/platform requires a token from IAF_ADMIN_TOKENS (not just IAF_API_TOKENS)
// If IAF_ADMIN_TOKENS is empty, fall back to IAF_API_TOKENS (so single-token setups work)
```
This is a simple string comparison in the handler, not a new middleware layer. The existing `Auth` middleware already validates the token is in `IAF_API_TOKENS`; the platform handler additionally checks it is in `IAF_ADMIN_TOKENS`. The two sets can overlap.

**`auth.SessionStore`** — add methods:
```go
func (s *SessionStore) Count() int   // returns len of session map
```

**Tests**:

`internal/api/handlers/config_test.go`:
- `TestConfig_WithGrafanaURL`: returns `observabilityEnabled: true` and `grafanaURL`
- `TestConfig_WithoutGrafanaURL`: returns `observabilityEnabled: false`, no `grafanaURL`

`internal/api/handlers/platform_test.go`:
- `TestPlatform_AdminToken`: returns correct phase counts
- `TestPlatform_NonAdminToken`: returns 403

`internal/api/handlers/applications_test.go` (extend from issue #16):
- `TestGetApp_GrafanaLinksPresent`: handler configured with `GrafanaURL` → response includes all three links
- `TestGetApp_GrafanaLinksAbsent`: no `GrafanaURL` → links absent from response

### Data / API Changes

**New endpoints**:
- `GET /api/v1/config` — platform config for frontend (auth required, no session)
- `GET /api/v1/platform` — platform health summary (admin token required)

**Updated `ApplicationResponse`**: three new optional Grafana URL fields.

**New env vars**:
- `IAF_GRAFANA_URL` — Grafana base URL (e.g., `http://grafana.localhost`); default empty (disables Grafana links); `make setup-local` should document default as `http://grafana.localhost`
- `IAF_ADMIN_TOKENS` — comma-separated admin-level tokens (optional; falls back to `IAF_API_TOKENS` if empty)

**Frontend interface changes**: `Application` gains three optional observability URL fields; new `PlatformConfig` and `PlatformHealth` interfaces.

### Multi-tenancy & Shared Resource Impact

- `GET /api/v1/config` returns global platform config only — no tenant data, no namespace scoping needed.
- `GET /api/v1/platform` performs a cluster-wide `ApplicationList` (no namespace filter). This is intentional — it's a cross-tenant admin view. The admin token requirement enforces that only operators with elevated access can see the cross-tenant summary.
- Grafana links on the app detail page are scoped by the handler: `app.Namespace` is resolved from the session (existing session resolution in `ApplicationHandler.resolveNamespace`). An operator viewing `/applications/myapp` sees Grafana links filtered to their session's namespace. Admin operators can see the platform dashboard which is cross-namespace by design.

### Security Considerations

- **Grafana link generation**: `app.Name` and `app.Namespace` come from the `Application` CR — validated K8s names. They are URL-encoded when embedded in the `left` parameter. No user input is accepted in link generation; `IAF_GRAFANA_URL` is platform config.
- **`IAF_GRAFANA_URL` SSRF**: if `IAF_GRAFANA_URL` is set to an internal service, the frontend would expose internal URL patterns to users who can see the dashboard. Mitigation: `IAF_GRAFANA_URL` is set by the platform operator, not agents. Document that it should be a publicly reachable URL or a localhost-only URL for local dev. The backend does not fetch from `IAF_GRAFANA_URL` (it only embeds it in responses) — no server-side SSRF risk, only client-side link exposure.
- **`GET /api/v1/platform` admin check**: the admin check must happen AFTER the existing auth middleware validates the bearer token. Do not implement a separate auth path — add the admin check as a guard inside the handler. Fail with `403 Forbidden` and body `{"error": "admin access required"}` if not an admin token.
- **Session count endpoint**: `sessions.Count()` exposes the total number of sessions to admin-token holders. This is acceptable for operators. Ensure count does not include session IDs or any session content in the response — only the integer count.
- **Env var `env` tag `omitempty`**: all new optional `ApplicationResponse` fields must have `omitempty` to avoid leaking empty Grafana URL strings to non-admin callers.

### Resource & Performance Impact

- `GET /api/v1/platform` performs a cluster-wide `List` across all application CRs. On a large cluster with many sessions, this can be expensive. Add a 10-second cache in the handler (use `sync.Map` or a simple struct with `time.Time` expiry) to avoid hammering the K8s API on dashboard refresh.
- `GET /api/v1/config` is cheap — returns hardcoded config values; no K8s API call.
- Grafana link generation: string operations in `toResponse` — negligible.
- Frontend: `getPlatformConfig()` is called once at layout mount (React Query caches by default for 5 minutes). No polling.

### Migration / Compatibility

- New env vars `IAF_GRAFANA_URL` and `IAF_ADMIN_TOKENS` are optional — existing deployments unaffected.
- `ApplicationResponse` new fields are additive (`omitempty`) — existing API callers unaffected.
- `GET /api/v1/config` and `GET /api/v1/platform` are new routes — no breaking changes.
- Depends on issue #5 (session/auth) as prerequisite for the app detail page to show correct data.
- Depends on issues #29/#31/#36 (observability stack) for Grafana links to actually work, but the code can ship independently — links are simply absent when Grafana is not configured.

### Open Questions for Developer

1. **Admin token check location**: the cleanest implementation is a dedicated `AdminAuth(adminTokens, fallbackTokens)` middleware, or a simple check inside `PlatformHandler.Get`. The handler-level check is simpler and avoids adding a new middleware layer. Confirm preference.
2. **`SessionStore.Count()`**: the current `SessionStore` has a `sessions map[string]*Session` with a mutex. `Count()` takes a read lock and returns `len(sessions)`. Confirm the session file backing store is loaded at startup so `Count()` reflects all existing sessions (not just in-memory ones created since last restart).
3. **Grafana Explore URL encoding**: the `left` parameter is a JSON string that must be URL-encoded. Use `url.QueryEscape(json.Marshal(leftObj))`. Test with a real Grafana instance to confirm the URL format is correct for the kube-prometheus-stack Grafana version.
4. **`IAF_GRAFANA_URL` default**: default to empty (Grafana links hidden) rather than a hardcoded `http://grafana.localhost`. Document that `make setup-local` configures this in `config/deploy/platform.yaml` once the observability stack is deployed.
5. **Layout-level config fetch**: the `layout.tsx` is a Server Component in Next.js App Router — `useQuery` cannot be called there. Either use a Client Component wrapper (like the existing `Providers`), or fetch config server-side and pass as a prop. Client component wrapper is simpler and consistent with the existing pattern (`providers.tsx`). Consider adding a `ConfigProvider` context that fetches `/api/v1/config` once on app mount.
6. **`GET /api/v1/platform` caching**: implement a simple in-memory cache with a 10-second TTL. Do not use Redis or an external cache — this is a low-frequency admin endpoint.
