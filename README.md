# IAF - Intelligent Application Fabric

IAF is a Kubernetes-native platform purpose-built for AI agents to deploy, run, and manage applications. It provides an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server that AI coding assistants like Claude Code can use to build, deploy, and monitor applications automatically.

## How It Works

```
AI Agent (Claude Code)
    │
    │ MCP over HTTP / Bearer token auth
    ▼
IAF API Server ──── MCP tools, prompts, resources
    │
    │ creates Application CRs
    ▼
IAF Controller ──── Watches Application CRs
    │
    ▼
Kubernetes ──── Deployments, Services, Traefik IngressRoutes,
                cert-manager Certificates, kpack Image builds
```

1. An AI agent connects to IAF via MCP and calls `register` to get a `session_id`
2. Each session gets its own isolated Kubernetes namespace
3. The agent uses tools like `push_code` to upload source code or `deploy_app` to deploy container images
4. IAF auto-detects the language (Go, Node.js, Python, Java, Ruby) and builds containers using [kpack](https://github.com/buildpacks-community/kpack)
5. Applications are deployed with HTTPS routing via [Traefik](https://traefik.io/) and [cert-manager](https://cert-manager.io/), accessible at `https://<app-name>.<base-domain>`

## Features

- **MCP server** — tools, prompts, and resources for AI agents
- **Multi-agent isolation** — each session gets its own Kubernetes namespace
- **Three deployment methods** — container image, git repository, or source code upload
- **Auto-build from source** — Cloud Native Buildpacks (Paketo) detect the language and build automatically
- **HTTPS by default** — cert-manager issues TLS certificates per application
- **Data catalog** — operators register data sources (databases, APIs) that agents can discover and attach
- **Private git repositories** — agents store per-session git credentials (basic-auth or SSH)
- **REST API** — for dashboards and scripts
- **Web dashboard** — Next.js app for monitoring

## Quick Start

### Prerequisites

- Kubernetes cluster (Rancher Desktop, k3s, or similar)
- [kpack](https://github.com/buildpacks-community/kpack)
- [Traefik](https://traefik.io/) as ingress controller
- [cert-manager](https://cert-manager.io/) (optional, for HTTPS)

### Deploy

```bash
make setup-local   # one-time: namespaces, registry, CRDs, RBAC, kpack SA
make setup-kpack   # one-time: kpack and cluster builder
make deploy-local  # build and deploy the platform
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
        "Authorization": "Bearer iaf-dev-key"
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
| `register` | Create a session — call this first; returns `session_id` for all other tools |
| `push_code` | Upload source code files and auto-build/deploy |
| `deploy_app` | Deploy from a container image or git repository |
| `app_status` | Check phase, URL, build status, and replica count |
| `app_logs` | View application or build logs |
| `list_apps` | List applications in the current session |
| `delete_app` | Delete an application and all its resources |
| `add_git_credential` | Store a git credential (basic-auth or SSH) for private repo access |
| `list_git_credentials` | List stored git credentials (metadata only — no secrets) |
| `delete_git_credential` | Remove a stored git credential |
| `list_data_sources` | List platform data sources available for attachment |
| `get_data_source` | Get details about a specific data source |
| `attach_data_source` | Attach a data source to an app — injects credentials as env vars |

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | How IAF works — components, CRDs, session model, data flow |
| [Operator Guide](docs/operator-guide.md) | Installing and configuring the platform — TLS, data catalog, auth |
| [Usage Guide](docs/usage.md) | Using IAF from Claude Code — tools, prompts, example workflows |
| [Claude Code Setup](docs/claude-code-setup.md) | Connecting Claude Code to IAF |
| [Contributing](CONTRIBUTING.md) | Development setup and guidelines |

## Development

```bash
make build        # Build all three binaries (apiserver, mcpserver, controller)
make test         # Run all tests
make generate     # Regenerate CRD YAML and deepcopy from type annotations
make deploy-local # Build Docker image and deploy to local K8s
make fmt          # Format code
make vet          # Run go vet
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development instructions.

## Architecture Overview

IAF consists of three binaries:

- **API Server** (`cmd/apiserver`) — serves the MCP endpoint (`/mcp`) and REST API (`/api/v1/`); manages sessions; stores source code tarballs
- **Controller** (`cmd/controller`) — Kubernetes controller that reconciles `Application` CRs into Deployments, Services, Traefik IngressRoutes, cert-manager Certificates, and kpack Images
- **MCP Server** (`cmd/mcpserver`) — standalone STDIO MCP server for local development

The platform uses two custom CRDs:
- **`Application`** (`iaf.io/v1alpha1`) — represents a deployed application; created by agents via MCP tools
- **`DataSource`** (`iaf.io/v1alpha1`, cluster-scoped) — represents an organisational data source; registered by operators via `kubectl`

See [docs/architecture.md](docs/architecture.md) for the full architecture.

## License

MIT License — see [LICENSE](LICENSE) for details.
