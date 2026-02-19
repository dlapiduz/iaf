# Architecture: Agent Coding Guidance — Org-Level Standards via MCP (Issue #1)

## Technical Design

### Approach

Add an `iaf://org/coding-standards` MCP resource and a `coding-guide` MCP prompt. Standards are loaded from a YAML/JSON config file whose path is supplied via `IAF_ORG_STANDARDS_FILE`. A new `internal/orgstandards` package owns a `Loader` type: it reads the file on startup, serves the result through a goroutine-safe `Get()` method, and watches the file with `fsnotify` to hot-reload on change without a pod restart. If the env var is unset or the file does not exist, the loader returns compile-time platform defaults. `Dependencies` gains an `OrgStandards *orgstandards.Loader` field so both the resource and prompt read from the same live value. The `deploy-guide` prompt is updated to advertise the new resource URI and prompt name so agents can discover them.

### Changes Required

- **New `internal/orgstandards/loader.go`** — `OrgStandards` struct (all fields below), `Loader` type with `New(path string) *Loader`, `Start(ctx context.Context)` (starts fsnotify goroutine), `Stop()`, and `Get() *OrgStandards`; `Get` uses `sync.RWMutex` so readers never block writers; reload errors log and retain the previous value.
- **New `internal/orgstandards/loader_test.go`** — table-driven tests: load from a valid temp file, load from a missing file (returns defaults), hot-reload (write new content to temp file, assert `Get()` returns updated value within a timeout).
- **New `internal/mcp/resources/org_standards.go`** — `RegisterOrgStandards(server, deps)` registers `iaf://org/coding-standards` as a static MCP resource (not a template); handler calls `deps.OrgStandards.Get()`, marshals to JSON, returns `application/json`.
- **New `internal/mcp/prompts/coding_guide.go`** — `RegisterCodingGuide(server, deps)` registers `coding-guide` prompt with optional `language` argument; handler calls `deps.OrgStandards.Get()`, merges per-language section with global standards, formats as Markdown; if `language` is unrecognised, returns global standards only without error.
- **`internal/mcp/tools/deps.go`** — add `OrgStandards *orgstandards.Loader` field to `Dependencies`.
- **`internal/mcp/server.go`** — call `RegisterOrgStandards` and `RegisterCodingGuide`; update `serverInstructions` constant to mention `coding-guide` prompt and `iaf://org/coding-standards`.
- **`internal/mcp/prompts/deploy_guide.go`** — add a "Coding Standards" section that references `coding-guide` and `iaf://org/coding-standards`.
- **`cmd/mcpserver/main.go`** — read `IAF_ORG_STANDARDS_FILE` from viper config (default `""`), create `orgstandards.New(path)`, call `loader.Start(ctx)`, pass to `deps`.
- **`cmd/apiserver/main.go`** — same as mcpserver (future admin API may expose standards; wire it now).
- **`internal/mcp/resources/resources_test.go`** — test `iaf://org/coding-standards` returns valid JSON with the expected top-level keys; test that `TestListResources` count is updated.
- **`internal/mcp/prompts/prompts_test.go`** — tests: `coding-guide` without `language` returns global standards Markdown; with a valid `language` returns per-language notes merged in; with an unknown `language` returns global standards (no error).

### Data / API Changes

**New MCP resource**: `iaf://org/coding-standards`
- Static (not a template), listed in `resources/list`
- MIME type: `application/json`
- Content structure:
```json
{
  "healthCheckPath": "/health",
  "loggingFormat": "json",
  "defaultPort": 8080,
  "envVarNaming": "UPPER_SNAKE_CASE",
  "requiredEnvVars": [],
  "bestPractices": [
    "Listen on the PORT environment variable",
    "Implement a health-check endpoint",
    "Do not embed secrets in source code"
  ],
  "perLanguage": {
    "go":     { "notes": [], "approvedLibraries": {} },
    "nodejs": { "notes": [], "approvedLibraries": {} },
    "python": { "notes": [], "approvedLibraries": {} },
    "java":   { "notes": [], "approvedLibraries": {} },
    "ruby":   { "notes": [], "approvedLibraries": {} }
  }
}
```

**New MCP prompt**: `coding-guide`, optional argument `language` (same aliases as `language-guide`).

**New env var**: `IAF_ORG_STANDARDS_FILE` — absolute path to YAML or JSON file; empty = use platform defaults.

**Config file format** (YAML preferred for operator readability; loader accepts both YAML and JSON by trying JSON unmarshal first):
```yaml
healthCheckPath: /health
loggingFormat: json
defaultPort: 8080
envVarNaming: UPPER_SNAKE_CASE
requiredEnvVars: []
bestPractices:
  - "Listen on the PORT environment variable"
perLanguage:
  go:
    notes:
      - "Use Go modules (go.mod); GOPATH mode is not supported"
    approvedLibraries:
      web: "net/http or github.com/labstack/echo"
  nodejs:
    notes:
      - "Include package-lock.json for reproducible builds"
```

### Multi-tenancy & Shared Resource Impact

The standards document is read-only and global — all sessions see the same value. No agent can write to it via MCP. The config file is loaded from a path set by the platform operator at pod startup; agents have no influence over the path or content. No shared K8s infrastructure is touched.

### Security Considerations

- **File path**: comes from env var set by the operator, not from agent input. No path traversal risk from agents. The loader should reject paths containing `..` as a defence-in-depth measure.
- **File size limit**: parse only up to 1 MB; reject larger files with a startup error to prevent OOM from malicious YAML (billion-laughs, deeply nested anchors). Use `io.LimitReader` before unmarshaling.
- **Hot-reload error safety**: if the file becomes unreadable or contains invalid YAML, the loader logs the error, retains the previous in-memory value, and does not crash. Fail-safe behaviour.
- **No `needs-security-review`** — standards are read-only, no secrets, no external calls.

### Resource & Performance Impact

- One file read on startup + inotify kernel events only (no polling).
- Zero additional K8s objects.
- `Get()` is a mutex-protected in-memory read — effectively zero latency per prompt/resource call.
- Binary size increase: negligible (no embedded files, just code).

### Migration / Compatibility

Purely additive. No existing tools, prompts, or resources are changed in a breaking way (deploy-guide text is extended, not modified). Existing deployments without `IAF_ORG_STANDARDS_FILE` receive platform defaults unchanged.

### Open Questions for Developer

1. **YAML library**: check `go.sum` for `gopkg.in/yaml.v3` — it is almost certainly a transitive dep via controller-runtime. If so, use it; otherwise use `encoding/json` only and require JSON format.
2. **fsnotify dependency**: add `github.com/fsnotify/fsnotify` if not already present (check go.mod). It is the standard choice for file watching in Go. Viper already uses it internally.
3. **Graceful shutdown**: `loader.Start(ctx)` should use the provided context for shutdown. When the context is cancelled (e.g., on SIGTERM), the watcher goroutine must exit cleanly without a leak.
4. **Should apiserver expose the standards via REST?** Out of scope for this issue, but wiring the loader into the apiserver now makes it trivial to add a `GET /api/v1/org/standards` endpoint later.
