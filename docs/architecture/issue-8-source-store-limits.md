# Technical Design: Source Store Upload Limits (#8)

**Security review flag: this issue also carries `needs-security-review`.**

---

## Approach

Add a `Limits` struct to the `sourcestore` package and enforce limits at the storage
layer (`StoreFiles` + `StoreTarball`). This is the single authoritative enforcement
point regardless of whether the upload arrives via the MCP `push_code` tool or the REST
tarball endpoint. The REST handler adds a complementary `http.MaxBytesReader` as an
HTTP-layer guard, preventing large payloads from being fully buffered into the server
before the store layer can reject them.

Path traversal hardening (from the #14 design review) is included here since both
changes touch `StoreFiles`. The developer should coordinate with the #14 implementation
to avoid merge conflicts.

---

## Changes Required

### `internal/config/config.go`

Add three new fields to `Config`:

```go
// Source store upload limits
SourceMaxFiles     int   `mapstructure:"source_max_files"`
SourceMaxFileSize  int64 `mapstructure:"source_max_file_size"`
SourceMaxTotalSize int64 `mapstructure:"source_max_total_size"`
```

Add defaults in `Load()`:

```go
v.SetDefault("source_max_files", 500)
v.SetDefault("source_max_file_size", 1*1024*1024)   // 1 MiB
v.SetDefault("source_max_total_size", 50*1024*1024)  // 50 MiB
```

Env vars: `IAF_SOURCE_MAX_FILES`, `IAF_SOURCE_MAX_FILE_SIZE`, `IAF_SOURCE_MAX_TOTAL_SIZE`.

### `internal/sourcestore/store.go`

**1. New `Limits` struct:**

```go
// Limits configures upload size constraints for the Store.
// Zero values disable the corresponding limit.
type Limits struct {
    MaxFiles      int   // maximum number of files per upload
    MaxFileBytes  int64 // maximum size of a single file in bytes
    MaxTotalBytes int64 // maximum total upload size in bytes (StoreFiles and StoreTarball)
}
```

**2. Change `Store` struct:**

```go
type Store struct {
    dir     string
    baseURL string
    logger  *slog.Logger
    limits  Limits
}
```

**3. Change `New()` signature:**

```go
func New(dir, baseURL string, logger *slog.Logger, limits Limits) (*Store, error)
```

Store `limits` in the struct. Update all callers.

**4. Add getter for HTTP-layer use:**

```go
func (s *Store) MaxTotalBytes() int64 { return s.limits.MaxTotalBytes }
```

**5. Update `StoreFiles` with limit checks (insert before the tar loop):**

```go
if s.limits.MaxFiles > 0 && len(files) > s.limits.MaxFiles {
    return "", fmt.Errorf("file count %d exceeds maximum of %d; reduce the number of files", len(files), s.limits.MaxFiles)
}

var totalBytes int64
for path, content := range files {
    size := int64(len(content))
    if s.limits.MaxFileBytes > 0 && size > s.limits.MaxFileBytes {
        return "", fmt.Errorf("file %q size %d bytes exceeds per-file maximum of %d bytes", path, size, s.limits.MaxFileBytes)
    }
    totalBytes += size
    if s.limits.MaxTotalBytes > 0 && totalBytes > s.limits.MaxTotalBytes {
        return "", fmt.Errorf("total upload size exceeds maximum of %d bytes (%d MiB)", s.limits.MaxTotalBytes, s.limits.MaxTotalBytes>>20)
    }
}
```

Check total size inside the loop so we short-circuit before accumulating further. The
per-file check happens before adding to the total so both limits report clearly.

**6. Harden path traversal check in `StoreFiles`** (replaces current `strings.HasPrefix` check):

```go
cleanPath := filepath.Clean(path)
fullPath := filepath.Join(appDir, cleanPath)
if !strings.HasPrefix(filepath.Clean(fullPath)+string(filepath.Separator), filepath.Clean(appDir)+string(filepath.Separator)) {
    return "", fmt.Errorf("invalid file path %q: must not escape upload directory", path)
}
```

This handles `..secret` filenames correctly (they are allowed) while still blocking
actual traversal. The existing test `TestStoreFiles_PathTraversal` should continue to
pass.

**7. Update `StoreTarball` with LimitReader + size detection:**

```go
func (s *Store) StoreTarball(namespace, appName string, r io.Reader) (string, error) {
    appDir := filepath.Join(s.dir, namespace, appName)
    if err := os.MkdirAll(appDir, 0o755); err != nil {
        return "", fmt.Errorf("creating app source directory: %w", err)
    }

    tarballPath := filepath.Join(appDir, "source.tar.gz")
    f, err := os.Create(tarballPath)
    if err != nil {
        return "", fmt.Errorf("creating tarball file: %w", err)
    }
    defer f.Close()

    reader := io.Reader(r)
    if s.limits.MaxTotalBytes > 0 {
        // Read one byte beyond the limit to detect overflow without buffering.
        reader = io.LimitReader(r, s.limits.MaxTotalBytes+1)
    }

    n, err := io.Copy(f, reader)
    if err != nil {
        _ = os.Remove(tarballPath)
        return "", fmt.Errorf("writing tarball: %w", err)
    }
    if s.limits.MaxTotalBytes > 0 && n > s.limits.MaxTotalBytes {
        _ = os.Remove(tarballPath)
        return "", fmt.Errorf("upload size exceeds maximum of %d bytes (%d MiB)", s.limits.MaxTotalBytes, s.limits.MaxTotalBytes>>20)
    }

    // ... rest unchanged
}
```

Key: `io.LimitReader(r, maxTotalBytes+1)` allows reading exactly one byte past the
limit, making it possible to detect overflow via `n > maxTotalBytes`. The partial file
is deleted before returning the error.

### `internal/api/handlers/applications.go`

**1. Add `maxUploadBytes int64` to `ApplicationHandler`:**

```go
type ApplicationHandler struct {
    client         client.Client
    sessions       *auth.SessionStore
    store          *sourcestore.Store
    maxUploadBytes int64
}
```

Pass it from `NewApplicationHandler(c, sessions, store, maxUploadBytes)`.

**2. Apply `http.MaxBytesReader` at the start of `UploadSource`** (before any body reads):

```go
func (h *ApplicationHandler) UploadSource(c echo.Context) error {
    if h.maxUploadBytes > 0 {
        c.Request().Body = http.MaxBytesReader(
            c.Response().Writer,
            c.Request().Body,
            h.maxUploadBytes,
        )
    }
    // ... existing namespace resolution, app lookup, etc.
```

When `http.MaxBytesReader` detects an oversize body, subsequent reads return an error
with `(*http.MaxBytesError).Limit` set. The error surfaces through `c.Bind` (for JSON
uploads) or `io.Copy` inside `StoreTarball`. Catch it in `UploadSource` and return HTTP
413 (`StatusRequestEntityTooLarge`) instead of 500:

```go
blobURL, err = h.store.StoreTarball(namespace, name, c.Request().Body)
if err != nil {
    var maxBytesErr *http.MaxBytesError
    if errors.As(err, &maxBytesErr) {
        return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
    }
    return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
```

Do the same for the JSON path (errors from `StoreFiles` should return 400, not 500):

```go
blobURL, err = h.store.StoreFiles(namespace, name, req.Files)
if err != nil {
    return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
}
```

### `internal/api/routes.go`

Update `NewApplicationHandler` call:

```go
apps := handlers.NewApplicationHandler(c, sessions, store, cfg.SourceMaxTotalSize)
```

`routes.go` currently takes only `c, cs, sessions, store` — add `cfg *config.Config`
(or just `maxUploadBytes int64`) as a parameter. Choose the simpler option: pass
`maxUploadBytes int64` directly to avoid importing config into the routes package.

### `cmd/apiserver/main.go` and `cmd/mcpserver/main.go`

Update calls to `sourcestore.New(...)` to pass `sourcestore.Limits{...}` from `cfg`:

```go
limits := sourcestore.Limits{
    MaxFiles:      cfg.SourceMaxFiles,
    MaxFileBytes:  cfg.SourceMaxFileSize,
    MaxTotalBytes: cfg.SourceMaxTotalSize,
}
store, err := sourcestore.New(cfg.SourceStoreDir, cfg.SourceStoreURL, logger, limits)
```

Update `api.RegisterRoutes(...)` call in `cmd/apiserver/main.go` to pass
`cfg.SourceMaxTotalSize`.

---

## Data / API Changes

No CRD changes. No new MCP tool parameters.

Error responses change for oversized uploads:
- MCP `push_code`: tool error text includes the limit and count/size that was exceeded
  (machine-actionable: `"file count 501 exceeds maximum of 500; reduce the number of files"`)
- REST tarball endpoint: HTTP 413 instead of HTTP 500 for oversized tarballs
- REST JSON endpoint: HTTP 400 instead of HTTP 500 for over-limit file maps

The `iaf://platform` resource and `deploy-guide` prompt should be updated to document
the default limits.

---

## Multi-tenancy & Shared Resource Impact

This change directly protects the shared `iaf-sources` PVC. A single tenant uploading
a 10 GiB tarball today would exhaust the PVC for all sessions. After this change, the
largest possible upload is `IAF_SOURCE_MAX_TOTAL_SIZE` (default 50 MiB) and the HTTP
body is capped at the same value by `MaxBytesReader`.

The store layer's `io.LimitReader(r, maxTotalBytes+1)` ensures the partial file is
written to disk (at most `maxTotalBytes+1` bytes) and then deleted on limit detection.
Peak disk usage per rejected upload is at most `maxTotalBytes+1` bytes, after which the
file is removed. No permanent disk impact from rejected uploads.

---

## Security Considerations

**Path traversal**: The new join-and-confirm check in `StoreFiles` is stronger than the
current prefix check and handles edge cases like filenames starting with `..` that are
valid names, not traversals.

**Defense in depth**: `http.MaxBytesReader` prevents the server from receiving the
full body before the store layer can reject it. For a 10 GiB upload, this means at most
`IAF_SOURCE_MAX_TOTAL_SIZE` bytes are read from the network — the connection is closed
after the limit is hit.

**No buffering of oversized content**: `io.LimitReader` + file deletion ensures that
oversized tarballs are not written to disk beyond the limit. The `+1` byte trick ensures
overflow is detected without over-reading.

**Limit bypass**: All limits are enforced server-side. An agent cannot bypass them by
setting a content-length header — `http.MaxBytesReader` and `io.LimitReader` enforce
actual bytes read, not declared content length.

**`needs-security-review` items:**
1. Verify that `http.MaxBytesReader` closes the connection after the limit is hit (it
   does, via the response writer's hijack mechanism) so large uploads don't tie up a
   goroutine indefinitely.
2. The `StoreFiles` total-size check is performed during iteration and stops at the
   first file that causes the total to exceed the limit. This means some files are
   processed before the error is returned. No data is written to disk at this point
   (the check happens before the tar loop), so there is no partial write risk.
3. Confirm the `+1` byte in `io.LimitReader(r, maxTotalBytes+1)` does not cause an
   off-by-one error on an exactly `maxTotalBytes`-sized upload. With `limit =
   maxTotalBytes+1`, an upload of exactly `maxTotalBytes` bytes reads `maxTotalBytes`
   bytes, `n = maxTotalBytes`, and `n > maxTotalBytes` is false — correctly accepted.

---

## Resource & Performance Impact

- No memory allocation change — files are already streamed to disk.
- One extra pass over `files` map keys for count + size checks (O(n) in file count,
  done in a single loop, before any tar writing).
- `io.LimitReader` adds a single integer comparison per `Read` call — negligible.

---

## Migration / Compatibility

Agents that currently upload more than 500 files or 50 MiB of total content (rare in
practice but possible) will start receiving errors. The error messages are
machine-actionable: they state the limit and suggest what to reduce.

Existing `sourcestore.New(dir, url, logger)` callers must be updated to pass a `Limits`
struct. All callers are internal (`cmd/apiserver`, `cmd/mcpserver`, tests) — no external
API breakage.

Test files that call `New` with 3 args must be updated to pass `Limits{}` (zero values =
no limits enforced, preserving existing test behaviour).

---

## Open Questions (resolved)

**Q1 — Per-session storage quota vs per-upload?** Per-upload only (this issue). Per-session
cumulative storage quota is deferred to the resource quotas issue (#7), which will
address PVC-level accounting via `ResourceQuota`.

**Q2 — Reject binary file content?** No. `StoreFiles` accepts any byte sequence as file
content. Binary files are valid source inputs (e.g., pre-compiled assets). Restricting
to UTF-8 would break legitimate use cases.

---

## Open Questions for Developer

1. Coordinate with the #14 implementation: both issues touch the path traversal check in
   `StoreFiles`. Recommend: implement #8 and #14 on the same feature branch, or at
   minimum ensure one merges before the other to avoid conflicts.

2. The `RegisterRoutes` function signature currently takes `store *sourcestore.Store`
   but not the upload limit. Decide whether to pass `cfg.SourceMaxTotalSize` as a
   separate `int64` parameter to `RegisterRoutes`, or extract it from a getter on
   `Store`. The getter approach (`store.MaxTotalBytes()`) keeps `RegisterRoutes`
   signature stable — recommended.

3. For the JSON upload path, `c.Bind(&req)` reads the full body before `StoreFiles` is
   called. Adding `http.MaxBytesReader` wrapping before `c.Bind` will cause large JSON
   bodies to fail at bind time with an `http.MaxBytesError`. Check that this error is
   caught and returns HTTP 400/413 rather than 500. The bind error message may need to
   be normalised for agents.
