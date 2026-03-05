package coach

import (
	"context"
	"fmt"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type securityGuide struct {
	correctExample string
	wrongExample   string
	notes          string
}

var securityGuides = map[string]securityGuide{
	"go": {
		correctExample: `// CORRECT — parameterized query with database/sql
import "database/sql"

rows, err := db.QueryContext(ctx, "SELECT * FROM users WHERE id = $1", userID)`,
		wrongExample: `// WRONG — string concatenation creates SQL injection risk
rows, err := db.QueryContext(ctx, "SELECT * FROM users WHERE id = " + userID)`,
		notes: "Use `database/sql` with `$1`-style placeholders (PostgreSQL) or `?`-style (MySQL/SQLite). " +
			"For ORMs, use `gorm.Where(\"id = ?\", id)` — never raw string interpolation. " +
			"Use `crypto/subtle.ConstantTimeCompare` for token comparison.",
	},
	"nodejs": {
		correctExample: `// CORRECT — parameterized query with pg pool
const { rows } = await pool.query(
  'SELECT * FROM users WHERE id = $1',
  [userId]
);

// Also correct with Sequelize:
// User.findOne({ where: { id: userId } })`,
		wrongExample: `// WRONG — template literal creates SQL injection risk
const { rows } = await pool.query(
  ` + "`" + `SELECT * FROM users WHERE id = ${userId}` + "`" + `
);`,
		notes: "Always pass values as the second argument array to `pool.query`. " +
			"With Sequelize, use `where` objects or named parameters — never raw SQL with interpolation. " +
			"Use `crypto.timingSafeEqual` for token comparison.",
	},
	"python": {
		correctExample: `# CORRECT — parameterized query with psycopg2
cursor.execute("SELECT * FROM users WHERE id = %s", (user_id,))

# Also correct with SQLAlchemy:
# session.execute(text("SELECT * FROM users WHERE id = :id"), {"id": user_id})`,
		wrongExample: `# WRONG — f-string creates SQL injection risk
cursor.execute(f"SELECT * FROM users WHERE id = {user_id}")`,
		notes: "Always pass parameters as a tuple (note the trailing comma for single values). " +
			"With SQLAlchemy, use `text()` with named bind parameters — never format strings. " +
			"Use `hmac.compare_digest` for token comparison (Python 3.3+).",
	},
	"java": {
		correctExample: `// CORRECT — PreparedStatement prevents SQL injection
PreparedStatement ps = conn.prepareStatement(
    "SELECT * FROM users WHERE id = ?"
);
ps.setLong(1, userId);
ResultSet rs = ps.executeQuery();

// Also correct with JPA:
// @Query("SELECT u FROM User u WHERE u.id = :id")
// User findById(@Param("id") Long id);`,
		wrongExample: `// WRONG — Statement + string concatenation creates SQL injection risk
Statement stmt = conn.createStatement();
ResultSet rs = stmt.executeQuery(
    "SELECT * FROM users WHERE id = " + userId
);`,
		notes: "Always use `PreparedStatement` for JDBC. " +
			"In JPA/Spring Data, use named parameters (`:param`) in `@Query` annotations — never string concatenation. " +
			"Use `MessageDigest.isEqual` or `Arrays.equals` for constant-time comparison.",
	},
	"ruby": {
		correctExample: `# CORRECT — ActiveRecord parameterized queries
user = User.find(id)
users = User.where("name = ?", name)
# Or with hash syntax (preferred):
users = User.where(name: name)`,
		wrongExample: `# WRONG — string interpolation creates SQL injection risk
users = User.where("name = '#{name}'")`,
		notes: "Prefer hash-style `where(name: value)` over SQL string conditions. " +
			"When raw SQL is unavoidable, always use `?` or `:named` placeholders. " +
			"Use `ActiveSupport::SecurityUtils.secure_compare` for constant-time token comparison.",
	},
}

// RegisterSecurityGuide registers the security-guide prompt that provides
// SQL injection prevention examples and security best practices.
// An optional language argument returns per-language parameterized query examples.
func RegisterSecurityGuide(server *gomcp.Server, deps *Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "security-guide",
		Description: "Security coding practices: SQL injection prevention, secret handling, output encoding, dependency hygiene, and OWASP Top 10 guidance. Optional language argument adds per-language parameterized query examples.",
		Arguments: []*gomcp.PromptArgument{
			{
				Name:        "language",
				Description: "Target language (go, nodejs, python, java, ruby). Aliases like 'golang', 'node', 'py' are accepted. Omit for the language-agnostic overview.",
				Required:    false,
			},
		},
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		lang := strings.ToLower(strings.TrimSpace(req.Params.Arguments["language"]))

		var text string
		if lang == "" {
			text = securityGuideOverview()
		} else {
			canonical, ok := languageAliases[lang]
			if !ok {
				return nil, fmt.Errorf("unsupported language %q; supported languages: go, nodejs, python, java, ruby", lang)
			}
			guide := securityGuides[canonical]
			text = renderSecurityGuide(canonical, guide)
		}

		return &gomcp.GetPromptResult{
			Description: "Security coding practices guide for IAF applications.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}

func securityGuideOverview() string {
	return `# IAF Security Coding Guide

## Non-Negotiable Rules

### 1. SQL Injection Prevention
**Never concatenate user input into SQL queries.** Always use parameterized queries or prepared statements.

Run ` + "`security-guide language=<lang>`" + ` for copy-paste-ready examples in go, nodejs, python, java, or ruby.

### 2. Secret Handling
- Read credentials exclusively from environment variables — never hardcode tokens, passwords, or keys in source code.
- Never log secret values, even at debug level. Log only non-sensitive identifiers.
- Never commit secrets to version control — add secret patterns to ` + "`.gitignore`" + ` and use pre-commit hooks.
- Compare tokens with constant-time comparison (` + "`crypto/subtle`" + `, ` + "`hmac.Equal`" + `) to prevent timing attacks.

### 3. Output Encoding
- HTML-escape all user-supplied content rendered in HTML responses.
- Return JSON responses with ` + "`Content-Type: application/json`" + ` to prevent MIME sniffing.
- Set security headers: ` + "`X-Content-Type-Options: nosniff`" + `, ` + "`X-Frame-Options: DENY`" + `.

### 4. Dependency Hygiene
- Pin exact dependency versions in lock files (` + "`package-lock.json`" + `, ` + "`go.sum`" + `, ` + "`Pipfile.lock`" + `, ` + "`Gemfile.lock`" + `).
- Enable automated vulnerability scanning (Dependabot, govulncheck, pip-audit, bundler-audit).
- Remove unused dependencies — every dependency is an attack surface.

## OWASP Top 10 References

| ID | Name |
|----|------|
| A01:2021 | [Broken Access Control](https://owasp.org/Top10/A01_2021-Broken_Access_Control/) |
| A02:2021 | [Cryptographic Failures](https://owasp.org/Top10/A02_2021-Cryptographic_Failures/) |
| A03:2021 | [Injection](https://owasp.org/Top10/A03_2021-Injection/) |
| A05:2021 | [Security Misconfiguration](https://owasp.org/Top10/A05_2021-Security_Misconfiguration/) |
| A06:2021 | [Vulnerable and Outdated Components](https://owasp.org/Top10/A06_2021-Vulnerable_and_Outdated_Components/) |
| A07:2021 | [Identification and Authentication Failures](https://owasp.org/Top10/A07_2021-Identification_and_Authentication_Failures/) |

## Language-Specific SQL Injection Examples

Run ` + "`security-guide`" + ` with a ` + "`language`" + ` argument for copy-paste-ready parameterized query examples:
- ` + "`security-guide language=go`" + `      — database/sql
- ` + "`security-guide language=nodejs`" + `  — pg pool + Sequelize
- ` + "`security-guide language=python`" + `  — psycopg2 + SQLAlchemy
- ` + "`security-guide language=java`" + `    — JDBC PreparedStatement + JPA
- ` + "`security-guide language=ruby`" + `    — ActiveRecord

## Full Standards Reference
Read ` + "`iaf://org/security-standards`" + ` for the machine-readable security standard.
`
}

func renderSecurityGuide(lang string, g securityGuide) string {
	title := strings.ToUpper(lang[:1]) + lang[1:]
	return fmt.Sprintf(`# %s Security Guide — SQL Injection Prevention

## CORRECT: Parameterized Query

`+"```"+`
%s
`+"```"+`

## WRONG: String Concatenation / Interpolation (Anti-Pattern)

`+"```"+`
%s
`+"```"+`

## Notes

%s

## Other Security Rules (apply to all languages)

- **Secret handling**: Read credentials from env vars only — never hardcode. Never log secret values.
- **Output encoding**: HTML-escape user content, set `+"`Content-Type: application/json`"+`, use security headers.
- **Dependency hygiene**: Pin versions in lock files, run automated vulnerability scans, remove unused dependencies.

## Full Standards Reference
Read `+"`iaf://org/security-standards`"+` for the complete machine-readable security standard.
`, title, g.correctExample, g.wrongExample, g.notes)
}
