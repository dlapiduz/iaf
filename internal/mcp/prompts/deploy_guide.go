package prompts

import (
	"context"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterDeployGuide registers the deploy-guide prompt that provides
// comprehensive deployment workflow guidance.
func RegisterDeployGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "deploy-guide",
		Description: "Comprehensive deployment workflow guidance for the IAF platform — methods, lifecycle phases, security considerations, and constraints.",
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		text := `# IAF Deployment Guide

## Getting Started — Session Registration

**Before using any tool, you MUST register a session:**
1. Call the ` + "`register`" + ` tool (optionally with a friendly name).
2. Store the returned ` + "`session_id`" + ` — you will need it for EVERY subsequent tool call.
3. Include ` + "`session_id`" + ` in every tool call for the duration of your work.
4. If resuming previous work, reuse a previously stored session_id.

## Platform Overview
IAF (Intelligent Application Fabric) deploys applications on Kubernetes. Each session gets its own isolated namespace. Applications are exposed via Traefik ingress at <name>.` + deps.BaseDomain + `. TLS is enabled by default (HTTPS); set ` + "`tls.enabled: false`" + ` in the Application spec to opt out.

## Deployment Methods

### 1. Container Image (Fastest)
Provide a pre-built container image reference. The platform creates a Deployment and IngressRoute immediately.
- Use when you already have a built image in a registry.
- Set the "image" field on the Application spec.

### 2. Git Repository (Build from source)
Provide a git URL and optional revision. The platform uses kpack with Cloud Native Buildpacks to automatically detect the language, build the image, and deploy.
- Set "git.url" and optionally "git.revision" (defaults to "main").
- Supported languages: Go, Node.js, Python, Java, Ruby.

### 3. Source Upload (Push code directly)
Use the push_code tool to upload source files as a tarball. The platform stores the archive and builds via kpack blob source.
- Ideal for agent-generated code that isn't in a git repository.
- Push code first, then deploy — the platform handles the rest.

## Application Lifecycle Phases
1. **Pending** — Application CR created, awaiting processing.
2. **Building** — kpack is building the source into a container image (git/blob sources only).
3. **Deploying** — Deployment and IngressRoute are being created/updated.
4. **Running** — Pods are ready and traffic is being served.
5. **Failed** — Build or deployment error occurred. Check app_status or app_logs for details.

## Naming Constraints
- Application names must be valid DNS labels: lowercase alphanumeric and hyphens, 1-63 characters.
- Names must be unique within your session's namespace.
- The name becomes part of the URL: <name>.` + deps.BaseDomain + `

## Networking
- Default container port: 8080 (configurable via "port" field).
- Applications are exposed via HTTPS at https://<name>.` + deps.BaseDomain + `
- Health endpoint recommendation: implement a /health or /healthz endpoint for readiness probes.

## Security Considerations
- All builds use the Paketo buildpack stack based on Ubuntu Jammy LTS.
- Containers run as non-root by default (buildpack behavior).
- Environment variables can be set via the "env" field — do not embed secrets in source code.
- Images are built in-cluster; no external registry credentials are needed for blob/git builds.
- Each session is isolated in its own Kubernetes namespace.

## Replicas and Scaling
- Default: 1 replica.
- Set "replicas" field to scale horizontally.
- The platform manages the Kubernetes Deployment; pods are spread across available nodes.

## Coding Standards
Before writing any code, read the org-level coding standards:
- Prompt: ` + "`coding-guide`" + ` (accepts optional ` + "`language`" + ` argument) — markdown guide merging platform and org standards
- Resource: ` + "`iaf://org/coding-standards`" + ` — machine-readable JSON standards document

## Private Repository Access
To clone a private git repository, you must first store a credential:
1. Call ` + "`add_git_credential`" + ` with your session_id, a name, the type (` + "`basic-auth`" + ` or ` + "`ssh`" + `), and the git server URL.
   - For ` + "`basic-auth`" + `: provide ` + "`git_server_url`" + ` as ` + "`https://github.com`" + ` (or your server), plus ` + "`username`" + ` and ` + "`password`" + ` (or a personal access token).
   - For ` + "`ssh`" + `: provide ` + "`git_server_url`" + ` as ` + "`git@github.com`" + ` and ` + "`private_key`" + ` as your PEM-encoded SSH private key.
2. Pass the credential name as ` + "`git_credential`" + ` when calling ` + "`deploy_app`" + `.
3. To rotate a credential, call ` + "`delete_git_credential`" + ` then ` + "`add_git_credential`" + ` again.
4. Credential material is never returned in any tool output — only name, type, and server URL are shown.

## Git-Based Deployment with GitHub
When your code lives in a GitHub repository:
- Call ` + "`setup_github_repo`" + ` to create the repo, apply branch protection, and commit a CI template.
- Read the ` + "`github-guide`" + ` prompt for branch naming, commit format, PR, and review workflow.
- Read ` + "`iaf://org/github-standards`" + ` for the machine-readable GitHub standards document.
- Use ` + "`deploy_app`" + ` with ` + "`git_url`" + ` set to the clone URL returned by ` + "`setup_github_repo`" + `.

## Persistent Data

**Do NOT deploy databases as Applications** (e.g. do not use a postgres Docker image as an Application spec). Use ` + "`provision_service`" + ` instead — it provisions a properly managed, isolated PostgreSQL database via CloudNativePG.

Workflow: ` + "`provision_service`" + ` → poll ` + "`service_status`" + ` every 10s until Ready → ` + "`bind_service`" + ` → use ` + "`DATABASE_URL`" + ` env var in your application.

See the ` + "`services-guide`" + ` prompt for full details.

## Observability

### Logging
Log to stdout in JSON Lines format. Logs are collected automatically — no configuration needed.
- Full guide: ` + "`logging-guide`" + ` prompt (accepts optional ` + "`language`" + ` argument)
- Standard: ` + "`iaf://org/logging-standards`" + `

### Metrics
Expose a ` + "`/metrics`" + ` endpoint in Prometheus text format. Use the RED method: ` + "`http_requests_total`" + ` and ` + "`http_request_duration_seconds`" + `.
- Full guide: ` + "`metrics-guide`" + ` prompt (accepts optional ` + "`language`" + ` argument)
- Standard: ` + "`iaf://org/metrics-standards`" + `

### Tracing
The platform injects ` + "`OTEL_EXPORTER_OTLP_ENDPOINT`" + ` and ` + "`OTEL_SERVICE_NAME`" + ` automatically. Use the OTel SDK — do not hardcode the endpoint.
- Full guide: ` + "`tracing-guide`" + ` prompt (accepts optional ` + "`language`" + ` argument)
- Standard: ` + "`iaf://org/tracing-standards`" + `

## Recommended Workflow
1. Call ` + "`register`" + ` to get a session_id.
2. Read the ` + "`coding-guide`" + ` prompt to understand org coding standards.
3. Read the ` + "`language-guide`" + ` prompt for your target language to understand buildpack requirements.
4. Write buildpack-compatible code following org standards (correct files, entry points, dependency manifests).
5. Use push_code to upload source or provide a git URL (always include session_id).
6. Use deploy_app to create the Application CR (always include session_id).
7. Use app_status to monitor build and deployment progress (always include session_id).
8. Use app_logs to debug any issues (always include session_id).
`

		return &gomcp.GetPromptResult{
			Description: "Comprehensive IAF deployment workflow guidance.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}
