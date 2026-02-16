package prompts

import (
	"context"
	"fmt"

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
		text := fmt.Sprintf(`# IAF Deployment Guide

## Platform Overview
IAF (Intelligent Application Fabric) deploys applications on Kubernetes using the Application custom resource in namespace %q. Applications are exposed via Traefik ingress at <name>.%s.

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
- Names must be unique within the namespace.
- The name becomes part of the URL: <name>.%s

## Networking
- Default container port: 8080 (configurable via "port" field).
- Applications are exposed via HTTP at http://<name>.%s
- Health endpoint recommendation: implement a /health or /healthz endpoint for readiness probes.

## Security Considerations
- All builds use the Paketo buildpack stack based on Ubuntu Jammy LTS.
- Containers run as non-root by default (buildpack behavior).
- Environment variables can be set via the "env" field — do not embed secrets in source code.
- Images are built in-cluster; no external registry credentials are needed for blob/git builds.

## Replicas and Scaling
- Default: 1 replica.
- Set "replicas" field to scale horizontally.
- The platform manages the Kubernetes Deployment; pods are spread across available nodes.

## Recommended Workflow
1. Read the language-guide prompt for your target language to understand buildpack requirements.
2. Write buildpack-compatible code (correct files, entry points, dependency manifests).
3. Use push_code to upload source or provide a git URL.
4. Use deploy_app to create the Application CR.
5. Use app_status to monitor build and deployment progress.
6. Use app_logs to debug any issues.
`, deps.Namespace, deps.BaseDomain, deps.BaseDomain, deps.BaseDomain)

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
