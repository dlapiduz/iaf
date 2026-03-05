package coach

import (
	"context"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterCICDGuide registers the cicd-guide prompt that provides pipeline
// stage requirements, approval gates, environment promotion rules, and
// deployment strategy guidance for IAF applications.
func RegisterCICDGuide(server *gomcp.Server, deps *Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "cicd-guide",
		Description: "CI/CD pipeline requirements for IAF: required stages, manual approval gates, environment promotion, and deployment strategies mapped to IAF Application fields.",
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		text := cicdGuideText()
		return &gomcp.GetPromptResult{
			Description: "CI/CD pipeline guidance for IAF applications.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}

func cicdGuideText() string {
	bt := "`"
	return fmt.Sprintf(`# IAF CI/CD Pipeline Guide

## Required Pipeline Stages

Every application pipeline must include all of the following stages in order:

1. **lint** — Static analysis and code style checks (language-specific linter)
2. **unit-test** — Automated unit tests with coverage reporting
3. **security-scan** — Dependency vulnerability scan (govulncheck, pip-audit, npm audit, bundler-audit, etc.)
4. **build** — Container image build or source compilation verification
5. **deploy-staging** — Deploy to the staging environment
6. **smoke-test** — Lightweight post-deployment checks against staging
7. **deploy-prod** — Deploy to production (requires manual approval gate — see below)

## Manual Approval Gates

The **deploy-prod** stage requires explicit manual approval before execution.

- In GitHub Actions, use an %senvironment%s with required reviewers configured on the %sprod%s environment.
- No automated pipeline may bypass the approval gate for production deployments.
- Approval must be granted by a human or a designated release manager agent.

## Environment Promotion

### Staging
- Auto-deploys on every merge to %smain%s.
- Required checks before promotion: %slint%s, %sunit-test%s, %ssecurity-scan%s, %sbuild%s.
- No manual approval needed.

### Production
- Promotes from %sstaging%s only — never deploy directly to prod without a staging run.
- Requires manual approval gate (see above).
- Required checks before promotion: %ssmoke-test%s (run against staging).

## Deployment Strategies and IAF Mapping

Choose a deployment strategy based on risk tolerance and traffic requirements:

### Rolling (Default)
Update the %sreplicas%s field on the IAF Application CR. The controller performs a Kubernetes rolling update — new pods are created before old ones are terminated.

`+"```"+`
# Trigger a rolling update by changing the image or env:
deploy_app session_id=<sid> name=<app> image=<new-image>
`+"```"+`

### Blue-Green
Deploy a second Application with a %s-v2%s suffix. Once healthy, update the ingress %shost%s field on the v2 Application to the production hostname, then delete the original.

`+"```"+`
# 1. Deploy v2 with a test hostname
deploy_app session_id=<sid> name=<app>-v2 image=<new-image> host=<app>-v2.<domain>
# 2. Verify v2 is healthy via app_status
# 3. Update v2 to the production hostname (patch Application spec)
# 4. Delete the original Application
delete_app session_id=<sid> name=<app>
`+"```"+`

### Canary
Deploy a new Application alongside the existing one with a lower replica count. Monitor error rates and latency, then scale up the canary and remove the original once confidence is established.

`+"```"+`
# 1. Deploy canary with 1 replica (original has N replicas)
deploy_app session_id=<sid> name=<app>-canary image=<new-image> replicas=1
# 2. Monitor via app_status and app_logs
# 3. Scale up canary, scale down original
# 4. Delete original once canary is stable
`+"```"+`

## GitHub Actions Integration

Use the CI template committed by %ssetup_github_repo%s as the starting point (%s.github/workflows/ci.yml%s). Extend it with language-specific lint, test, and security scan steps.

Branch protection should be configured to require the %sCI / ci%s status check before merging.

## Standards References

- Machine-readable pipeline standard: %siaf://org/cicd-standards%s
- GitHub workflow conventions: %siaf://org/github-standards%s
- GitHub setup guide: %sgithub-guide%s prompt
`,
		// Manual Approval Gates
		bt, bt, bt, bt,
		// Environment Promotion — Staging
		bt, bt, bt, bt, bt, bt, bt, bt, bt, bt,
		// Environment Promotion — Production
		bt, bt, bt, bt,
		// Rolling
		bt, bt,
		// Blue-Green
		bt, bt, bt, bt,
		// GitHub Actions Integration
		bt, bt, bt, bt, bt, bt,
		// Standards References
		bt, bt, bt, bt, bt, bt,
	)
}
