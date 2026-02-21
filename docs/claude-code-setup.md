# Claude Code Setup Guide

> **Who this is for:** A human who has access to a running IAF platform and wants to configure their Claude Code instance to use it.
>
> This is *not* documentation for Claude itself — Claude receives its own instructions automatically from the platform once connected. This guide is for you, the human doing the setup.

---

## What you're setting up

Once configured, your Claude Code instance will have access to IAF's MCP tools. You'll be able to give Claude natural language instructions like:

```
> Create a Go web server and deploy it as "hello-go"
> What's the status of my apps?
> Show me the logs for hello-go
```

Claude will handle everything: writing code, deploying containers, monitoring builds, and reporting the live URL. You don't need to know Kubernetes.

---

## Prerequisites

- Claude Code is installed on your machine ([install guide](https://docs.anthropic.com/claude-code))
- IAF is deployed and reachable (ask your platform operator for the URL and token)
- The platform is healthy — verify with:
  ```bash
  curl http://iaf.localhost/health
  # should return: {"status":"ok"}
  ```

---

## Step 1 — Register the MCP server

Run this once to add IAF to Claude Code:

```bash
claude mcp add --transport http iaf http://iaf.localhost/mcp
```

Replace `http://iaf.localhost/mcp` with your platform's URL if it's hosted elsewhere.

---

## Step 2 — Add your auth token

The platform requires a Bearer token on every request. Open your MCP config file and add the `headers` field.

The config file is either:
- `.mcp.json` in your current project directory (project-scoped), or
- `~/.claude.json` (global, applies to all projects)

Edit the `iaf` entry to look like this:

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

Replace `iaf-dev-key` with the token your operator gave you.

> **Without this header, every tool call will fail with an authentication error.** This is the most common setup mistake.

---

## Step 3 — Verify the connection

Start a new Claude Code session and run:

```
> /mcp
```

You should see `iaf` listed with a set of tools including `register`, `push_code`, `deploy_app`, and others. If it doesn't appear, check the troubleshooting section below.

---

## Step 4 — Deploy something

Just ask Claude in plain English. A good first test:

```
> Deploy nginx as "test-app" on port 80
```

Claude will call the IAF tools automatically. You'll see it create a session, deploy the app, and return a URL. The whole thing takes about 30 seconds for a pre-built image.

For a source code build (longer, ~2 minutes):

```
> Create a simple Go web server that returns "hello world" on / and deploy it as "hello-go"
```

---

## How it works behind the scenes

When Claude connects to IAF, the platform automatically sends it:

- **Server instructions** — a system-level briefing telling Claude what tools are available, that it must call `register` first, and key constraints (default port 8080, etc.)
- **Prompts** — on-demand guides Claude can read (e.g. `deploy-guide`, `language-guide` for Go/Node.js/Python)
- **Resources** — structured data Claude can read (e.g. `iaf://platform` for platform config, `iaf://org/coding-standards` for your org's coding rules)

You don't need to set any of this up — it's built into the platform. Claude learns how to use IAF the moment it connects.

---

## What Claude can do

| Task | What to say |
|------|-------------|
| Deploy a container image | "Deploy nginx:alpine as 'web' on port 80" |
| Deploy from source code | "Create a Python Flask app and deploy it as 'api'" |
| Deploy from a git repo | "Deploy https://github.com/myorg/myapp as 'myapp'" |
| Deploy from a private repo | "Store my GitHub token as 'github-creds', then deploy…" |
| Check status | "What's the status of hello-go?" |
| View logs | "Show me the logs for hello-go" |
| List apps | "What apps are running?" |
| Delete an app | "Delete the test-app" |
| Use a database | "List available data sources, then attach prod-postgres to api" |

---

## Troubleshooting

**`iaf` not listed in `/mcp`**
The `claude mcp add` command may not have run, or it added to the wrong config file. Run it again and check `~/.claude.json`.

**All tool calls fail / "unauthorized"**
The `Authorization` header is missing or wrong. Double-check the `headers` section in your MCP config file and make sure the token matches what the operator configured.

**"connection refused" or MCP endpoint unreachable**
The platform isn't reachable from your machine. Verify: `curl http://iaf.localhost/health`. If that fails, check with your operator that Traefik is routing correctly.

**App stuck in `Building` for more than 5 minutes**
Ask Claude: `"Show me the build logs for <app-name>"` — it will call `app_logs` with build mode and surface the kpack error.

**App stuck in `Deploying`**
Usually an image pull error or crash loop. Ask Claude: `"What's wrong with <app-name>?"` and it will check the pod status.
