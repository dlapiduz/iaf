# Architecture: Standard UI Scaffold Templates via MCP (Issue #3)

## Technical Design

### Approach

Add an `iaf://scaffold/{framework}` MCP resource template and a `scaffold-guide` MCP prompt. All scaffold template files are embedded into the binary at compile time using Go's `//go:embed` directive, stored under `internal/mcp/resources/scaffolds/{framework}/`. The resource handler extracts the `{framework}` value from the URI, validates it against an allowlist (`nextjs`, `html`), and walks the embedded filesystem to build a `{"relative/path": "file_content"}` JSON map that agents can pass directly to `push_code`. The filesystem walk — not the URI value — determines which files are read, so there is no path traversal risk from agent-supplied input. The `scaffold-guide` prompt explains when to use the scaffold and how to extend it. Both `language-guide` (Node.js section) and `scaffold-guide` cross-reference each other.

### Changes Required

- **New `internal/mcp/resources/scaffolds/nextjs/`** — embedded Next.js scaffold files:
  - `package.json` — Next.js 14 project; `start` script is `next start`; deps: `next`, `react`, `react-dom`, `tailwindcss`
  - `pages/index.js` — minimal home page using layout component
  - `pages/api/health.js` — Next.js API route returning `{ status: "ok" }` with HTTP 200
  - `pages/_app.js` — wraps all pages with the layout and imports global CSS
  - `styles/globals.css` — Tailwind base/components/utilities directives + org design tokens as CSS custom properties
  - `tailwind.config.js` — colour palette, font family, spacing tokens; points to `pages/**` and `components/**` for PurgeCSS
  - `postcss.config.js` — standard Tailwind postcss config
  - `components/Layout.js` — navigation shell with org-branded header/footer
  - `README.md` — explains how to add pages, extend design tokens, run locally

- **New `internal/mcp/resources/scaffolds/html/`** — embedded plain-HTML + Tailwind via CDN scaffold:
  - `package.json` — minimal; deps: `express`; `start` script is `node server.js`
  - `server.js` — Express app: `express.static('public')` + explicit `GET /health` route returning 200 `{ status: "ok" }`; listens on `process.env.PORT || 8080`
  - `public/index.html` — HTML5 page with Tailwind CDN `<script>` tag, navigation shell, main content area, org design tokens in `<style>` block as CSS custom properties
  - `public/styles.css` — org design tokens for use alongside Tailwind CDN
  - `README.md` — explains structure, how to add pages, how to migrate from CDN to build step

- **New `internal/mcp/resources/scaffold.go`** — `RegisterScaffoldResource(server, deps)`:
  - Uses `//go:embed scaffolds/**` on an `embed.FS` var
  - Registers `iaf://scaffold/{framework}` as a `ResourceTemplate`
  - Handler: extract framework from URI (`strings.TrimPrefix`), validate against `allowedFrameworks = []string{"nextjs","html"}`, then `fs.WalkDir(embedFS, "scaffolds/"+framework, ...)` to collect `{relative_path: content}` entries, marshal to JSON
  - Returns `ResourceNotFoundError` for unknown frameworks
  - Never passes the raw URI value to `embedFS.Open`; always constructs the path as `"scaffolds/" + allowlistedFramework + "/" + walkPath`

- **New `internal/mcp/prompts/scaffold_guide.go`** — `RegisterScaffoldGuide(server, deps)`:
  - Prompt `scaffold-guide` with optional `framework` argument (`nextjs`, `html`)
  - Returns Markdown guide explaining: when to start from a scaffold vs from scratch, how to fetch the scaffold (read `iaf://scaffold/{framework}`), how to pass the file map to `push_code`, how to add pages, and how to stay within the design system
  - If `framework` omitted, lists both frameworks

- **`internal/mcp/resources/resources_test.go`** — new tests:
  - `TestScaffoldResource_NextJS`: resource returns JSON, contains keys `package.json`, `pages/index.js`, `pages/api/health.js`, `styles/globals.css`
  - `TestScaffoldResource_HTML`: resource returns JSON, contains keys `package.json`, `server.js`, `public/index.html`
  - `TestScaffoldResource_Unknown`: `iaf://scaffold/react-native` returns error

- **`internal/mcp/prompts/prompts_test.go`** — new tests:
  - `TestScaffoldGuide_WithFramework`: prompt returns non-empty text containing framework name
  - `TestScaffoldGuide_NoFramework`: prompt lists both frameworks

- **`internal/mcp/server.go`** — call `RegisterScaffoldResource` and `RegisterScaffoldGuide`; update `serverInstructions` to mention scaffold resources

- **`internal/mcp/prompts/language_guide.go`** — add note in Node.js `bestPractices` referencing `iaf://scaffold/nextjs` for frontend apps

- **`internal/mcp/resources/resources_test.go`** — update `TestListResourceTemplates` count from 1 to 2 (adds `scaffold`)

### Data / API Changes

**New MCP resource template**: `iaf://scaffold/{framework}`
- MIME type: `application/json`
- Supported `{framework}` values: `nextjs`, `html`
- Response shape:
```json
{
  "package.json": "{\n  \"name\": \"myapp\", ...\n}",
  "pages/index.js": "import Layout from '../components/Layout';\n...",
  "pages/api/health.js": "export default function handler(req, res) { res.status(200).json({ status: 'ok' }); }",
  "styles/globals.css": "@tailwind base;\n@tailwind components;\n...",
  "tailwind.config.js": "module.exports = { content: [...], theme: { extend: { colors: {...} } } }",
  "postcss.config.js": "module.exports = { plugins: { tailwindcss: {}, autoprefixer: {} } }",
  "components/Layout.js": "...",
  "README.md": "# IAF Next.js Scaffold\n..."
}
```

**New MCP prompt**: `scaffold-guide`, optional argument `framework`.

**No new env vars, no CRD changes, no new K8s objects.**

### Multi-tenancy & Shared Resource Impact

Scaffolds are static, embedded, and read-only. Every session sees the same scaffold content. No shared infrastructure is touched by reading a scaffold resource. No K8s objects are created until the agent calls `push_code` with the scaffold files.

### Security Considerations

- **Embedded content**: templates compiled into binary at build time; agents cannot inject or modify content. Template content **must be reviewed** before merge to ensure:
  - No hardcoded secrets, API keys, or tokens
  - No disabled CORS (`Access-Control-Allow-Origin: *` acceptable for a scaffold; `credentials: true` with wildcard is not)
  - No `eval()`, `new Function()`, or equivalent dynamic code execution patterns
  - No development-only configurations shipped as defaults (e.g., debug flags enabled)
- **URI-to-path mapping**: the `{framework}` value extracted from the URI is compared against `allowedFrameworks` (a compile-time slice). The embed FS is then accessed using `"scaffolds/" + validatedFramework + "/"` — never the raw URI segment. This ensures no path traversal via `../` or encoded sequences.
- **File size**: scaffold files are small (< 50 KB total per framework). No size limit needed for reads, but ensure CI review catches unusually large template additions.
- **No `needs-security-review`** — beyond template content review in the PR itself.

### Resource & Performance Impact

- Binary size increase: ~50–100 KB for both scaffolds (all template files combined). Acceptable.
- All reads are in-memory `embed.FS` lookups — effectively zero latency.
- `fs.WalkDir` over the embedded subtree runs once per resource request; with ~10 files per scaffold this is microseconds.

### Migration / Compatibility

Purely additive. `TestListResources` count in the existing test must be updated from 2 static resources to 2 (static count unchanged) and `TestListResourceTemplates` from 1 to 2 templates. No existing API contracts change.

### Open Questions for Developer

1. **`//go:embed` and nested dirs**: `//go:embed scaffolds/**` does not recursively include hidden files (dot-files) or directories named with dots. If any scaffold file starts with `.` (e.g., `.eslintrc`), use `//go:embed all:scaffolds` instead. Verify this at implementation time.
2. **`fs.WalkDir` path stripping**: `fs.WalkDir(embedFS, "scaffolds/nextjs", ...)` will produce paths like `scaffolds/nextjs/package.json`. Strip the `scaffolds/nextjs/` prefix when building the output map so the agent receives `package.json`, not `scaffolds/nextjs/package.json`.
3. **Design token values**: use a neutral org color palette (e.g., Tailwind `slate` + a single brand accent in `blue-600`). Do not brand with any specific org identity in the default templates — operators can override by updating the template files.
4. **Should the HTML scaffold use a CDN or a build step?** Decision: CDN only (no build step) for the `html` framework. The Next.js scaffold uses a full build step. This keeps the two scaffolds clearly differentiated by complexity.
5. **`pages/api/health.js` content**: must return HTTP 200 even when no other pages load, so the Kubernetes readiness probe passes immediately after deployment. Use `res.status(200).json({ status: 'ok' })` — no database dependency in the health route.
