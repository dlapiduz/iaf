package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterGitHubGuide registers the github-guide prompt.
// Call only when GitHub is configured (deps.GitHub != nil).
func RegisterGitHubGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "github-guide",
		Description: "Step-by-step GitHub workflow guide for IAF: repo creation, branch naming, commits, PRs, and deploying from Git. Tailored to the chosen workflow mode.",
		Arguments: []*gomcp.PromptArgument{
			{
				Name:        "workflow",
				Description: "Workflow mode: 'solo-agent' (default), 'multi-agent', or 'human-review'.",
				Required:    false,
			},
		},
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		workflow := strings.ToLower(strings.TrimSpace(req.Params.Arguments["workflow"]))
		if workflow == "" {
			workflow = "solo-agent"
		}

		var sb strings.Builder
		sb.WriteString("# GitHub Workflow Guide for IAF\n\n")

		sb.WriteString(fmt.Sprintf("**Workflow mode**: `%s`\n\n", workflow))

		sb.WriteString("## Step 1: Create the Repository\n\n")
		sb.WriteString("Call `setup_github_repo` to create the repository, apply branch protection, and commit a starter CI workflow:\n\n")
		sb.WriteString("```\n")
		sb.WriteString("setup_github_repo session_id=<your-session> repo_name=<name> visibility=private\n")
		sb.WriteString("```\n\n")
		sb.WriteString("Returns `clone_url` — use this with `deploy_app` to deploy from Git.\n\n")

		sb.WriteString("## Step 2: Branch Naming\n\n")
		sb.WriteString("Follow the org convention: `<type>/<slug>`\n\n")
		sb.WriteString("- `feat/my-feature` — new feature\n")
		sb.WriteString("- `fix/login-bug` — bug fix\n")
		sb.WriteString("- `chore/update-deps` — maintenance\n")
		sb.WriteString("- `docs/readme-update` — documentation\n")
		sb.WriteString("- `test/add-unit-tests` — tests\n\n")

		sb.WriteString("## Step 3: Commit Messages\n\n")
		sb.WriteString("Use [Conventional Commits](https://www.conventionalcommits.org):\n\n")
		sb.WriteString("```\n")
		sb.WriteString("feat: add user authentication\n")
		sb.WriteString("fix: resolve race condition in worker pool\n")
		sb.WriteString("chore: bump dependencies\n")
		sb.WriteString("```\n\n")

		switch workflow {
		case "multi-agent":
			sb.WriteString("## Step 4: Multi-Agent PR Review\n\n")
			sb.WriteString("- Agent A opens the PR and posts the PR URL as output.\n")
			sb.WriteString("- Agent B fetches the PR URL, reviews the diff, and posts an approving review via the GitHub API (or a GitHub MCP server).\n")
			sb.WriteString("- Branch protection requires 1 approving reviewer — the PR merges after approval + CI passes.\n\n")
		case "human-review":
			sb.WriteString("## Step 4: Human Review\n\n")
			sb.WriteString("- Open a PR from your feature branch to `main`.\n")
			sb.WriteString("- Assign a human reviewer — 1 approving review is required before merge.\n")
			sb.WriteString("- CI must pass (`CI / ci` check) before the PR can be merged.\n\n")
		default: // solo-agent
			sb.WriteString("## Step 4: Solo-Agent Workflow\n\n")
			sb.WriteString("- No PR reviews required — push directly to `main` or merge your own PR.\n")
			sb.WriteString("- Required CI check (`CI / ci`) must pass before merge.\n\n")
		}

		sb.WriteString("## Step 5: Deploy from Git\n\n")
		sb.WriteString("After merging to `main`, deploy the repository on IAF:\n\n")
		sb.WriteString("```\n")
		sb.WriteString("deploy_app session_id=<your-session> name=<app-name> git_url=<clone_url>\n")
		sb.WriteString("```\n\n")
		sb.WriteString("IAF uses kpack to build the image automatically from the `main` branch.\n\n")

		sb.WriteString("## Next Steps\n\n")
		sb.WriteString("- Read `iaf://org/github-standards` for the machine-readable standards document.\n")
		sb.WriteString("- Update `.github/workflows/ci.yml` with language-specific lint, test, and build steps.\n")
		sb.WriteString(fmt.Sprintf("- Once deployed, your app is at `http://<app-name>.%s`.\n", deps.BaseDomain))

		return &gomcp.GetPromptResult{
			Description: "GitHub workflow guide for IAF.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: sb.String()},
				},
			},
		}, nil
	})
}
