# Technical Design: Session Lifecycle — Expiry, Unregister, Cleanup (#6)

---

## Approach

Three coordinated parts: (1) extend `Session` with `LastActivityAt` and `TTL` fields and
add `Delete`, `Touch`, and `ListExpired` methods to `SessionStore`; (2) a new MCP tool
`unregister` that deletes apps, source store, K8s namespace, and session record; (3) a
background GC goroutine in the apiserver that runs the same cleanup logic on a timer for
expired sessions. Activity is tracked by calling `Touch` in `ResolveNamespace`, so every
tool call resets the TTL clock.

---

## Changes Required

### `internal/auth/session.go`

**Extend `Session`:**

```go
type Session struct {
    ID             string        `json:"id"`
    Namespace      string        `json:"namespace"`
    Name           string        `json:"name"`
    CreatedAt      time.Time     `json:"created_at"`
    LastActivityAt time.Time     `json:"last_activity_at"`
    TTL            time.Duration `json:"ttl,omitempty"` // nanoseconds; 0 = no expiry
}
```

`TTL` is set at registration time from `cfg.SessionTTL`. Agents cannot override it.

**New `SessionStore` methods:**

```go
// Delete removes a session. Returns error if not found.
func (s *SessionStore) Delete(sessionID string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if _, ok := s.sessions[sessionID]; !ok {
        return fmt.Errorf("session %q not found", sessionID)
    }
    delete(s.sessions, sessionID)
    return s.persistLocked()
}

// Touch updates LastActivityAt. Called on every tool invocation.
func (s *SessionStore) Touch(sessionID string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if sess, ok := s.sessions[sessionID]; ok {
        sess.LastActivityAt = time.Now().UTC()
        _ = s.persistLocked() // best-effort; GC correctness depends on in-memory state
    }
}

// ListExpired returns sessions whose TTL has elapsed since LastActivityAt.
// Sessions with TTL == 0 are never expired.
func (s *SessionStore) ListExpired() []*Session {
    s.mu.RLock()
    defer s.mu.RUnlock()
    now := time.Now().UTC()
    var expired []*Session
    for _, sess := range s.sessions {
        if sess.TTL > 0 && now.Sub(sess.LastActivityAt) > sess.TTL {
            // Make a copy to avoid returning a pointer into the map
            copy := *sess
            expired = append(expired, &copy)
        }
    }
    return expired
}

// List returns copies of all sessions (for GC and admin views).
func (s *SessionStore) List() []*Session {
    s.mu.RLock()
    defer s.mu.RUnlock()
    result := make([]*Session, 0, len(s.sessions))
    for _, sess := range s.sessions {
        copy := *sess
        result = append(result, &copy)
    }
    return result
}
```

**Update `Register` to accept TTL:**

```go
func (s *SessionStore) Register(name string, ttl time.Duration) (*Session, error) {
    // ... existing ID generation ...
    sess := &Session{
        ID:             id,
        Namespace:      "iaf-" + id,
        Name:           name,
        CreatedAt:      now,
        LastActivityAt: now,
        TTL:            ttl,
    }
    // ...
}
```

The `register` MCP tool and the REST `register` endpoint pass `cfg.SessionTTL` here.

### `internal/mcp/tools/deps.go`

Update `ResolveNamespace` to call `Touch` after a successful lookup:

```go
func (d *Dependencies) ResolveNamespace(sessionID string) (string, error) {
    sess, ok := d.Sessions.Lookup(sessionID)
    if !ok {
        return "", fmt.Errorf("session not found, call the register tool first")
    }
    d.Sessions.Touch(sessionID)
    return sess.Namespace, nil
}
```

### `internal/mcp/tools/register.go`

Update `RegisterRegisterTool` to pass TTL to `Register` and include it in the response:

```go
sess, err := deps.Sessions.Register(input.Name, deps.SessionTTL)
// ...
result := map[string]any{
    "session_id": sess.ID,
    "namespace":  sess.Namespace,
    "message":    "...",
}
if sess.TTL > 0 {
    result["ttl_seconds"] = int64(sess.TTL.Seconds())
    result["expires_after"] = fmt.Sprintf("%.0f hours of inactivity", sess.TTL.Hours())
}
```

Add `SessionTTL time.Duration` to `Dependencies` struct.

### `internal/mcp/tools/deps.go` — `Dependencies` struct

```go
type Dependencies struct {
    Client     client.Client
    Store      *sourcestore.Store
    BaseDomain string
    Sessions   *auth.SessionStore
    SessionTTL time.Duration // 0 = no expiry
}
```

### New file: `internal/mcp/tools/unregister.go`

```go
type UnregisterInput struct {
    SessionID string `json:"session_id" jsonschema:"required - session ID to clean up"`
}

func RegisterUnregisterTool(server *gomcp.Server, deps *Dependencies) {
    gomcp.AddTool(server, &gomcp.Tool{
        Name:        "unregister",
        Description: "Delete all applications and resources for your session and release the namespace. Call this when you are done. Returns a summary of what was deleted.",
    }, func(ctx context.Context, req *gomcp.CallToolRequest, input UnregisterInput) (*gomcp.CallToolResult, any, error) {
        // Validate session exists
        sess, ok := deps.Sessions.Lookup(input.SessionID)
        if !ok {
            return nil, nil, fmt.Errorf("session not found")
        }
        namespace := sess.Namespace

        // List apps for the summary
        var list iafv1alpha1.ApplicationList
        _ = deps.Client.List(ctx, &list, client.InNamespace(namespace))
        appNames := make([]string, 0, len(list.Items))
        for _, app := range list.Items {
            appNames = append(appNames, app.Name)
        }

        // Delete source tarballs for the whole namespace
        _ = deps.Store.DeleteNamespace(namespace)

        // Delete the K8s namespace (cascades to all resources within it)
        ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
        if err := deps.Client.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
            return nil, nil, fmt.Errorf("deleting namespace: %w", err)
        }

        // Remove session from store
        if err := deps.Sessions.Delete(input.SessionID); err != nil {
            return nil, nil, fmt.Errorf("deleting session: %w", err)
        }

        result := map[string]any{
            "status":          "unregistered",
            "namespace":       namespace,
            "deletedApps":     appNames,
            "deletedAppCount": len(appNames),
            "message":         fmt.Sprintf("Session %q unregistered. Namespace %q and %d application(s) deleted.", input.SessionID, namespace, len(appNames)),
        }
        text, _ := json.MarshalIndent(result, "", "  ")
        return &gomcp.CallToolResult{Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}}}, nil, nil
    })
}
```

**Security note**: log at WARN before namespace deletion. Add:
```go
logger.Warn("unregistering session",
    "session_id", input.SessionID,
    "namespace", namespace,
    "app_count", len(appNames))
```

where `logger` comes from `deps` (add `Logger *slog.Logger` to `Dependencies`).

### `internal/sourcestore/store.go` — new method

```go
// DeleteNamespace removes all source tarballs for an entire session namespace.
func (s *Store) DeleteNamespace(namespace string) error {
    return os.RemoveAll(filepath.Join(s.dir, namespace))
}
```

### `internal/mcp/server.go`

Register `RegisterUnregisterTool(server, deps)` alongside other tools.

Update `serverInstructions` to mention `unregister`.

### New file: `internal/sessiongc/gc.go`

Background GC logic extracted into its own package for testability:

```go
package sessiongc

// Cleaner encapsulates the cleanup logic shared by unregister and GC.
type Cleaner struct {
    Client  client.Client
    Store   *sourcestore.Store
    Sessions *auth.SessionStore
    Logger  *slog.Logger
}

// CleanupSession deletes all resources for a session. Idempotent.
func (c *Cleaner) CleanupSession(ctx context.Context, sessionID, namespace string) error {
    // Delete source store namespace directory
    _ = c.Store.DeleteNamespace(namespace)

    // Delete K8s namespace
    ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
    if err := c.Client.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
        return fmt.Errorf("deleting namespace: %w", err)
    }

    // Remove session record
    return c.Sessions.Delete(sessionID)
}

// RunGC checks for expired sessions and cleans them up.
// Designed to be called on a ticker.
func (c *Cleaner) RunGC(ctx context.Context) {
    expired := c.Sessions.ListExpired()
    for _, sess := range expired {
        c.Logger.Warn("GC: expiring session",
            "session_id", sess.ID,
            "namespace", sess.Namespace,
            "last_activity", sess.LastActivityAt,
            "ttl", sess.TTL)
        if err := c.CleanupSession(ctx, sess.ID, sess.Namespace); err != nil {
            c.Logger.Error("GC: cleanup failed", "session_id", sess.ID, "error", err)
        }
    }
}

// Start runs RunGC on a ticker until ctx is cancelled.
func (c *Cleaner) Start(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            c.RunGC(ctx)
        case <-ctx.Done():
            return
        }
    }
}
```

`CleanupSession` is used by both the `unregister` MCP tool handler and GC. The MCP
tool imports it from `sessiongc` to avoid duplicating the logic.

### `cmd/apiserver/main.go`

After startup, start the GC goroutine if TTL is configured:

```go
ctx := context.Background()
cleaner := &sessiongc.Cleaner{
    Client:   k8sClient,
    Store:    store,
    Sessions: sessions,
    Logger:   logger,
}
if cfg.SessionTTL > 0 {
    gcInterval := cfg.SessionGCInterval
    if gcInterval == 0 {
        gcInterval = time.Hour
    }
    go cleaner.Start(ctx, gcInterval)
}
```

### `internal/config/config.go`

```go
SessionTTL        time.Duration `mapstructure:"session_ttl"`
SessionGCInterval time.Duration `mapstructure:"session_gc_interval"`
```

Defaults:
```go
v.SetDefault("session_ttl", 0)
v.SetDefault("session_gc_interval", time.Hour)
```

Env vars: `IAF_SESSION_TTL` (e.g., `"24h"`), `IAF_SESSION_GC_INTERVAL` (e.g., `"1h"`).

Note: viper's `GetDuration` parses duration strings. Confirm that `mapstructure`
correctly maps these to `time.Duration` fields during `v.Unmarshal(cfg)`. If not, use
`v.GetDuration("session_ttl")` and assign manually after unmarshal.

### RBAC

`unregister` and the GC both call `client.Delete` on a `Namespace`. Add:

```
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create;get;list;delete
```

Replace the existing `create;get;list` marker for namespaces with `create;get;list;delete`.

Run `make generate` after this change.

---

## Data / API Changes

**MCP tool changes:**
- New tool `unregister` (input: `session_id`; output: summary with `status`, `namespace`, `deletedApps`, `deletedAppCount`, `message`)
- `register` response gains optional `ttl_seconds` and `expires_after` fields when TTL is configured

**`Session` JSON schema change**: new `last_activity_at` and `ttl` fields in
`sessions.json`. These are additive — old sessions loaded from disk will have zero
values for these fields, meaning they will never expire (TTL=0 → no expiry). This is
the correct backwards-compatible behaviour.

---

## Multi-tenancy & Shared Resource Impact

`unregister` deletes the session namespace, which removes all K8s resources including
Deployments, Services, IngressRoutes, Application CRs, and kpack Images in that
namespace. This is scoped entirely to the session's own namespace. No other tenant is
affected.

The GC goroutine only deletes sessions whose TTL has elapsed. Concurrent GC runs are
safe because `ListExpired` takes a read lock and `Delete` takes a write lock; between
them, another goroutine might delete the session, but `CleanupSession` handles
`IsNotFound` for the namespace deletion.

---

## Security Considerations

- `unregister` validates `session_id` via `Lookup` before taking any destructive action.
  Session IDs are 128-bit random values — not guessable by another tenant.
- Namespace deletion is logged at `WARN` level with session ID, namespace, and app
  count before execution.
- GC logs at `WARN` for each session deleted, providing an audit trail.
- The `namespaces: delete` RBAC verb is additive to the existing marker. Document that
  this grants the platform service account the ability to delete any namespace (not
  restricted to IAF-managed namespaces — Kubernetes RBAC does not support label-based
  restriction on delete). Operators should review this RBAC grant before deploying in
  shared clusters.

---

## Resource & Performance Impact

- `Touch` writes to disk on every tool call. With file-based persistence this is a
  single write syscall. If this becomes a bottleneck (high tool call volume), a
  write-back cache (persist every 60s) can be added later.
- GC runs at most once per `IAF_SESSION_GC_INTERVAL` (default 1h). Each GC pass reads
  the session map and makes at most one namespace deletion per expired session.

---

## Migration / Compatibility

Existing sessions loaded from disk will have `last_activity_at: zero` and `ttl: 0`.
`ListExpired` returns nothing for TTL=0 sessions. If an operator sets `IAF_SESSION_TTL`
for the first time on an existing deployment, existing sessions will start accumulating
TTL from their first tool call (which sets `last_activity_at` via `Touch`). Sessions
never touched after TTL is enabled will have `last_activity_at: zero` and will appear
to have been idle for an extremely long time — they will be GC'd on the next tick.
Mitigation: document that setting `IAF_SESSION_TTL` for the first time on an existing
deployment will GC all sessions that have been idle since before the TTL was set.

---

## Open Questions (resolved)

**Q1 — GC grace period?** No grace period. Immediate deletion on TTL expiry. A
crash-looping pod stays in its namespace; the GC doesn't know or care — it deletes the
namespace regardless of pod state.

**Q2 — `unregister` without active connection?** Yes, it works. The `session_id` is a
durable identifier stored on disk. An agent can call `unregister` on a session created
in a previous process run.

---

## Open Questions for Developer

1. Confirm that `mapstructure` correctly converts env var duration strings (`"24h"`)
   to `time.Duration` fields during `v.Unmarshal`. If not, use `v.GetDuration()`
   explicitly after unmarshal.

2. The `unregister` MCP tool currently uses `deps.Client.Delete(namespace)` which is
   the same client as all other tools. Confirm this client has the `namespaces: delete`
   RBAC grant before closing this issue (see RBAC section above).

3. Consider whether the `Logger` field should be added to `Dependencies` or passed
   via the context (using `ctrl.Log.FromContext`). The MCP tools currently don't have
   a logger in deps — the unregister tool is the first to need one for the WARN log.
   Simplest: `log/slog.Default()` inside the tool handler (no dep change needed for MVP).

4. The `sessiongc` package is used by `cmd/apiserver`. Confirm whether `cmd/mcpserver`
   also needs the GC goroutine (it would, if agents use the standalone stdio server).
   Recommend: move GC startup into a shared function callable from both binaries.
