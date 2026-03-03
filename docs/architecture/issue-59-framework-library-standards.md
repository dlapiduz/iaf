# Architecture: Coach — Framework & Library Standards (Issue #59)

## Approach

Extend `internal/orgstandards` to add structured framework and library fields to
`PerLanguageStandards`. The existing `ApprovedLibraries map[string]string` is replaced
with `[]LibraryStandard`. Update `platformDefaults()` with sensible per-language data.
The `coding-guide` prompt (now in `internal/mcp/coach/prompts/` after Issue #56) renders
approved and prohibited sections separately.

This issue depends on Issue #56 (coach server) because the coding-guide prompt moves
to the coach package. However, the `internal/orgstandards` struct changes are
independent and can land first.

## Changes Required

### Modified files

- `internal/orgstandards/loader.go` — add `FrameworkStandard`, `LibraryStandard`
  types; update `PerLanguageStandards`; update `platformDefaults()`
- `internal/mcp/coach/prompts/coding_guide.go` (after #56) or
  `internal/mcp/prompts/coding_guide.go` (before #56 lands) — update prompt rendering
  to include approved/prohibited framework sections
- `internal/mcp/resources/org_standards.go` (or coach equivalent after #56) — no
  code changes needed; the JSON marshaling of the updated struct is automatic
- `docs/operator-guide.md` — add migration note for operators using the old
  `approvedLibraries: {category: "lib"}` map format

## Data / API Changes

### New types in `internal/orgstandards/loader.go`

```go
// FrameworkStandard describes an approved or prohibited application framework.
type FrameworkStandard struct {
    Name       string `json:"name"                 yaml:"name"`
    MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
    Notes      string `json:"notes,omitempty"      yaml:"notes,omitempty"`
    Reason     string `json:"reason,omitempty"     yaml:"reason,omitempty"` // used for prohibited
}

// LibraryStandard describes an approved or prohibited library dependency.
type LibraryStandard struct {
    Name       string `json:"name"                 yaml:"name"`
    MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
    Notes      string `json:"notes,omitempty"      yaml:"notes,omitempty"`
    Category   string `json:"category,omitempty"   yaml:"category,omitempty"` // e.g. "web", "orm", "logging"
    Reason     string `json:"reason,omitempty"     yaml:"reason,omitempty"` // used for prohibited
}
```

### Updated `PerLanguageStandards`

```go
type PerLanguageStandards struct {
    Notes               []string          `json:"notes"               yaml:"notes"`
    ApprovedFrameworks  []FrameworkStandard `json:"approvedFrameworks,omitempty"  yaml:"approvedFrameworks,omitempty"`
    ProhibitedFrameworks []FrameworkStandard `json:"prohibitedFrameworks,omitempty" yaml:"prohibitedFrameworks,omitempty"`
    ApprovedLibraries  []LibraryStandard  `json:"approvedLibraries,omitempty"  yaml:"approvedLibraries,omitempty"`
    ProhibitedLibraries []LibraryStandard `json:"prohibitedLibraries,omitempty" yaml:"prohibitedLibraries,omitempty"`
}
```

**Breaking change**: `ApprovedLibraries` changes from `map[string]string` to
`[]LibraryStandard`. Any operator-supplied YAML/JSON using the old map format will
fail to parse and silently fall back to platform defaults. Document the migration.

### Updated `platformDefaults()` (representative excerpt)

```go
// Java
"java": {
    Notes: []string{"Use Gradle or Maven wrapper scripts"},
    ApprovedFrameworks: []FrameworkStandard{
        {Name: "Spring Boot", MinVersion: "3.2", Notes: "Preferred framework for REST APIs"},
        {Name: "Quarkus", Notes: "Approved for teams already using it; prefer Spring Boot for new projects"},
    },
    ProhibitedFrameworks: []FrameworkStandard{
        {Name: "Spring MVC (standalone)", Reason: "Use Spring Boot instead — it handles configuration automatically"},
    },
    ApprovedLibraries: []LibraryStandard{
        {Name: "spring-boot-starter-web", Category: "web", MinVersion: "3.2"},
        {Name: "logstash-logback-encoder", Category: "logging", MinVersion: "7.4"},
    },
},
// Ruby
"ruby": {
    Notes: []string{"Include a Gemfile.lock"},
    ApprovedFrameworks: []FrameworkStandard{
        {Name: "Rails", MinVersion: "7.1"},
        {Name: "Sinatra", Notes: "Approved for lightweight APIs"},
    },
    ApprovedLibraries: []LibraryStandard{
        {Name: "semantic_logger", Category: "logging"},
    },
},
// Python
"python": {
    Notes: []string{"Use requirements.txt or pyproject.toml", "Do not use root user"},
    ApprovedFrameworks: []FrameworkStandard{
        {Name: "FastAPI", MinVersion: "0.100"},
        {Name: "Flask"},
        {Name: "Django"},
    },
    ApprovedLibraries: []LibraryStandard{
        {Name: "python-json-logger", Category: "logging"},
    },
},
// Node.js
"nodejs": {
    Notes: []string{"Pin exact versions in package-lock.json", "Use node:lts-alpine base"},
    ApprovedFrameworks: []FrameworkStandard{
        {Name: "Express", MinVersion: "4.x"},
        {Name: "Fastify"},
        {Name: "(none)", Notes: "Approved for simple CLI tools or scripts"},
    },
    ApprovedLibraries: []LibraryStandard{
        {Name: "pino", Category: "logging"},
        {Name: "next.js", Category: "frontend"},
    },
},
// Go
"go": {
    Notes: []string{"Use Go modules", "Run as non-root"},
    ApprovedFrameworks: []FrameworkStandard{
        {Name: "net/http (stdlib)"},
        {Name: "Gin"},
        {Name: "(none)", Notes: "Go stdlib is sufficient for most services"},
    },
    ApprovedLibraries: []LibraryStandard{
        {Name: "log/slog", Category: "logging", Notes: "Stdlib, Go 1.21+"},
    },
},
```

### Version string validation

When loading operator config, after unmarshaling, validate all version strings:

```go
// validateVersionString returns an error if s contains characters outside
// the safe set for display in build manifests.
var validVersionRe = regexp.MustCompile(`^[0-9a-zA-Z.\-+*x]+$`)

func validateVersionString(s string) error {
    if s == "" {
        return nil
    }
    if !validVersionRe.MatchString(s) {
        return fmt.Errorf("version string %q contains invalid characters", s)
    }
    return nil
}
```

Apply this check in `loader.go` after unmarshaling, iterating over all
`ApprovedFrameworks`, `ProhibitedFrameworks`, `ApprovedLibraries`, `ProhibitedLibraries`
in all languages. On validation failure, log a warning and retain the previous value
(existing behavior for parse errors).

### `coding-guide` prompt rendering update

Add two new sections after the existing best-practices section:

```
## Approved Frameworks
- Spring Boot >= 3.2 (Java) — preferred for REST APIs
- ...

## Prohibited Frameworks
- Spring MVC standalone (Java) — use Spring Boot instead
- ...

## Approved Libraries (by category)
### web
- ...
### logging
- ...

## Prohibited Libraries
- ...
```

Render only sections that have content. If approved/prohibited lists are empty (e.g. Go),
omit that section entirely.

## Multi-tenancy & Shared Resource Impact

No K8s impact. Org standards are a single shared configuration served to all agents.
No per-tenant standards in this design.

## Security Considerations

- **Version string injection**: validated (see above). Version strings must not contain
  shell metacharacters since agents may copy them verbatim into `pom.xml`, `Gemfile`,
  `go.mod`, etc.
- **No executable content**: all framework/library fields are display-only strings.
  They are rendered into prompt text but never evaluated.
- **Operator file validation**: run the version validation on load, not just on render.
  An operator who supplies a bad config file gets a clear log warning.

## Resource & Performance Impact

Negligible. Standards are loaded once at startup and cached in memory.

## Migration / Compatibility

**Breaking**: `ApprovedLibraries` format changes. Operators using a custom YAML/JSON
config with the old `map[string]string` format must migrate to the new list format.

Migration guide (for `docs/operator-guide.md`):

```yaml
# Before (old format — no longer supported)
perLanguage:
  java:
    approvedLibraries:
      web: "spring-boot"
      logging: "logback"

# After (new format)
perLanguage:
  java:
    approvedLibraries:
      - name: spring-boot-starter-web
        category: web
        minVersion: "3.2"
      - name: logstash-logback-encoder
        category: logging
        minVersion: "7.4"
```

## Open Questions for Developer

- Should version strings be semver-parsed for display ordering (e.g. show 3.2 before 3.1)?
  Issue recommendation: display-only for now; don't enforce or sort.
- Confirm whether the `coding-guide` prompt rendering should group approved libraries by
  `category` (recommended) or list them flat.
