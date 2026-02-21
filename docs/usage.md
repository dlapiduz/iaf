# Using IAF

This guide covers how to use IAF once the platform is deployed and running in Kubernetes.

## Prerequisites

- IAF is deployed in your cluster (see the [Operator Guide](operator-guide.md))
- The `iaf-apiserver` and `iaf-controller` pods are running in `iaf-system`
- Traefik is routing `iaf.localhost` to the API server

Verify the platform is healthy:

```bash
curl http://iaf.localhost/health
# {"status":"ok"}
```

---

## Connecting Claude Code to IAF

The API server exposes an MCP server at `http://iaf.localhost/mcp` via Streamable HTTP. Every request requires a Bearer token.

### Add the MCP server

```bash
claude mcp add --transport http iaf http://iaf.localhost/mcp
```

Then add the authorization header. In your Claude Code MCP config (`.mcp.json` or `~/.claude.json`):

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

### Alternative: STDIO transport (local development)

For developing IAF itself, run the MCP server as a local process:

```bash
make build
claude mcp add --transport stdio iaf -- ./bin/mcpserver
```

This requires a valid kubeconfig on the local machine.

### Verify the connection

Start Claude Code and confirm the tools are available:

```
> /mcp
```

You should see `iaf` listed with all available tools.

---

## MCP Tools

All tools (except `register`) require a `session_id` obtained from the `register` tool.

### Session management

| Tool | Description |
|------|-------------|
| `register` | **Call this first.** Creates an isolated session and returns a `session_id` required by all other tools |

### Deployment tools

| Tool | Description |
|------|-------------|
| `deploy_app` | Deploy from a container image (`image`), git repository (`git_url`), or source upload. Optional: `git_credential` for private repos |
| `push_code` | Upload source code files as a map of `{"path": "content"}` — the platform auto-detects the language and builds a container |

### Monitoring tools

| Tool | Description |
|------|-------------|
| `app_status` | Current phase, URL, build status, replica count |
| `app_logs` | Application logs or build logs (`build_logs: true`) |
| `list_apps` | List all apps in your session (optional `status` filter) |

### Lifecycle tools

| Tool | Description |
|------|-------------|
| `delete_app` | Delete an application and all its resources |

### Git credential tools (for private repositories)

| Tool | Description |
|------|-------------|
| `add_git_credential` | Store a git credential (basic-auth or SSH) for use with `deploy_app` |
| `list_git_credentials` | List stored credentials (names and metadata only — no secret values) |
| `delete_git_credential` | Remove a stored credential |

### Data source tools

| Tool | Description |
|------|-------------|
| `list_data_sources` | List platform data sources (databases, APIs, etc.). Optional `kind` and `tags` filters |
| `get_data_source` | Get details about a specific data source: kind, schema, env var names |
| `attach_data_source` | Attach a data source to your app — credentials injected as env vars into the container |

---

## MCP Prompts

Prompts provide narrative guidance that helps agents write correct, deployable code.

| Prompt | Description |
|--------|-------------|
| `deploy-guide` | Full deployment workflow — methods, lifecycle phases, naming rules, security, scaling |
| `language-guide` | Per-language buildpack guide. Pass `language` argument: `go`, `nodejs`, `python`, `java`, `ruby` |
| `coding-guide` | Organisation coding standards. Pass optional `language` argument |
| `scaffold-guide` | Application scaffolding patterns and templates |

---

## MCP Resources

Resources provide structured, machine-readable data.

| Resource | URI | Description |
|----------|-----|-------------|
| `platform-info` | `iaf://platform` | Platform config JSON — supported languages, routing, build defaults |
| `language-spec` | `iaf://languages/{language}` | Buildpack spec for a language — detection files, required structure, env vars |
| `application-spec` | `iaf://schema/application` | Application CRD field reference — all spec/status fields and constraints |
| `org-coding-standards` | `iaf://org/coding-standards` | Machine-readable organisation coding standards |
| `data-catalog` | `iaf://catalog/data-sources` | JSON index of all registered data sources (no credential data) |

---

## Example Workflows

### Deploy a Go app from scratch

```
Build and deploy a simple Go web server called "hello-go" that responds
with "Hello, World!" on the root path and has a /health endpoint.
```

Claude will:
1. Read the `language-guide` prompt for Go
2. Write `go.mod` and `main.go` with the correct buildpack structure
3. Call `push_code` to upload the files
4. Monitor progress with `app_status`
5. Report the URL once the app is running at `https://hello-go.example.com`

### Deploy from a container image

```
Deploy nginx:alpine as an app called "webserver" on port 80.
```

### Deploy from a git repository

```
Deploy my app from https://github.com/myorg/myapp, call it "myapp".
```

### Deploy from a private git repository

```
Deploy from https://github.com/myorg/private-repo. Use my stored credential "github-creds".
```

If the credential doesn't exist yet:
```
Store my GitHub token (ghp_xxx) for https://github.com as "github-creds",
then deploy https://github.com/myorg/private-repo as "myapp".
```

### Check on a running app

```
What's the status of hello-go? Show me the last 50 log lines.
```

### Use a data source

If your platform operator has registered a PostgreSQL data source called `prod-postgres`:

```
List available data sources, then attach prod-postgres to my app "api-server".
```

Claude will call `list_data_sources`, then `attach_data_source`, which:
- Copies the credentials into your session namespace
- Patches the `api-server` Application CR
- Triggers a rolling restart so the new env vars (`POSTGRES_HOST`, `POSTGRES_PASSWORD`, etc.) are available

---

## Application Lifecycle

```
Pending → Building → Deploying → Running
                ↘        ↘
                  Failed ←
```

| Phase | Description |
|-------|-------------|
| **Pending** | Application CR created, controller hasn't processed it yet |
| **Building** | kpack is building source code into a container image (git/blob sources only) |
| **Deploying** | Deployment and IngressRoute being created; pods not yet ready |
| **Running** | ≥1 replica available, traffic is being served |
| **Failed** | Build or deployment error — check `app_status` or `app_logs` |

---

## Supported Languages

The platform uses Cloud Native Buildpacks (Paketo, Jammy LTS / Ubuntu 22.04) to auto-detect and build applications.

| Language | Detection file(s) |
|----------|-------------------|
| Go | `go.mod` |
| Node.js | `package.json` |
| Python | `requirements.txt`, `setup.py`, `pyproject.toml` |
| Java | `pom.xml`, `build.gradle`, `build.gradle.kts` |
| Ruby | `Gemfile` |

Containers run as non-root by default.

---

## Networking and TLS

- Apps are exposed at `https://<name>.<base-domain>` via Traefik
- **TLS is on by default** — cert-manager issues a certificate for each app automatically
- Build logs visible during `Building` phase via `app_logs` with `build_logs: true`
- Default container port: 8080 — your app must listen on this port unless you set a different `port`
- Recommendation: implement a `/health` or `/healthz` endpoint for Kubernetes readiness probes

---

## REST API

The API server also exposes a REST API for non-MCP clients (dashboards, CI/CD, scripts).

All REST endpoints require `Authorization: Bearer <token>`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (no auth) |
| `GET` | `/ready` | Readiness check (no auth) |
| `GET` | `/api/v1/applications` | List all applications |
| `POST` | `/api/v1/applications` | Create an application |
| `GET` | `/api/v1/applications/:name` | Get application details |
| `PUT` | `/api/v1/applications/:name` | Update an application |
| `DELETE` | `/api/v1/applications/:name` | Delete an application |
| `POST` | `/api/v1/applications/:name/source` | Upload source code |
| `GET` | `/api/v1/applications/:name/logs` | Get application logs |
| `GET` | `/api/v1/applications/:name/build` | Get build logs |

### Examples

```bash
# List applications
curl -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications

# Deploy from image
curl -X POST -H "Authorization: Bearer iaf-dev-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"webserver","image":"nginx:alpine","port":80}' \
  http://iaf.localhost/api/v1/applications

# Check status
curl -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications/webserver

# Delete
curl -X DELETE -H "Authorization: Bearer iaf-dev-key" http://iaf.localhost/api/v1/applications/webserver
```

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Tools not appearing in Claude | Check `/mcp` in Claude Code; verify `Authorization` header is set |
| App stuck in `Building` | `app_logs` with `build_logs: true` to see kpack output |
| App stuck in `Deploying` | Check `app_status` — pods may be in image pull error or crash loop |
| `git_credential not found` | The credential name passed to `deploy_app` must match one returned by `list_git_credentials` |
| `data source not found` | Use `list_data_sources` to see what's registered on your platform |
| `env var X already defined` | The data source you're attaching shares an env var name with `app.Spec.Env` or another attached source |
| TLS certificate not working | Verify cert-manager is installed and the ClusterIssuer exists: `kubectl get clusterissuers` |
