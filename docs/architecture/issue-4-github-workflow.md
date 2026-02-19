# Architecture: GitHub Workflow Standard via MCP (Issue #4)

## Technical Design

### Approach

Add three components: (1) an `iaf://org/github-standards` MCP resource returning a structured JSON document of org GitHub conventions, (2) a `github-guide` MCP prompt with an optional `workflow` argument, and (3) a `setup_github_repo` MCP tool that calls the GitHub REST API to create a repo, apply branch protection rules, and commit an embedded CI pipeline template. The GitHub API is abstracted behind a `github.Client` interface in a new `internal/github` package so tests can inject a mock. The platform's `IAF_GITHUB_TOKEN` is stored in `Dependencies` and never surfaced in tool output, logs, or error messages. All three components are registered only if `IAF_GITHUB_TOKEN` is non-empty; existing deployments without the env var see no new surface area. An `IAF_GITHUB_ORG` env var, when set, scopes all repo creation to that org; if unset, the tool fails fast with a clear error rather than creating repos in an agent's personal account.

**Security note**: This design has moderate security risk. See Security Considerations. Adding `needs-security-review` label.

### Changes Required

- **New `internal/github/client.go`** — `Client` interface with methods: `CreateRepo(ctx, org, name, private bool) (*RepoInfo, error)`, `SetBranchProtection(ctx, owner, repo, branch string, cfg BranchProtectionConfig) error`, `CreateFile(ctx, owner, repo, path, message string, content []byte) error`; `RepoInfo` struct: `CloneURL`, `HTMLURL`, `Name`. `HTTPClient` concrete implementation using `net/http` with GitHub REST API v3; reads `Authorization: Bearer <token>` header from the token passed at construction.

- **New `internal/github/mock_client.go`** — `MockClient` implementing `Client` interface for use in tests; configurable with per-method function fields (e.g., `CreateRepoFn func(...) (*RepoInfo, error)`).

- **New `internal/github/client_test.go`** — unit tests for request construction and error handling using `httptest.NewServer`.

- **New `internal/mcp/resources/github_standards.go`** — `RegisterGitHubStandards(server, deps)` registers `iaf://org/github-standards` as a static MCP resource; content built from a `GitHubStandards` struct defined in this file (org can override via the same org standards config file from issue #1 in a future iteration; for now hardcoded defaults).

- **New `internal/mcp/prompts/github_guide.go`** — `RegisterGitHubGuide(server, deps)` registers `github-guide` prompt with optional `workflow` argument (`solo-agent`, `multi-agent`, `human-review`); returns a step-by-step Markdown playbook covering: create repo via `setup_github_repo`, clone, branch naming, commits, PRs, and IAF git-deployment integration.

- **New `internal/mcp/tools/setup_github_repo.go`** — `RegisterSetupGithubRepo(server, deps)`:
  - Input: `session_id string`, `repo_name string`, `visibility string` (`"private"` or `"public"`)
  - Validates `session_id` via `deps.ResolveNamespace`
  - Validates `repo_name` using `validation.ValidateGitHubRepoName` (see below)
  - Validates `visibility` is `"private"` or `"public"`
  - If `deps.GitHubOrg == ""`, returns error: `"IAF_GITHUB_ORG not configured; contact your platform operator"`
  - Calls `deps.GitHub.CreateRepo(ctx, deps.GitHubOrg, repoName, private)`
  - Calls `deps.GitHub.SetBranchProtection(ctx, owner, repoName, "main", protectionConfig)`
  - Reads embedded CI template, calls `deps.GitHub.CreateFile(ctx, owner, repoName, ".github/workflows/ci.yml", "chore: add CI pipeline", ciTemplate)`
  - Returns JSON: `{ "repo_name", "clone_url", "html_url", "visibility", "branch_protection_applied", "ci_workflow_committed" }`
  - Wraps all GitHub errors before returning — strip any string that could contain the token

- **New `internal/mcp/resources/scaffolds/github/ci.yml`** (embedded) — GitHub Actions CI pipeline template:
  ```yaml
  name: CI
  on:
    push:
      branches: [main]
    pull_request:
  jobs:
    ci:
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4
        - name: Set up environment
          run: echo "Add language setup here (e.g., actions/setup-go, actions/setup-node)"
        - name: Lint
          run: echo "Add lint step here"
        - name: Test
          run: echo "Add test step here"
        - name: Build
          run: echo "Add build step here"
  ```
  The template passes CI (each step is `echo`-based) so branch protection checks do not block the initial push.

- **`internal/validation/validation.go`** (from issue #14 design) — add `ValidateGitHubRepoName(name string) error`: max 100 characters, regexp `^[a-zA-Z0-9._-]+$`, rejects `..` traversal sequences; add test cases.

- **`internal/mcp/tools/deps.go`** — add fields: `GitHubToken string`, `GitHubOrg string`, `GitHub github.Client` (interface type).

- **`internal/mcp/server.go`** — after existing registrations, add a guarded block:
  ```go
  if deps.GitHub != nil {
      tools.RegisterSetupGithubRepo(server, deps)
      resources.RegisterGitHubStandards(server, deps)
      prompts.RegisterGitHubGuide(server, deps)
  }
  ```
  Update `serverInstructions` to mention `setup_github_repo` (shown only when GitHub is configured — acceptable to always include the description since the tool list controls availability).

- **`internal/mcp/prompts/deploy_guide.go`** — add a "Git-Based Deployment with GitHub" section referencing `github-guide` prompt and `setup_github_repo` tool.

- **`cmd/mcpserver/main.go`** — read `IAF_GITHUB_TOKEN` and `IAF_GITHUB_ORG` from viper config; if token non-empty, construct `github.NewHTTPClient(token)` and set `deps.GitHub`, `deps.GitHubToken`, `deps.GitHubOrg`.

- **Tests**:
  - `internal/mcp/tools/setup_github_repo_test.go` — table-driven: valid inputs succeed (mock returns canned RepoInfo); invalid `repo_name` returns validation error; missing `IAF_GITHUB_ORG` returns actionable error; GitHub API error (mock returns 422) returns descriptive error without exposing token.
  - `internal/mcp/resources/resources_test.go` — test `iaf://org/github-standards` returns valid JSON with required keys.
  - `internal/mcp/prompts/prompts_test.go` — test `github-guide` with each `workflow` value and without argument.

### Data / API Changes

**New MCP resource**: `iaf://org/github-standards`
- Static (not a template), `application/json`
- Structure:
```json
{
  "defaultBranch": "main",
  "branchNamingPattern": "^(feat|fix|chore|docs|test)/[a-z0-9][a-z0-9-]*$",
  "requiredReviewers": {
    "solo-agent": 0,
    "multi-agent": 1,
    "human-review": 1
  },
  "requiredStatusChecks": ["CI / ci"],
  "commitMessageFormat": "Conventional Commits (https://www.conventionalcommits.org)",
  "ciTemplate": ".github/workflows/ci.yml",
  "iafIntegration": {
    "deployBranch": "main",
    "deployMethod": "git",
    "note": "After repo setup, call deploy_app with git_url pointing to this repo"
  }
}
```

**New MCP prompt**: `github-guide`, optional argument `workflow` (`solo-agent` | `multi-agent` | `human-review`; default `solo-agent`).

**New MCP tool**: `setup_github_repo`
- Inputs: `session_id`, `repo_name`, `visibility`
- Output:
```json
{
  "repo_name": "myapp",
  "clone_url": "https://github.com/myorg/myapp.git",
  "html_url": "https://github.com/myorg/myapp",
  "visibility": "private",
  "branch_protection_applied": true,
  "ci_workflow_committed": true
}
```

**New env vars**: `IAF_GITHUB_TOKEN` (required for GitHub features), `IAF_GITHUB_ORG` (required at runtime; platform fails the tool call if unset).

**New `github.Client` interface** in `internal/github` — not part of the public MCP API but a key internal contract.

### Multi-tenancy & Shared Resource Impact

GitHub is a shared resource outside Kubernetes. Any agent with a valid `session_id` can call `setup_github_repo`. There is no per-namespace quota on GitHub repo creation — a misconfigured or misbehaving agent could create many repos in the org.

Mitigations:
1. `IAF_GITHUB_ORG` restricts creation to a single org (operators must set this).
2. `repo_name` validation prevents names that could conflict with platform repos or include path traversal.
3. If `CreateRepo` returns a 422 (repo already exists), the tool returns an actionable error — it does NOT continue to set protection rules on a repo it did not create (could interfere with an existing project).
4. Document the known limitation: no per-session repo quota. Operator-level GitHub org quotas (max repos, cost controls) are the backstop.

The `iaf://org/github-standards` resource and `github-guide` prompt are read-only and global — no isolation concerns.

### Security Considerations

- **Token never exposed**: `IAF_GITHUB_TOKEN` must never appear in tool output, error messages, or log lines. All errors returned from `deps.GitHub.*` methods must be wrapped by the tool handler before returning to the agent. The `internal/github` client implementation must not include the token in error strings.
- **Required token scopes**: `repo` (full control of private repositories, includes branch protection API). Do **not** use `admin:org` — it is not needed. Document the exact scope in deployment instructions.
- **`IAF_GITHUB_ORG` enforcement**: if unset, `setup_github_repo` returns error `"IAF_GITHUB_ORG not configured; contact your platform operator"` immediately — it does not fall back to creating in the user account. This prevents agents from scattering repos across personal accounts when the operator forgets to set the env var.
- **`repo_name` validation**: `ValidateGitHubRepoName` must reject: empty string, names containing `..`, names starting or ending with `.` or `-`, names longer than 100 characters. GitHub's own naming rules allow `.` and `_` — the validator must accept these while still applying the above restrictions. Never pass the raw `repo_name` to any shell command.
- **CI template content**: the embedded `.github/workflows/ci.yml` must use only trusted actions (`actions/checkout@v4` pinned to SHA or major-version tag); must not include `${{ github.event.*.body }}` or any user-controlled input in `run:` steps without sanitization; must not `echo` secrets. Review the template before merge.
- **Branch protection config**: for `solo-agent` workflow, set `required_status_checks` with `strict: true` and `contexts: ["CI / ci"]`; set `required_pull_request_reviews: null` (no PR review required). For `human-review`/`multi-agent`, set `required_pull_request_reviews: { required_approving_review_count: 1 }`. This prevents agents from self-approving PRs in review workflows.
- **Rate limiting**: check GitHub API `X-RateLimit-Remaining` header; if ≤ 10, return error `"GitHub API rate limit nearly exhausted; try again later"` rather than proceeding (three API calls per tool invocation means running out mid-transaction could leave a repo in partially-configured state).
- **Partial failure handling**: if `CreateRepo` succeeds but `SetBranchProtection` fails, the tool should return an error indicating partial success: `{ "repo_created": true, "clone_url": "...", "branch_protection_applied": false, "error": "branch protection failed: ..." }`. The agent can retry or report to the operator. Do not silently return success.

**⚠ Escalate to security review (`needs-security-review`)**:
- Token scope minimization and token rotation policy
- Org allowlist enforcement (can `IAF_GITHUB_ORG` be bypassed?)
- Risk of token compromise (token has `repo` scope — compromise affects all org repos)
- CI template content review for supply chain risks (pinned actions, no user-controlled injection)

### Resource & Performance Impact

- 3 GitHub REST API calls per `setup_github_repo` invocation (create repo, set branch protection, create file).
- GitHub rate limit for authenticated apps: 5,000 requests/hour. At typical agentic usage (<10 repos/hour), this is not a concern. Log `X-RateLimit-Remaining` at DEBUG level.
- Binary size: +~1 KB for the embedded CI YAML template.
- No additional K8s objects created by this feature.

### Migration / Compatibility

- Purely additive: the tool, resource, and prompt are registered only if `IAF_GITHUB_TOKEN != ""`. Existing deployments without the env var are unaffected.
- No CRD changes, no API schema changes, no database migrations.
- The `internal/github` package is a new internal dependency — no external import cycles.

### Open Questions for Developer

1. **HTTP client vs go-github library**: check `go.mod` for `github.com/google/go-github`. If absent, use `net/http` with manual JSON marshaling for the three required endpoints — this avoids adding a heavyweight dependency for three API calls. If `go-github` is already present (via some transitive dep), use it.
2. **Partial failure response**: should the output struct include a `warnings` array for non-fatal issues (e.g., branch protection applied but CI file commit failed due to empty repo)? Recommend yes — agents need machine-actionable status for each step.
3. **`multi-agent` workflow in `github-guide`**: describe the pattern as "Agent B fetches the PR URL from Agent A's output, calls the GitHub API to post a review comment via a separate MCP GitHub tool, and approves if all checks pass." This avoids requiring human intervention while maintaining a meaningful review gate.
4. **IAF webhook auto-wiring**: should `setup_github_repo` also call the IAF API to register the repo URL for automatic re-deploys on push? Out of scope for this issue — leave as an open item in the `github-guide` prompt's "Next Steps" section.
5. **Repo name collision**: if a repo with that name already exists in the org, return error `"repo 'myapp' already exists in org 'myorg'; choose a different name"` — do not attempt to configure the existing repo.
