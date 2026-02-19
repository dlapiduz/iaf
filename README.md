# IAF - Intelligent Application Fabric

IAF is a platform that lets AI agents deploy and manage applications on Kubernetes. It provides an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server that AI coding assistants like Claude Code can use to build, deploy, and monitor applications automatically.

## How It Works

```
AI Agent (Claude Code)
    |
    | MCP Protocol (HTTP)
    v
IAF API Server ──── MCP Tools (register, push_code, deploy_app, ...)
    |
    v
IAF Controller ──── Watches Application CRs
    |
    v
Kubernetes ──── Deployments, Services, Ingress, kpack Builds
```

1. An AI agent connects to IAF via MCP and calls `register` to get a session
2. Each session gets an isolated Kubernetes namespace
3. The agent uses tools like `push_code` to upload source code or `deploy_app` to deploy container images
4. IAF auto-detects the language (Go, Node.js, Python, Java, Ruby) and builds containers using [kpack](https://github.com/buildpacks-community/kpack)
5. Applications are deployed with routing via [Traefik](https://traefik.io/) and accessible at `<app-name>.<base-domain>`

## Features

- **MCP server** with tools, prompts, and resources for AI agents
- **Multi-agent isolation** via session-based namespaces
- **Auto-build from source** using Cloud Native Buildpacks (Paketo)
- **Deploy from images** or git repositories
- **REST API** for dashboards and scripts
- **Web dashboard** (Next.js) for monitoring applications

## Quick Start

### Prerequisites

- Kubernetes cluster (Rancher Desktop, k3s, or similar)
- [kpack](https://github.com/buildpacks-community/kpack) installed
- [Traefik](https://traefik.io/) as ingress controller

### Deploy

```bash
make deploy-local
```

### Connect Claude Code

```bash
claude mcp add --transport http iaf http://iaf.localhost/mcp
```

Add authentication in your MCP config (`.mcp.json` or `~/.claude.json`):

```json
{
  "mcpServers": {
    "iaf": {
      "type": "http",
      "url": "http://iaf.localhost/mcp",
      "headers": {
        "Authorization": "Bearer <your-api-token>"
      }
    }
  }
}
```

Then ask Claude to deploy an app:

```
> Create a Go hello world app and deploy it as "hello-go"
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `register` | Create a session (call first, returns `session_id` for all other tools) |
| `push_code` | Upload source code and auto-build/deploy |
| `deploy_app` | Deploy from a container image or git repository |
| `app_status` | Check application status, URL, and build progress |
| `app_logs` | View application or build logs |
| `list_apps` | List applications in the current session |
| `delete_app` | Delete an application |

## Documentation

- [Using IAF](docs/usage.md) - Detailed usage guide with examples
- [Claude Code Setup](docs/claude-code-setup.md) - Connecting Claude Code to IAF
- [Contributing](CONTRIBUTING.md) - Development setup and guidelines

## Development

```bash
make build        # Build all binaries
make test         # Run all tests
make generate     # Regenerate CRDs and RBAC
make deploy-local # Build and deploy to local K8s
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development instructions.

## Architecture

IAF consists of three components:

- **API Server** (`cmd/apiserver`) - Serves the MCP endpoint and REST API
- **Controller** (`cmd/controller`) - Kubernetes controller that reconciles Application CRs into Deployments, Services, and IngressRoutes
- **MCP Server** (`cmd/mcpserver`) - Standalone STDIO-based MCP server for local development

The platform uses a custom `Application` CRD (`iaf.io/v1alpha1`) to represent deployed applications.

## License

MIT License - see [LICENSE](LICENSE) for details.
