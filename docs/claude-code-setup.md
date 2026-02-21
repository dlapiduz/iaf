# Connecting Claude Code to IAF

This guide walks through getting a Claude Code instance connected to the IAF platform so it can deploy and manage applications on Kubernetes.

## Prerequisites

- IAF is deployed and healthy in your cluster (see [Operator Guide](operator-guide.md))
- The platform is reachable at `http://iaf.localhost` (or your configured domain)
- You have a Bearer token (default dev token: `iaf-dev-key`)

Verify the platform is up:

```bash
curl http://iaf.localhost/health
# {"status":"ok"}
```

---

## Step 1: Add the MCP server to Claude Code

Run this command to register the IAF MCP server:

```bash
claude mcp add --transport http iaf http://iaf.localhost/mcp
```

## Step 2: Add the auth header

Every request to the MCP endpoint requires a Bearer token. Open your Claude Code MCP config — either `.mcp.json` in your project directory or `~/.claude.json` — and add the `headers` field:

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

Replace `iaf-dev-key` with your token if the operator has configured a different one.

## Step 3: Verify the connection

Start a new Claude Code session and run:

```
> /mcp
```

You should see `iaf` listed with all available tools (`register`, `push_code`, `deploy_app`, etc.).

---

## Step 4: Start using IAF

The first thing Claude must do in any session is call `register` to get a `session_id`. All other tools require it. Claude will do this automatically when you ask it to deploy something — just give it a task:

```
> Create a Go hello world web server and deploy it as "hello-go"
```

Claude will:
1. Call `register` to create an isolated session (your own Kubernetes namespace)
2. Write the Go source code
3. Call `push_code` to upload it — the platform auto-detects Go and builds a container
4. Poll `app_status` until the build completes
5. Report the URL once the app is running

---

## What Claude can do

Once connected, Claude has access to these tools:

| Tool | Description |
|------|-------------|
| `register` | Creates a session — Claude calls this automatically |
| `push_code` | Upload source files and auto-build/deploy (Go, Node.js, Python, Java, Ruby) |
| `deploy_app` | Deploy from a container image or git repository |
| `app_status` | Check build/deploy progress and get the app URL |
| `app_logs` | View application logs or build logs |
| `list_apps` | List all deployed apps in the session |
| `delete_app` | Delete an app and all its resources |
| `add_git_credential` | Store a git credential for private repo access |
| `list_git_credentials` | List stored credentials (no secrets returned) |
| `delete_git_credential` | Remove a stored credential |
| `list_data_sources` | Discover platform data sources (databases, APIs) |
| `get_data_source` | Get details about a data source |
| `attach_data_source` | Attach a data source — injects credentials as env vars into the app |

---

## Example prompts

### Deploy from source

```
Create a Python Flask app that returns {"status": "ok"} on GET /health and deploy it as "health-api".
```

### Deploy a container image

```
Deploy nginx:alpine as "web" on port 80.
```

### Deploy from a private git repo

```
Store my GitHub token ghp_xxxx for https://github.com as "github-creds",
then deploy https://github.com/myorg/myapp as "myapp".
```

### Check on apps

```
What apps are running? Show me the logs for hello-go.
```

### Use a data source

```
List available data sources, then attach prod-postgres to my app "api-server".
```

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `iaf` not listed in `/mcp` | Check the `Authorization` header is set in your MCP config |
| All tool calls fail with auth error | Verify the token matches `IAF_API_TOKENS` in the platform config |
| App stuck in `Building` | Ask Claude: "Show me the build logs for \<app\>" — it will call `app_logs` with `build_logs: true` |
| App stuck in `Deploying` | Check pod events: `kubectl get pods -n iaf-<session-id>` |
| Can't reach the app URL | Verify Traefik is running: `kubectl get pods -A -l app.kubernetes.io/name=traefik` |
| MCP endpoint unreachable | `curl http://iaf.localhost/health` — check Traefik IngressRoute: `kubectl get ingressroute -n iaf-system` |
