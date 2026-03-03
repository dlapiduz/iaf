# Architecture: Coach — CI/CD Standards and Pipeline Guidance (Issue #61)

## Approach

Two changes:

1. **Make `iaf://org/github-standards` operator-configurable**: the existing resource
   returns a hardcoded map. Move defaults to `defaults/github-standards.json` in the
   coach resources package and add a `COACH_GITHUB_STANDARDS_FILE` env var override,
   following the exact pattern used by logging/metrics/tracing standards.

2. **New `iaf://org/cicd-standards` resource and `cicd-guide` prompt**: a structured
   CI/CD policy covering required pipeline stages, manual approval gates, environment
   definitions, and deployment strategy. File-override pattern with
   `COACH_CICD_STANDARDS_FILE`.

The `github-guide` prompt is updated to reference `cicd-guide` for the CI/CD subset.
GitHub-conditional registration is preserved.

Depends on Issue #56 (coach server) for the package location.

## Changes Required

### New files

- `internal/mcp/coach/resources/defaults/github-standards.json` — hardcoded defaults
  extracted from `github_standards.go`
- `internal/mcp/coach/resources/cicd_standards.go` — `RegisterCICDStandards` function
- `internal/mcp/coach/resources/defaults/cicd-standards.json` — embedded default
- `internal/mcp/coach/prompts/cicd_guide.go` — `RegisterCICDGuide` function

### Modified files

- `internal/mcp/coach/resources/github_standards.go` (after #56 migration) — replace
  hardcoded map with file-override using `loadStandards(deps.GitHubStandardsFile, defaultGitHubStandards)`
- `internal/mcp/coach/deps.go` — add `GitHubStandardsFile string` and
  `CICDStandardsFile string` fields
- `internal/mcp/coach/server.go` — call `resources.RegisterCICDStandards` and
  `prompts.RegisterCICDGuide`; preserve conditional GitHub registration
- `cmd/coachserver/main.go` — read `COACH_GITHUB_STANDARDS_FILE` and
  `COACH_CICD_STANDARDS_FILE` env vars
- `internal/mcp/coach/prompts/github_guide.go` (after #56 migration) — add reference
  to `cicd-guide`
- `internal/mcp/coach/prompts/prompts_test.go` — update `TestListPrompts` count
- `internal/mcp/coach/resources/resources_test.go` — update `TestListResources` count

## Data / API Changes

### `defaults/github-standards.json`

Extract the existing hardcoded standards map exactly as JSON:

```json
{
  "defaultBranch": "main",
  "branchNamingPattern": "^(feat|fix|chore|docs|test)/[a-z0-9][a-z0-9-]*$",
  "requiredReviewers": {
    "solo-agent":   0,
    "multi-agent":  1,
    "human-review": 1
  },
  "requiredStatusChecks": ["CI / ci"],
  "commitMessageFormat": "Conventional Commits (https://www.conventionalcommits.org)",
  "ciTemplate": ".github/workflows/ci.yml",
  "iafIntegration": {
    "deployBranch": "main",
    "deployMethod": "git"
  }
}
```

### Updated `RegisterGitHubStandards`

```go
//go:embed defaults/github-standards.json
var defaultGitHubStandards []byte

func RegisterGitHubStandards(server *gomcp.Server, deps *coach.Dependencies) {
    server.AddResource(&gomcp.Resource{
        URI:         "iaf://org/github-standards",
        Name:        "github-standards",
        Description: "Organisation GitHub workflow conventions — branching strategy, branch protection, required status checks, commit format, and CI template.",
        MIMEType:    "application/json",
    }, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
        content, err := loadStandards(deps.GitHubStandardsFile, defaultGitHubStandards)
        if err != nil {
            return nil, fmt.Errorf("loading github standards: %w", err)
        }
        return &gomcp.ReadResourceResult{
            Contents: []*gomcp.ResourceContents{
                {URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
            },
        }, nil
    })
}
```

No behavior change visible to agents; operator can now override via file.

### `EnvironmentDef` type and CI/CD standards struct

Define these in `internal/mcp/coach/resources/cicd_standards.go` (not in orgstandards,
since this is coach-specific and not shared with the K8s controller):

```go
type CICDStandards struct {
    // RequiredStages are pipeline stages every CI/CD pipeline must include, in order.
    RequiredStages []string `json:"requiredStages"`

    // ApprovalRequired lists stages that require a manual approval gate before execution.
    ApprovalRequired []string `json:"approvalRequired"`

    // Environments defines the deployment targets and promotion rules.
    Environments []EnvironmentDef `json:"environments,omitempty"`

    // DeploymentStrategy is the default strategy for IAF Application deployments.
    // One of: rolling, blue-green, canary.
    DeploymentStrategy string `json:"deploymentStrategy"`
}

type EnvironmentDef struct {
    Name           string   `json:"name"`
    PromotesFrom   []string `json:"promotesFrom,omitempty"` // previous env names
    AutoDeploy     bool     `json:"autoDeploy"`
    RequiredChecks []string `json:"requiredChecks,omitempty"`
}
```

### `defaults/cicd-standards.json`

```json
{
  "requiredStages": [
    "lint",
    "unit-test",
    "security-scan",
    "build",
    "deploy-staging",
    "smoke-test",
    "deploy-prod"
  ],
  "approvalRequired": ["deploy-prod"],
  "environments": [
    {
      "name": "staging",
      "autoDeploy": true,
      "requiredChecks": ["lint", "unit-test", "security-scan", "build"]
    },
    {
      "name": "prod",
      "promotesFrom": ["staging"],
      "autoDeploy": false,
      "requiredChecks": ["smoke-test"]
    }
  ],
  "deploymentStrategy": "rolling"
}
```

### `RegisterCICDStandards`

Follows the exact same pattern as `RegisterLoggingStandards`:

```go
//go:embed defaults/cicd-standards.json
var defaultCICDStandards []byte

func RegisterCICDStandards(server *gomcp.Server, deps *coach.Dependencies) {
    server.AddResource(&gomcp.Resource{
        URI:         "iaf://org/cicd-standards",
        Name:        "org-cicd-standards",
        Description: "CI/CD pipeline standards — required stages, manual approval gates, environment promotion rules, and deployment strategy.",
        MIMEType:    "application/json",
    }, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
        content, err := loadStandards(deps.CICDStandardsFile, defaultCICDStandards)
        if err != nil {
            return nil, fmt.Errorf("loading cicd standards: %w", err)
        }
        return &gomcp.ReadResourceResult{
            Contents: []*gomcp.ResourceContents{
                {URI: req.Params.URI, MIMEType: "application/json", Text: string(content)},
            },
        }, nil
    })
}
```

### `cicd-guide` prompt content outline

```
# CI/CD Standards Guide

## Required Pipeline Stages
Every pipeline must include these stages, in this order:
1. lint — check code style and formatting
2. unit-test — run the test suite with coverage
3. security-scan — SAST/dependency scan (see security-guide)
4. build — produce the container image or deployable artifact
5. deploy-staging — deploy to staging environment
6. smoke-test — basic health check on staging
7. deploy-prod — deploy to production (requires manual approval)

## Manual Approval Gates
These stages require a human or authorised bot to approve before running:
- deploy-prod

## Environment Promotion
Staging → (manual gate) → Prod

Read iaf://org/cicd-standards for the machine-readable pipeline policy.
Read iaf://org/github-standards for GitHub branch and CI template conventions.

## Deployment Strategy
IAF Applications use rolling updates by default. For blue-green or canary,
configure spec.replicas and use multiple Application CRs with different weights.

## Generating a GitHub Actions Workflow
Use the github-guide prompt for a starter .github/workflows/ci.yml that implements
these required stages. Extend it with your application's test and build commands.
```

### `github-guide` cross-reference update

Add at the end of the github-guide prompt:

```
## CI/CD Policy
For required pipeline stages, approval gates, and environment promotion rules,
read the `cicd-guide` prompt and `iaf://org/cicd-standards`.
```

## Multi-tenancy & Shared Resource Impact

Stateless, read-only, org-wide. No K8s access.
GitHub conditional registration is preserved: `RegisterGitHubStandards` and
`prompts.RegisterGitHubGuide` are only called when GitHub is configured (same condition
as current platform: `deps.GitHub != nil` → equivalent coach condition to be defined,
e.g. a `GitHubOrg string` field on `CoachDependencies`).

## Security Considerations

- **File path validation**: unchanged — `loadStandards` rejects `..`.
- **Stage names and environment names**: display-only strings. Not evaluated or executed.
  No injection risk.
- **GitHub standards file**: could contain internal branch naming conventions or CI
  check names. Treat as confidential; protect `COACH_GITHUB_STANDARDS_FILE` accordingly.

## Resource & Performance Impact

Two additional small embedded JSON resources (~500 bytes each). Negligible.

## Migration / Compatibility

- `iaf://org/github-standards` behavior is unchanged for agents; the underlying
  implementation switches to file-override. Operators gain configuration flexibility.
- `iaf://org/cicd-standards` is new — no existing agent code references it.
- Test counts must be updated.

## Open Questions for Developer

- Should `cicd-guide` be registered unconditionally, or only when GitHub is configured?
  Recommendation: unconditional — CI/CD standards apply regardless of VCS provider.
- Should `deploymentStrategy` in `CICDStandards` map to IAF Application fields in the
  prompt output? E.g. explain how to configure rolling vs blue-green in Application spec.
  Recommendation: yes, include a brief mapping in the cicd-guide prompt.
