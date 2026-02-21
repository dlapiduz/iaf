# IAF Operator Guide

This guide covers everything a platform operator needs to install, configure, and run IAF in a Kubernetes cluster.

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Kubernetes cluster | Rancher Desktop, k3s, GKE, EKS, AKS — any standard distribution |
| [kpack](https://github.com/buildpacks-community/kpack) | For building applications from source |
| [Traefik](https://traefik.io/) | Ingress controller |
| Container registry | Accessible from the cluster (default: `registry.localhost:5000`) |
| [cert-manager](https://cert-manager.io/) | **Optional.** Required for HTTPS/TLS provisioning |

---

## Installation

### 1. One-time cluster setup

Run the setup script to create namespaces, install CRDs, configure RBAC, and prepare kpack:

```bash
make setup-local   # creates namespaces, registry, CRDs, RBAC, kpack service account
make setup-kpack   # installs kpack and configures the cluster builder
```

### 2. Install CRDs

If you're updating an existing install after adding new CRD types (e.g., `DataSource`), regenerate and apply:

```bash
make generate      # regenerates config/crd/bases/ from type annotations
make install-crds  # kubectl apply -f config/crd/bases/
```

### 3. Deploy the platform

```bash
make deploy-local
```

This builds the platform Docker image, applies CRDs, applies RBAC, deploys the platform manifest, and restarts the controller and API server pods.

### 4. Verify

```bash
kubectl get pods -n iaf-system
# NAME                             READY   STATUS    RESTARTS   AGE
# iaf-apiserver-xxxx               1/1     Running   0          1m
# iaf-controller-xxxx              1/1     Running   0          1m

curl http://iaf.localhost/health
# {"status":"ok"}
```

---

## Configuration

All configuration is via environment variables with the `IAF_` prefix, set in `config/deploy/platform.yaml`.

| Variable | Default | Description |
|----------|---------|-------------|
| `IAF_API_PORT` | `8080` | API server listen port |
| `IAF_API_TOKENS` | `iaf-dev-key` | Comma-separated Bearer tokens. **Change in production.** |
| `IAF_BASE_DOMAIN` | `localhost` | Base domain. Apps are exposed at `<name>.<base_domain>` |
| `IAF_CLUSTER_BUILDER` | `iaf-cluster-builder` | kpack ClusterBuilder name |
| `IAF_REGISTRY_PREFIX` | `registry.localhost:5000/iaf` | Container registry prefix for built images |
| `IAF_SOURCE_STORE_DIR` | `/tmp/iaf-sources` | Local directory for source code tarballs |
| `IAF_SOURCE_STORE_URL` | `http://iaf-source-store.iaf-system.svc.cluster.local` | URL kpack uses to fetch source tarballs |
| `IAF_TLS_ISSUER` | `selfsigned-issuer` | cert-manager ClusterIssuer name. Set to `""` to disable TLS |
| `IAF_GITHUB_TOKEN` | (empty) | GitHub PAT. GitHub tools are disabled when empty |
| `IAF_GITHUB_ORG` | (empty) | GitHub organisation for the GitHub integration |

### Authentication tokens

`IAF_API_TOKENS` accepts a comma-separated list. Every API and MCP request must present one of these tokens as a Bearer token:

```
Authorization: Bearer <token>
```

Generate a secure token:
```bash
openssl rand -hex 32
```

Add multiple tokens to allow token rotation without downtime:
```
IAF_API_TOKENS=prod-token-abc123,new-token-xyz789
```

---

## TLS / HTTPS

TLS is **on by default** for every application. When an agent deploys an app, the controller automatically creates a cert-manager `Certificate` CR and routes traffic over HTTPS.

### Setup

1. **Install cert-manager** (if not already installed):
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
   ```

2. **Create a ClusterIssuer.** For development, a self-signed issuer works:
   ```yaml
   # config/certmanager/selfsigned-issuer.yaml
   apiVersion: cert-manager.io/v1
   kind: ClusterIssuer
   metadata:
     name: selfsigned-issuer
   spec:
     selfSigned: {}
   ```
   ```bash
   kubectl apply -f config/certmanager/selfsigned-issuer.yaml
   ```

   For production, use Let's Encrypt or your organisation's CA.

3. **Configure IAF** to use the issuer:
   ```
   IAF_TLS_ISSUER=selfsigned-issuer   # or letsencrypt-prod, etc.
   ```

### Disabling TLS globally

Set `IAF_TLS_ISSUER=""` in the platform config. The controller will not create Certificate CRs and will use plain HTTP.

### Disabling TLS per application

An agent can opt a specific application out of TLS by setting `spec.tls.enabled: false` in the `Application` CR. This is surfaced via the `deploy_app` MCP tool.

---

## Data Catalog

The data catalog lets operators register organisational data sources (databases, APIs, etc.) that agents can discover and attach to their applications. Agents can list and attach data sources but **cannot create or modify them**.

### How it works

1. Operator creates a credential Secret in `iaf-system`
2. Operator creates a cluster-scoped `DataSource` CR referencing that Secret
3. Agents call `list_data_sources` / `get_data_source` to discover sources
4. Agents call `attach_data_source` — the platform copies credentials into the session namespace and injects them as env vars into the application container

### Registering a data source

**Step 1: Create the credential Secret in `iaf-system`**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: prod-postgres-creds
  namespace: iaf-system
type: Opaque
stringData:
  host: db.prod.internal
  port: "5432"
  database: mydb
  username: readonly_user
  password: s3cr3t-password
```

**Step 2: Create the DataSource CR**

```yaml
apiVersion: iaf.io/v1alpha1
kind: DataSource
metadata:
  name: prod-postgres    # agents discover it by this name
spec:
  kind: postgres
  description: "Production PostgreSQL read-only replica"
  schema: "Tables: users, orders, products, inventory"
  tags:
    - production
    - sql
    - postgres
  secretRef:
    name: prod-postgres-creds   # must exist in iaf-system
    namespace: iaf-system
  envVarMapping:
    # secret key → env var name injected into the app container
    host: POSTGRES_HOST
    port: POSTGRES_PORT
    database: POSTGRES_DB
    username: POSTGRES_USER
    password: POSTGRES_PASSWORD
```

```bash
kubectl apply -f prod-postgres-datasource.yaml
```

Agents can now discover it:
```
> What data sources are available?
```

And attach it to an app:
```
> Attach prod-postgres to my app "api-server"
```

### DataSource spec reference

| Field | Required | Description |
|-------|----------|-------------|
| `kind` | yes | Type of data source: `postgres`, `mysql`, `s3`, `http-api`, `kafka`, or any string |
| `description` | no | Human-readable description shown to agents |
| `schema` | no | Data schema description (tables, endpoints, etc.) |
| `tags` | no | String array for filtering (`list_data_sources` supports tag filters) |
| `secretRef.name` | yes | Name of the credential Secret |
| `secretRef.namespace` | yes | Namespace containing the Secret (typically `iaf-system`) |
| `envVarMapping` | yes | Map of Secret data key → env var name injected into the app |

### Secret type restrictions

Accepted Secret types: `Opaque`, `kubernetes.io/basic-auth`

Rejected types (attach will fail): `kubernetes.io/service-account-token`, `kubernetes.io/dockerconfigjson`, `kubernetes.io/dockercfg`

### Revoking access

Copying a credential into an agent's namespace (at `attach_data_source` time) makes the agent's app self-sufficient — deleting the source Secret in `iaf-system` does **not** revoke access. The copied Secret in the session namespace persists.

To revoke access:
1. Delete the copied Secret in the agent's namespace: `kubectl delete secret iaf-ds-<datasource-name> -n iaf-<session-id>`
2. Or delete the Application CR entirely (the copied Secret is owned by the Application and will be garbage-collected)

### RBAC for the data catalog

The controller needs read access to Secrets in `iaf-system` to copy them into session namespaces. This is granted via a namespace-scoped `Role` (not a cluster-wide ClusterRole) in `iaf-system`:

```bash
kubectl apply -f config/rbac/iaf-system-role.yaml
kubectl apply -f config/rbac/iaf-system-rolebinding.yaml
```

Both files are applied automatically by `make deploy-local`.

---

## Git Credentials (for private repositories)

Agents store their own git credentials per-session — operators do not need to pre-provision these. The platform enforces:

- Only `basic-auth` (HTTPS) and `ssh` credential types
- Git server URL must use HTTPS or the `git@<host>` SSH format
- Internal/RFC 1918 IP addresses are rejected (SSRF prevention)
- Maximum 20 credentials per session
- Credential values are never returned in tool output

Agents manage their own credentials with `add_git_credential`, `list_git_credentials`, and `delete_git_credential`.

---

## Monitoring and Troubleshooting

### Check platform health

```bash
# All platform pods running?
kubectl get pods -n iaf-system

# Platform healthy?
curl http://iaf.localhost/health

# Any errors in the controller?
kubectl logs -n iaf-system deployment/iaf-controller --tail=50

# Any errors in the API server?
kubectl logs -n iaf-system deployment/iaf-apiserver --tail=50
```

### Check an agent's application

```bash
# List Application CRs in a session namespace
kubectl get applications -n iaf-<session-id>

# Get full Application status
kubectl describe application <app-name> -n iaf-<session-id>

# Check Deployment
kubectl get deployment <app-name> -n iaf-<session-id>

# Check if kpack build is running
kubectl get images -n iaf-<session-id>
kubectl get builds -n iaf-<session-id>
```

### Common issues

| Symptom | Check |
|---------|-------|
| App stuck in `Building` | `kubectl get images -n iaf-<ns>` — look for kpack Image CR; `kubectl describe build -n iaf-<ns>` for build errors |
| App stuck in `Deploying` | `kubectl get pods -n iaf-<ns>` — check pod events; image pull errors are common with private registries |
| TLS certificate not issued | `kubectl get certificate -n iaf-<ns>` — cert-manager may be misconfigured; check ClusterIssuer exists |
| `attach_data_source` fails with "credential secret not found" | Verify the Secret exists in `iaf-system` with the name/namespace in the DataSource CR; check the iaf-system Role/RoleBinding is applied |
| Build fails for private git repo | Agent needs to call `add_git_credential` first and pass the credential name to `deploy_app` |

---

## Upgrading

1. Pull the latest code
2. Run `make generate` if CRD types changed
3. Run `make deploy-local` — applies updated CRDs, RBAC, and platform image

Existing `Application` CRs are backward-compatible with additive field changes. The controller will re-reconcile all Applications on restart.
