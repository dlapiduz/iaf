# IAF Architecture

IAF (Intelligent Application Fabric) is a Kubernetes-native platform purpose-built for AI agents to deploy, run, and manage applications. This document explains how the system works end-to-end.

## Design Philosophy

**Agentic-first.** The primary users are AI agents, not humans. Every API, tool description, and error message is written to be unambiguous and self-recoverable without human intervention.

**Multi-tenancy at every layer.** Multiple agents (and the humans behind them) share the same cluster. Namespace isolation is a core invariant, not a feature.

**Security non-negotiable.** A compromised or misbehaving agent must not be able to affect other tenants. Credentials never appear in tool output. Cross-namespace access is minimised and explicitly scoped.

---

## Components

```
                         ┌──────────────────────────────────┐
                         │           AI Agent                │
                         │    (Claude Code or other MCP      │
                         │         client)                   │
                         └─────────────┬────────────────────┘
                                       │ MCP over HTTP
                                       │ Bearer token auth
                                       ▼
┌────────────────────────────────────────────────────────────┐
│                      iaf-system namespace                  │
│                                                            │
│  ┌──────────────────────────────┐                          │
│  │        API Server            │  also exposes REST API   │
│  │   (cmd/apiserver)            │  for dashboards/scripts  │
│  │                              │                          │
│  │  • MCP server (embedded)     │                          │
│  │  • Session management        │                          │
│  │  • Source store endpoint     │                          │
│  └──────────────┬───────────────┘                          │
│                 │ creates/updates Application CRs          │
│                 ▼                                          │
│  ┌──────────────────────────────┐                          │
│  │        Controller            │                          │
│  │   (cmd/controller)           │                          │
│  │                              │                          │
│  │  • Watches Application CRs   │                          │
│  │  • Reconciles K8s resources  │                          │
│  └──────────────┬───────────────┘                          │
└─────────────────┼──────────────────────────────────────────┘
                  │ creates/manages
                  ▼
┌────────────────────────────────────────────────────────────┐
│               Agent session namespace (iaf-<id>)           │
│                                                            │
│   Deployment  ──► Pods                                     │
│   Service                                                  │
│   IngressRoute (Traefik) ──► https://<name>.<domain>       │
│   Certificate (cert-manager)                               │
│   Secrets (git credentials, copied data source creds)      │
│   kpack Image CR (for source builds)                       │
└────────────────────────────────────────────────────────────┘
```

### API Server (`cmd/apiserver`)

The entry point for all external traffic. It serves:
- **MCP endpoint** at `/mcp` — Streamable HTTP, requires Bearer token
- **REST API** at `/api/v1/` — for dashboards and non-MCP clients
- **Source store** — stores uploaded source code tarballs for kpack

The MCP server is embedded in the API server process. All MCP tools resolve the agent's session to a namespace and operate only within that namespace.

### Controller (`cmd/controller`)

A standard Kubernetes controller (controller-runtime) that watches `Application` CRs and reconciles the desired state into actual Kubernetes resources. Per reconcile loop:

1. Resolve image — either from `spec.image` (immediate) or kpack Image CR status (wait for build)
2. Transition to `Deploying` phase
3. Create/update `Deployment`
4. Create/update `Service`
5. Create/update cert-manager `Certificate` (when TLS is enabled and issuer is configured)
6. Create/update Traefik `IngressRoute`
7. Update `Application` status (phase, URL, available replicas)

### MCP Server (`cmd/mcpserver`)

A standalone STDIO-based MCP server for local development. Uses the same tool/prompt/resource implementations as the API server but connects via the local kubeconfig instead of in-cluster credentials.

---

## Custom Resources

### Application (`iaf.io/v1alpha1`)

The central resource. Represents a deployed application.

```yaml
spec:
  image: nginx:latest          # pre-built image (mutually exclusive with git/blob)
  git:
    url: https://github.com/…  # git repo to build from
    revision: main
  blob: https://…/source.tar  # uploaded source tarball URL (set by push_code)
  port: 8080                   # container port
  replicas: 1
  env:                         # literal env vars
    - name: FOO
      value: bar
  tls:
    enabled: true              # defaults to true; set false to opt out of HTTPS
  attachedDataSources:         # set by attach_data_source tool
    - dataSourceName: prod-postgres
      secretName: iaf-ds-prod-postgres

status:
  phase: Running               # Pending | Building | Deploying | Running | Failed
  url: https://myapp.example.com
  latestImage: registry.../myapp@sha256:…
  buildStatus: Succeeded
  availableReplicas: 1
  conditions: […]
```

**Lifecycle phases:**

| Phase | Meaning |
|-------|---------|
| `Pending` | CR created, controller hasn't processed it yet |
| `Building` | kpack is building source into a container image |
| `Deploying` | Deployment and IngressRoute being created/updated; pods not yet ready |
| `Running` | ≥1 replica available, traffic being served |
| `Failed` | Build or deployment error — check `app_status` or `app_logs` |

### DataSource (`iaf.io/v1alpha1`, cluster-scoped)

Registered by platform operators (never by agents). Represents a platform-managed data source (database, API, etc.) that agents can discover and attach to their applications.

```yaml
spec:
  kind: postgres               # postgres | mysql | s3 | http-api | kafka | …
  description: "Production read replica"
  schema: "Tables: users, orders"
  tags: [production, sql]
  secretRef:
    name: prod-postgres-creds  # operator-created Secret
    namespace: iaf-system
  envVarMapping:               # secret key → env var name in the app container
    host: POSTGRES_HOST
    port: POSTGRES_PORT
    password: POSTGRES_PASSWORD
```

---

## Session Model

When an agent calls `register`:
1. A session record is created with a unique ID
2. A Kubernetes namespace `iaf-<short-id>` is provisioned
3. A kpack ServiceAccount (`iaf-kpack-sa`) is created in that namespace
4. All subsequent tool calls must include the `session_id` — it maps to the namespace

**Namespace isolation is the security boundary.** Agents can only create, read, and modify resources in their own namespace. The MCP tools enforce this on every call.

---

## Deployment Methods

### 1. Container Image

Agent calls `deploy_app` with `image: nginx:latest`. The controller creates a Deployment immediately — no build step.

### 2. Git Repository

Agent calls `deploy_app` with `git_url`. The controller creates a kpack `Image` CR, which triggers a Cloud Native Buildpack build. The `Application` stays in `Building` phase until kpack reports a successful image. For private repos, agents first call `add_git_credential` to store credentials as a Kubernetes Secret referenced by the kpack ServiceAccount.

### 3. Source Upload

Agent calls `push_code` with a map of file paths to contents. The platform packages the files as a tarball, stores it, and creates/updates the `Application` with a `blob` source URL. kpack fetches and builds from the tarball.

---

## Build System (kpack)

IAF uses [kpack](https://github.com/buildpacks-community/kpack) with [Cloud Native Buildpacks](https://buildpacks.io/) (Paketo) to build source into OCI images automatically.

- **Stack**: Paketo Jammy LTS (Ubuntu 22.04)
- **Containers**: run as non-root by default (buildpack behaviour)
- **Language detection**: automatic based on detection files

| Language | Detection file(s) |
|----------|-------------------|
| Go | `go.mod` |
| Node.js | `package.json` |
| Python | `requirements.txt`, `setup.py`, `pyproject.toml` |
| Java | `pom.xml`, `build.gradle`, `build.gradle.kts` |
| Ruby | `Gemfile` |

Built images are pushed to the configured registry prefix. The controller watches kpack `Image` CRs to detect build completion.

---

## Networking and TLS

Applications are exposed via [Traefik](https://traefik.io/) `IngressRoute` CRs. The hostname is `<app-name>.<base-domain>`.

**TLS is enabled by default.** When the controller has a `TLSIssuer` configured, it creates a cert-manager `Certificate` CR and routes traffic on the `websecure` (HTTPS) entrypoint. Without `TLSIssuer` (cert-manager not installed), the controller degrades gracefully to HTTP.

To opt a specific application out of TLS:
```yaml
spec:
  tls:
    enabled: false
```

---

## Auth and Security

### API Authentication
Every request to the MCP or REST API must include a Bearer token:
```
Authorization: Bearer <token>
```
Tokens are configured via `IAF_API_TOKENS` (comma-separated). The default dev token is `iaf-dev-key` — **change this in production**.

### Session Isolation
- Each session → one namespace. Tools enforce namespace resolution from session ID on every call.
- No tool can read from or write to another session's namespace.
- Shared cluster-scoped resources (DataSources, ClusterBuilders) are read-only for agents.

### Credential Handling
- Git credentials stored as `kubernetes.io/basic-auth` or `kubernetes.io/ssh-auth` Secrets with label `iaf.io/credential-type=git`
- Data source credentials copied from `iaf-system` into session namespace at attach time; never returned in tool output
- All tool output is scrubbed of credential values; tests explicitly assert this

### RBAC
- Controller has a ClusterRole for managing Application, DataSource, Deployment, Service, kpack Image, Traefik IngressRoute, and cert-manager Certificate resources
- Cross-namespace Secret access (for data source credential copying) is granted via a namespace-scoped Role in `iaf-system` — not a cluster-wide ClusterRole on Secrets

---

## Data Flow: agent deploys a Go app from source

```
1. Agent: push_code(session_id, "hello-go", files)
   → API server stores tarball, creates/updates Application CR
     spec.blob = "http://source-store/…/hello-go.tar"

2. Controller: reconciles Application
   → creates kpack Image CR with blob source
   → sets status.phase = Building

3. kpack: detects Go (go.mod), runs Paketo Go buildpack
   → pushes image to registry.example.com/iaf-<ns>/hello-go

4. Controller: kpack Image status shows latestImage
   → creates Deployment (image = registry.../hello-go@sha256:…)
   → creates Service
   → creates cert-manager Certificate (if TLS enabled)
   → creates Traefik IngressRoute (websecure entrypoint)
   → sets status.phase = Deploying, status.url = https://hello-go.example.com

5. Pods become ready
   → Controller sets status.phase = Running, availableReplicas = 1

6. Agent: app_status(session_id, "hello-go") → Running
   Agent: app_status returns url = https://hello-go.example.com
```
