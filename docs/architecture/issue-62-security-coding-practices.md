# Architecture: Coach — Security Coding Practices (Issue #62)

## Approach

Add a `iaf://org/security-standards` resource (file-override, operator-configurable)
and a `security-guide` prompt with an optional `language` argument that provides
language-specific SQL injection prevention and secret handling examples. Cross-reference
from `coding-guide` and `deploy-guide`.

Depends on Issue #56 for the coach package location. The per-language pattern mirrors
`logging-guide` exactly: a map of language-keyed guide structs, a generic overview
returned when no language is specified, and a per-language render function.

This issue is a security control — the content must be accurate and actionable.

## Changes Required

### New files

- `internal/mcp/coach/resources/security_standards.go` — `RegisterSecurityStandards`
- `internal/mcp/coach/resources/defaults/security-standards.json` — embedded default
- `internal/mcp/coach/prompts/security_guide.go` — `RegisterSecurityGuide`

### Modified files

- `internal/mcp/coach/deps.go` — add `SecurityStandardsFile string`
- `internal/mcp/coach/server.go` — register security resource and prompt
- `cmd/coachserver/main.go` — read `COACH_SECURITY_STANDARDS_FILE`
- `internal/mcp/coach/prompts/coding_guide.go` — add cross-reference to `security-guide`
- `internal/mcp/prompts/deploy_guide.go` — add cross-reference to `security-guide`
  (platform prompt, stays in `internal/mcp/prompts/`)
- `internal/mcp/coach/prompts/prompts_test.go` — update `TestListPrompts` count
- `internal/mcp/coach/resources/resources_test.go` — update `TestListResources` count

## Data / API Changes

### `defaults/security-standards.json`

```json
{
  "inputValidation": {
    "rules": [
      "Use parameterized queries or prepared statements for all database operations — never interpolate user input into SQL strings.",
      "Validate all user-supplied values against an explicit allowlist or schema before use.",
      "Never pass user input to eval(), exec(), os.system(), or equivalent dynamic code execution functions.",
      "Validate Content-Type headers before processing request bodies.",
      "Reject inputs that exceed expected length limits at the application boundary."
    ]
  },
  "secretHandling": {
    "rules": [
      "Read all credentials from environment variables — never hardcode secrets in source code.",
      "Never log the value of any environment variable that may contain a credential.",
      "Never commit secrets to source control — use .gitignore for local env files.",
      "Rotate secrets on suspected exposure; do not rely on obscurity.",
      "Reference Kubernetes Secrets via IAF env var injection — never embed secret values in Application spec."
    ]
  },
  "outputEncoding": {
    "rules": [
      "HTML-escape all user-supplied content before rendering in HTML templates.",
      "Use the framework's built-in template engine — do not concatenate strings into HTML.",
      "Always use structured JSON serialization — never concatenate strings into JSON.",
      "Set Content-Type response headers explicitly."
    ]
  },
  "dependencyHygiene": {
    "rules": [
      "Pin exact dependency versions in lock files (go.sum, package-lock.json, Gemfile.lock, etc.).",
      "Enable automated vulnerability scanning (Dependabot, Renovate, or equivalent).",
      "Review dependency licenses against iaf://org/license-policy before adding.",
      "Prefer well-maintained libraries with recent releases and active communities."
    ]
  },
  "owaspReferences": [
    { "id": "A03:2021", "name": "Injection",          "url": "https://owasp.org/Top10/A03_2021-Injection/" },
    { "id": "A02:2021", "name": "Cryptographic Failures", "url": "https://owasp.org/Top10/A02_2021-Cryptographic_Failures/" },
    { "id": "A06:2021", "name": "Vulnerable Components", "url": "https://owasp.org/Top10/A06_2021-Vulnerable_and_Outdated_Components/" },
    { "id": "A07:2021", "name": "Auth Failures",      "url": "https://owasp.org/Top10/A07_2021-Identification_and_Authentication_Failures/" }
  ]
}
```

### `RegisterSecurityStandards`

Follows the exact same pattern as `RegisterLoggingStandards`:

```go
//go:embed defaults/security-standards.json
var defaultSecurityStandards []byte

func RegisterSecurityStandards(server *gomcp.Server, deps *coach.Dependencies) {
    server.AddResource(&gomcp.Resource{
        URI:         "iaf://org/security-standards",
        Name:        "org-security-standards",
        Description: "Organisation secure coding standards — input validation, SQL injection prevention, secret handling, output encoding, dependency hygiene, and OWASP Top 10 references.",
        MIMEType:    "application/json",
    }, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
        content, err := loadStandards(deps.SecurityStandardsFile, defaultSecurityStandards)
        if err != nil {
            return nil, fmt.Errorf("loading security standards: %w", err)
        }
        return &gomcp.ReadResourceResult{
            Contents: []*gomcp.ResourceContents{
                {URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
            },
        }, nil
    })
}
```

### `security-guide` prompt — per-language SQL injection examples

The prompt accepts an optional `language` argument (same pattern as `logging-guide`).
When provided, render language-specific examples. When omitted, render an overview.

#### Per-language SQL injection prevention

```go
type sqlInjectionGuide struct {
    parameterizedExample string
    ormExample           string
    prohibited           string
}

var sqlGuides = map[string]sqlInjectionGuide{
    "go": {
        parameterizedExample: `
// CORRECT: use ? placeholders (database/sql)
row := db.QueryRowContext(ctx, "SELECT id, name FROM users WHERE id = ?", userID)

// CORRECT: pgx named parameters
rows, err := pool.Query(ctx, "SELECT id FROM users WHERE email = $1", email)`,
        prohibited: `
// WRONG: never concatenate user input into SQL
query := "SELECT * FROM users WHERE name = '" + userName + "'"  // SQL injection!`,
    },
    "python": {
        parameterizedExample: `
# CORRECT: SQLAlchemy ORM (prevents injection automatically)
user = session.query(User).filter(User.id == user_id).first()

# CORRECT: SQLAlchemy Core with bound parameters
result = conn.execute(text("SELECT id FROM users WHERE email = :email"), {"email": email})

# CORRECT: psycopg2 with %s placeholders
cur.execute("SELECT id FROM users WHERE id = %s", (user_id,))`,
        prohibited: `
# WRONG: f-string or % formatting in SQL
cur.execute(f"SELECT * FROM users WHERE name = '{user_name}'")  # SQL injection!`,
    },
    "java": {
        parameterizedExample: `
// CORRECT: JPA/Hibernate named parameters
TypedQuery<User> q = em.createQuery(
    "SELECT u FROM User u WHERE u.email = :email", User.class);
q.setParameter("email", email);

// CORRECT: JDBC PreparedStatement
PreparedStatement ps = conn.prepareStatement(
    "SELECT id FROM users WHERE id = ?");
ps.setLong(1, userId);`,
        prohibited: `
// WRONG: string concatenation
String query = "SELECT * FROM users WHERE name = '" + userName + "'"; // SQL injection!`,
    },
    "ruby": {
        parameterizedExample: `
# CORRECT: ActiveRecord (prevents injection automatically)
User.where(id: user_id)
User.where(email: email)

# CORRECT: ActiveRecord with ? placeholder for raw SQL
User.where("email = ?", email)`,
        prohibited: `
# WRONG: string interpolation in where clause
User.where("name = '#{user_name}'")  # SQL injection!`,
    },
    "nodejs": {
        parameterizedExample: `
// CORRECT: pg (node-postgres) parameterized query
const result = await client.query(
    'SELECT id FROM users WHERE email = $1', [email]);

// CORRECT: Sequelize ORM (prevents injection automatically)
const user = await User.findOne({ where: { id: userId } });`,
        prohibited: `
// WRONG: template literal in query
const result = await client.query(
    \`SELECT * FROM users WHERE name = '\${userName}'\`);  // SQL injection!`,
    },
}
```

#### Secret handling per language (brief — no language-specific divergence)

Secrets are handled uniformly across languages via env vars. The `security-guide`
overview covers this in one section; no per-language specialization needed.

#### Overview (no language argument)

```
# Security Coding Guide

## The Non-Negotiables
1. Parameterized queries only — never interpolate user input into SQL.
2. Credentials from env vars only — never hardcode, never log.
3. HTML-escape all output — never concatenate user input into HTML.

## SQL Injection Prevention
Run `security-guide language=<go|python|java|ruby|nodejs>` for language-specific examples.

## Secret Handling
- Read secrets from environment variables (injected by IAF or Kubernetes Secrets).
- Never log env var values that may contain credentials.
- Never commit secrets to source control.

## Output Encoding
- Use your framework's template engine with auto-escaping.
- Set Content-Type explicitly on all responses.

## Dependency Hygiene
- Pin exact versions in lock files.
- Enable automated vulnerability scanning.
- Check licenses against iaf://org/license-policy.

## Full Standards Reference
Read iaf://org/security-standards for the machine-readable policy.

## OWASP Top 10 (most relevant)
- A03:2021 Injection: https://owasp.org/Top10/A03_2021-Injection/
- A02:2021 Cryptographic Failures: https://owasp.org/Top10/A02_2021-Cryptographic_Failures/
- A06:2021 Vulnerable Components: https://owasp.org/Top10/A06_2021-Vulnerable_and_Outdated_Components/
```

### Cross-reference in `coding-guide`

Add to the end:

```
## Security
Before writing code that handles user input, database queries, or external API calls,
read the `security-guide` prompt and `iaf://org/security-standards`. SQL injection and
secret leakage are the highest-risk vectors for agent-generated code.
```

### Cross-reference in `deploy-guide`

Add to the secrets/env vars section:

```
## Security
Read the `security-guide` prompt before deploying applications that handle user input
or sensitive data. The security guide covers SQL injection prevention and secret
handling for all supported languages.
```

## Multi-tenancy & Shared Resource Impact

Stateless, read-only, org-wide. No K8s access. No per-tenant specialization.

## Security Considerations

This issue IS a security control. The content must be reviewed for accuracy:

- **SQL injection examples must be correct**: incorrect "safe" examples would give agents
  false confidence. Recommend a peer review of all per-language examples before merging.
- **The `prohibited` examples must never be rendered as suggestions** — they are shown
  only as anti-patterns under explicit "WRONG" labels.
- **File path validation**: `COACH_SECURITY_STANDARDS_FILE` validated by `loadStandards`.
- **OWASP URLs**: static links, no user input involved. Safe.
- **No enforcement at build time**: the standard is advisory. Future work item for SAST
  integration (noted in issue out-of-scope).
- Flag: `needs-security-review` — the SQL injection examples should be reviewed by
  someone with application security expertise before this ships.

## Resource & Performance Impact

One small embedded JSON resource (~2 KB). Per-language SQL examples add ~3 KB to the
prompt text. Negligible.

## Migration / Compatibility

- `iaf://org/security-standards` is a new resource URI — no existing agent code
  references it.
- `security-guide` is a new prompt — no count regressions, only additions.
- Cross-references in `coding-guide` and `deploy-guide` are additive text; no existing
  agent behavior changes.
- Test counts must be updated.

## Open Questions for Developer

- Should the `security-guide` language argument accept the same aliases as `logging-guide`
  (e.g. `golang`, `node`, `py`)? Recommendation: yes, reuse `languageAliases` from
  `internal/mcp/coach/prompts/aliases.go` (defined in Issue #56).
- Should `deploy-guide` cross-reference the coach's `security-guide` prompt by name, or
  only reference the resource URI? If the coach is not configured (no proxy), agents
  won't find `security-guide`. Recommendation: reference both the prompt name and the
  resource URI, so agents can fall back to reading the resource directly.
