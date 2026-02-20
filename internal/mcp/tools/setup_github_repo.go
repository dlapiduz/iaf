package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/validation"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ciYAML is the starter CI workflow committed to every new repository.
// All steps are no-op echoes so the initial run passes before the agent
// adds language-specific commands.
const ciYAML = `name: CI
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
        run: echo "Add language setup here (e.g. actions/setup-go, actions/setup-node)"
      - name: Lint
        run: echo "Add lint step here"
      - name: Test
        run: echo "Add test step here"
      - name: Build
        run: echo "Add build step here"
`

// SetupGithubRepoInput is the input struct for the setup_github_repo tool.
type SetupGithubRepoInput struct {
	SessionID  string `json:"session_id" jsonschema:"required - your session ID from the register tool"`
	RepoName   string `json:"repo_name"  jsonschema:"required - repository name (alphanumeric, dots, hyphens, underscores; max 100 chars)"`
	Visibility string `json:"visibility,omitempty" jsonschema:"repository visibility: 'private' (default) or 'public'"`
}

// RegisterSetupGithubRepo registers the setup_github_repo MCP tool.
// This function must only be called when deps.GitHub != nil.
func RegisterSetupGithubRepo(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "setup_github_repo",
		Description: "Create a GitHub repository in the org, apply branch protection, and commit a starter CI workflow. Returns repo URL and a summary of applied settings. Requires IAF_GITHUB_TOKEN and IAF_GITHUB_ORG to be configured.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input SetupGithubRepoInput) (*gomcp.CallToolResult, any, error) {
		// Resolve session first — every tool requires a valid session.
		if _, err := deps.ResolveNamespace(input.SessionID); err != nil {
			return nil, nil, err
		}

		// Validate repo name.
		if err := validation.ValidateGitHubRepoName(input.RepoName); err != nil {
			return nil, nil, err
		}

		// Org must be set by the operator — never fall back to personal accounts.
		if deps.GitHubOrg == "" {
			return nil, nil, fmt.Errorf("IAF_GITHUB_ORG not configured; contact your platform operator")
		}

		private := input.Visibility != "public"

		result := map[string]any{
			"repo_name":                  input.RepoName,
			"visibility":                 visibilityString(private),
			"branch_protection_applied":  false,
			"ci_workflow_committed":       false,
		}

		// Step 1: Create repository.
		info, err := deps.GitHub.CreateRepo(ctx, deps.GitHubOrg, input.RepoName, private)
		if err != nil {
			return nil, nil, fmt.Errorf("creating repository: %w", err)
		}
		result["clone_url"] = info.CloneURL
		result["html_url"] = info.HTMLURL

		// Step 2: Apply branch protection (partial-failure safe).
		protCfg := iafgithub.BranchProtectionConfig{
			RequiredStatusChecks: []string{"CI / ci"},
		}
		if err := deps.GitHub.SetBranchProtection(ctx, deps.GitHubOrg, input.RepoName, "main", protCfg); err != nil {
			result["warnings"] = []string{fmt.Sprintf("branch protection: %s", err.Error())}
		} else {
			result["branch_protection_applied"] = true
		}

		// Step 3: Commit CI workflow (partial-failure safe).
		if err := deps.GitHub.CreateFile(ctx, deps.GitHubOrg, input.RepoName,
			".github/workflows/ci.yml", "Add starter CI workflow", []byte(ciYAML)); err != nil {
			warnings, _ := result["warnings"].([]string)
			result["warnings"] = append(warnings, fmt.Sprintf("CI workflow: %s", err.Error()))
		} else {
			result["ci_workflow_committed"] = true
		}

		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling result: %w", err)
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{
				&gomcp.TextContent{Text: string(data)},
			},
		}, nil, nil
	})
}

func visibilityString(private bool) string {
	if private {
		return "private"
	}
	return "public"
}
