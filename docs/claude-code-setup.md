# Using IAF with Claude Code

## Quick Setup

IAF runs entirely in Kubernetes. The MCP server is embedded in the API server and exposed as a Streamable HTTP endpoint.

### 1. Deploy the platform

Make sure the IAF platform is running in your cluster:

```bash
kubectl get pods -n iaf-system
```

You should see `iaf-apiserver` and `iaf-controller` pods running. If not, deploy them:

```bash
# Build the platform image
cd /Users/diego/src/iaf2
nerdctl build -t iaf-platform:latest .
nerdctl save iaf-platform:latest | nerdctl --namespace k8s.io load

# Apply the platform manifests
kubectl apply -f config/deploy/platform.yaml
```

### 2. Add the MCP server to Claude Code

The MCP server is available at `http://iaf.localhost/mcp` via Traefik. Add it to Claude Code:

```bash
claude mcp add --transport http iaf http://iaf.localhost/mcp
```

### 3. Verify

Start a new Claude Code session and ask it to list apps:

```
> list my deployed applications
```

Claude will use the `list_apps` MCP tool and show you results.

## Available Tools

Once configured, Claude Code has access to these tools:

| Tool | What to say | Example |
|------|------------|---------|
| `deploy_app` | "Deploy nginx" | "Deploy an nginx app called my-site on port 80" |
| `push_code` | "Deploy this code" | "Create a Node.js hello world app and deploy it" |
| `app_status` | "Check status" | "What's the status of my-site?" |
| `app_logs` | "Show logs" | "Show me the logs for my-site" |
| `list_apps` | "List apps" | "What apps are deployed?" |
| `delete_app` | "Delete app" | "Delete the my-site app" |

## Example Conversations

### Deploy a pre-built image

```
You: Deploy nginx as "marketing-site" on port 80
Claude: [uses deploy_app] Created "marketing-site". It will be available at http://marketing-site.localhost
```

### Push source code directly

```
You: Create a simple Express.js app that returns "hello world" and deploy it as "hello-api"
Claude: [writes code, uses push_code] Source code uploaded and build started for "hello-api".
         The platform will auto-detect Node.js and build a container. It will be available
         at http://hello-api.localhost once the build completes.
```

### Check on your apps

```
You: What's running right now?
Claude: [uses list_apps] You have 2 applications deployed:
        - marketing-site: Running at http://marketing-site.localhost
        - hello-api: Building (kpack is building the container)
```

## Architecture

```
Claude Code --HTTP--> http://iaf.localhost/mcp --Traefik--> iaf-apiserver (in K8s)
                                                               |
                                                         Application CR
                                                               |
                                                    iaf-controller (in K8s)
                                                               |
                                              Deployment + Service + IngressRoute
```

All components run in the `iaf-system` namespace. Apps are deployed to `iaf-apps`.

## Troubleshooting

**"application not found"** — Check the platform is running: `kubectl get pods -n iaf-system`

**Build stuck in "Building"** — Check kpack is running: `kubectl get pods -n kpack`

**Can't reach app URL** — Make sure Traefik is running: `kubectl get pods -n kube-system -l app.kubernetes.io/name=traefik`

**MCP connection fails** — Verify the endpoint: `curl http://iaf.localhost/health`

**Rebuilding after code changes** — Rebuild and redeploy:
```bash
cd /Users/diego/src/iaf2
nerdctl build -t iaf-platform:latest .
nerdctl save iaf-platform:latest | nerdctl --namespace k8s.io load
kubectl rollout restart deployment/iaf-apiserver deployment/iaf-controller -n iaf-system
```
