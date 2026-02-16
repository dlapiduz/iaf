# Using IAF

This guide covers how to use IAF once the platform is deployed and running in Kubernetes.

## Prerequisites

- IAF is deployed in K3s (see installation docs)
- The `iaf-apiserver`, `iaf-controller`, and kpack are running in the `iaf-system` namespace
- Traefik is routing `iaf.localhost` to the apiserver

Verify the platform is healthy:

```bash
curl http://iaf.localhost/health
# {"status":"ok"}
```

## Connecting Claude Code to IAF

The apiserver exposes an MCP server at `http://iaf.localhost/mcp` via Streamable HTTP. No authentication is required for the MCP endpoint.

### Add the MCP server

Run this from any project directory where you want Claude to have access to IAF:

```bash
claude mcp add --transport http iaf http://iaf.localhost/mcp
```

Or create a `.mcp.json` in your project root to share the config with your team:

```json
{
  "mcpServers": {
    "iaf": {
      "type": "http",
      "url": "http://iaf.localhost/mcp"
    }
  }
}
```

### Alternative: STDIO transport (local development)

If you're developing IAF itself and want to run the MCP server as a local process:

```bash
make build
claude mcp add --transport stdio iaf -- ./bin/mcpserver
```

This requires a valid kubeconfig on the local machine. The mcpserver reads the same `IAF_*` environment variables as the apiserver (see [Configuration](#configuration) below).

### Verify the connection

Start Claude Code and check that the IAF tools are available:

```
> /mcp

# You should see "iaf" listed with 6 tools, 2 prompts, and 3 resources
```

## What Claude Can Do

Once connected, Claude has access to tools, prompts, and resources through the MCP protocol.

### Tools (actions)

| Tool | Description |
|------|-------------|
| `deploy_app` | Deploy an application from a container image or git repository |
| `push_code` | Upload source code files and deploy — the platform auto-detects the language and builds a container |
| `app_status` | Get the current status of an application (phase, URL, build status, replicas) |
| `app_logs` | Get application or build logs |
| `list_apps` | List all deployed applications |
| `delete_app` | Delete an application and all its resources |

### Prompts (guidance)

Prompts provide narrative context that helps Claude write correct, deployable code on the first try.

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `deploy-guide` | none | Comprehensive deployment workflow — methods, lifecycle phases, naming constraints, security considerations |
| `language-guide` | `language` (required) | Per-language buildpack guide — required files, minimal working example, detection rules, best practices. Supports: go, nodejs, python, java, ruby (with aliases like "golang", "node", "py", "rb") |

### Resources (structured data)

Resources provide machine-readable data that Claude can use for precise decision-making.

| Resource | URI | Description |
|----------|-----|-------------|
| `platform-info` | `iaf://platform` | Platform configuration as JSON — supported languages, build stack, deployment methods, defaults, routing |
| `language-spec` | `iaf://languages/{language}` | Structured language data — buildpack ID, detection files, required/recommended files, environment variables |
| `application-spec` | `iaf://schema/application` | Application CRD field reference — all spec/status fields, types, defaults, constraints |

## Example Workflows

### Deploy a Go app from scratch

Ask Claude:

```
Build and deploy a simple Go web server called "hello-go" that responds with
"Hello, World!" on the root path and has a /health endpoint.
```

Claude will:
1. Read the `language-guide` prompt for Go to understand buildpack requirements
2. Write `go.mod` and `main.go` with the correct structure
3. Use `push_code` to upload the source files
4. The platform auto-builds via kpack and deploys
5. The app becomes available at `http://hello-go.localhost`

### Deploy from a container image

```
Deploy nginx as an app called "webserver" using the nginx:alpine image on port 80.
```

### Deploy from a git repository

```
Deploy my app from https://github.com/user/myapp.git, call it "myapp".
```

### Check on a running app

```
What's the status of hello-go? Show me its logs.
```

### Scale an application

```
Scale hello-go to 3 replicas.
```

## REST API

The apiserver also exposes a REST API for non-MCP clients (dashboards, CI/CD, scripts). REST endpoints require a Bearer token (default: `iaf-dev-key`).

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (no auth) |
| `GET` | `/ready` | Readiness check (no auth) |
| `GET` | `/api/v1/applications` | List all applications |
| `POST` | `/api/v1/applications` | Create an application |
| `GET` | `/api/v1/applications/:name` | Get application details |
| `PUT` | `/api/v1/applications/:name` | Update an application |
| `DELETE` | `/api/v1/applications/:name` | Delete an application |
| `POST` | `/api/v1/applications/:name/source` | Upload source code (JSON or tarball) |
| `GET` | `/api/v1/applications/:name/logs` | Get application logs |
| `GET` | `/api/v1/applications/:name/build` | Get build logs |

### Examples

```bash
# List applications
curl -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications

# Deploy from image
curl -X POST -H "Authorization: Bearer iaf-dev-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"nginx","image":"nginx:alpine","port":80}' \
  http://iaf.localhost/api/v1/applications

# Deploy from git
curl -X POST -H "Authorization: Bearer iaf-dev-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"myapp","gitUrl":"https://github.com/user/myapp.git"}' \
  http://iaf.localhost/api/v1/applications

# Check status
curl -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications/myapp

# Upload source code
curl -X POST -H "Authorization: Bearer iaf-dev-key" \
  -H "Content-Type: application/json" \
  -d '{"files":{"main.go":"package main\n\nimport \"net/http\"\n\nfunc main() { http.ListenAndServe(\":8080\", nil) }","go.mod":"module myapp\ngo 1.22"}}' \
  http://iaf.localhost/api/v1/applications/myapp/source

# View logs
curl -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications/myapp/logs?lines=50

# Delete
curl -X DELETE -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications/myapp
```

## Application Lifecycle

```
Pending → Building → Deploying → Running
                ↘        ↘         ↓
                  Failed ← ← ← ← ←
```

| Phase | Description |
|-------|-------------|
| **Pending** | Application CR created, waiting for the controller to process it |
| **Building** | kpack is building source code into a container image (git/blob sources only) |
| **Deploying** | Kubernetes Deployment and Traefik IngressRoute are being created or updated |
| **Running** | Pods are ready, traffic is being served at `http://<name>.localhost` |
| **Failed** | Build or deployment failed — check `app_status` or `app_logs` for details |

## Supported Languages

The platform uses Cloud Native Buildpacks (Paketo) to automatically detect and build applications from source code.

| Language | Detection Files | Buildpack |
|----------|----------------|-----------|
| Go | `go.mod` | `paketo-buildpacks/go` |
| Node.js | `package.json` | `paketo-buildpacks/nodejs` |
| Python | `requirements.txt`, `setup.py`, or `pyproject.toml` | `paketo-buildpacks/python` |
| Java | `pom.xml`, `build.gradle`, or `build.gradle.kts` | `paketo-buildpacks/java` |
| Ruby | `Gemfile` | `paketo-buildpacks/ruby` |

All builds use the Paketo Jammy LTS stack (Ubuntu 22.04). Containers run as non-root by default.

## Configuration

The platform is configured via environment variables with the `IAF_` prefix.

| Variable | Default | Description |
|----------|---------|-------------|
| `IAF_API_PORT` | `8080` | API server listen port |
| `IAF_API_KEY` | `iaf-dev-key` | Bearer token for REST API authentication |
| `IAF_NAMESPACE` | `iaf-apps` | Kubernetes namespace for deployed applications |
| `IAF_BASE_DOMAIN` | `localhost` | Base domain for app routing (`<name>.<base_domain>`) |
| `IAF_KUBECONFIG` | (default) | Path to kubeconfig file |
| `IAF_CLUSTER_BUILDER` | `iaf-cluster-builder` | kpack ClusterBuilder name |
| `IAF_REGISTRY_PREFIX` | `registry.localhost:5000/iaf` | Container registry prefix for built images |
| `IAF_SOURCE_STORE_DIR` | `/tmp/iaf-sources` | Directory for source code tarballs |
| `IAF_SOURCE_STORE_URL` | `http://iaf-source-store.iaf-system.svc.cluster.local` | URL for kpack to fetch source tarballs |
