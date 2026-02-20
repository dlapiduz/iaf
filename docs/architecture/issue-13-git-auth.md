# Architecture: Git Source Authentication — Private Repository Support (Issue #13)

## Technical Design

### Approach

Add three new MCP tools (`add_git_credential`, `list_git_credentials`, `delete_git_credential`) that manage Kubernetes Secrets in the session namespace. Each Secret is annotated with `kpack.io/git: <host-prefix>` so kpack automatically selects the right credential when building from a matching git URL — no per-deployment credential selection is needed. After creating a Secret, the tool patches the session namespace's `iaf-kpack-sa` ServiceAccount to add the Secret to its `secrets` list (required by kpack for credential lookup). `deploy_app` gains an optional `git_credential` parameter that validates the named credential exists before creating the Application CR. Credential material (passwords, tokens, private keys) is never returned by any tool or API, and never logged at any level.

**⚠ This design creates and manages Kubernetes Secrets with raw private keys and tokens — escalating `needs-security-review`.**

### Changes Required

**New `internal/mcp/tools/git_credentials.go`** — three registration functions:

1. `RegisterAddGitCredential(server, deps)`:
   - Input: `session_id`, `name` (DNS label), `type` (`"basic-auth"` | `"ssh"`), `git_server_url` (e.g., `"https://github.com"` or `"git@github.com"`), and conditional fields: `username`+`password` (basic-auth) or `private_key` (ssh)
   - Validate `name` using `validation.ValidateAppName` (DNS label rules)
   - Validate `type` against allowlist
   - Validate `git_server_url`: basic-auth must start with `https://`; ssh must match `git@<host>` pattern; reject all other schemes (prevent SSRF-style abuse)
   - Check per-session credential count ≤ 20 (list existing `iaf-git-cred-` secrets, reject if at limit)
   - Call `k8s.BuildGitCredentialSecret(namespace, name, credType, gitServerURL, username, password, privateKey)`
   - Create the Secret via `deps.Client.Create`
   - Patch the `iaf-kpack-sa` ServiceAccount to add the Secret to `secrets` list: call `k8s.AddSecretToKpackSA(ctx, deps.Client, namespace, name)`
   - Return: `{ "name": "...", "type": "...", "git_server_url": "..." }` — no credential material
   - On any error after Secret creation but before SA patch: attempt to clean up the Secret; return error

2. `RegisterListGitCredentials(server, deps)`:
   - Input: `session_id`
   - Lists Secrets in the session namespace with label `iaf.io/credential-type: git`
   - Returns array of `{ "name", "type", "git_server_url", "created_at" }` — Secret `data` fields are never included in output

3. `RegisterDeleteGitCredential(server, deps)`:
   - Input: `session_id`, `name`
   - Gets and deletes the named Secret (verifying it has the `iaf.io/credential-type: git` label to prevent deleting arbitrary secrets)
   - Calls `k8s.RemoveSecretFromKpackSA(ctx, deps.Client, namespace, name)` to patch the SA
   - Returns: `{ "deleted": true, "name": "..." }`

**New `internal/k8s/git_credentials.go`**:

- `BuildGitCredentialSecret(namespace, name, credType, gitServerURL, username, password, privateKey string) *corev1.Secret`:
  - For `basic-auth`:
    ```go
    &corev1.Secret{
        ObjectMeta: metav1.ObjectMeta{
            Name: name, Namespace: namespace,
            Labels: map[string]string{"iaf.io/credential-type": "git"},
            Annotations: map[string]string{
                "kpack.io/git": gitServerURL,
                "iaf.io/git-server": gitServerURL,
            },
        },
        Type: corev1.SecretTypeBasicAuth,
        StringData: map[string]string{
            "username": username,
            "password": password,
        },
    }
    ```
  - For `ssh`:
    ```go
    Type: corev1.SecretTypeSSHAuth,
    StringData: map[string]string{"ssh-privatekey": privateKey},
    ```
  - Note: `StringData` is write-only; the API server converts it to base64-encoded `Data` and never returns `StringData` in subsequent reads.

- `AddSecretToKpackSA(ctx, client, namespace, secretName string) error`:
  - Gets `iaf-kpack-sa` ServiceAccount
  - Appends `corev1.ObjectReference{Name: secretName}` to `sa.Secrets` if not already present
  - Updates the SA
  - Idempotent: if secret is already in the list, no-op

- `RemoveSecretFromKpackSA(ctx, client, namespace, secretName string) error`:
  - Gets `iaf-kpack-sa` ServiceAccount
  - Filters `sa.Secrets` to remove the entry with `Name == secretName`
  - Updates the SA

**Updated `internal/mcp/tools/deploy.go`**:
- Add `GitCredential string` (optional) to `DeployAppInput`
- If `GitCredential != ""`: verify a Secret named `GitCredential` with label `iaf.io/credential-type: git` exists in the session namespace; if not, return error `"git credential %q not found; call add_git_credential first"`
- No change to the Application CR — kpack picks up credentials from the SA automatically
- Update result message to note git credential was validated when provided

**Updated `internal/mcp/tools/deps.go`** — no structural changes needed; `deps.Client` already supports Secret operations.

**RBAC markers** (add to `internal/controller/application_controller.go` — these apply to the platform SA used by all binaries):
```go
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;get;list;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;get;update;patch
```
The existing `create;get` on serviceaccounts is insufficient — add `update;patch`.

**New `internal/mcp/tools/git_credentials_test.go`**:
- Table-driven tests using fake K8s client:
  - `TestAddGitCredential_BasicAuth`: secret created with correct type, annotation, labels; SA patched with secret name; tool output contains no password field
  - `TestAddGitCredential_SSH`: secret created with `kubernetes.io/ssh-auth` type
  - `TestAddGitCredential_InvalidType`: returns validation error
  - `TestAddGitCredential_InvalidURL_HTTPNoTLS`: rejects `http://` scheme for basic-auth
  - `TestAddGitCredential_CountLimit`: creating 21st credential returns limit error
  - `TestListGitCredentials_NoCredentialMaterial`: verify `Data` and `StringData` not present in output
  - `TestDeleteGitCredential`: secret deleted, SA updated
  - `TestDeleteGitCredential_WrongLabel`: cannot delete a secret without the `iaf.io/credential-type: git` label

**`internal/mcp/server.go`**: call `RegisterAddGitCredential`, `RegisterListGitCredentials`, `RegisterDeleteGitCredential`; update `serverInstructions` to mention the credential tools.

**`internal/mcp/prompts/deploy_guide.go`**: add section "Private Repository Access" explaining the credential workflow.

### Data / API Changes

**No CRD changes.**

**New K8s resources** (created by MCP tools, not controller):
- `Secret` in session namespace, type `kubernetes.io/basic-auth` or `kubernetes.io/ssh-auth`
- Labels: `iaf.io/credential-type: git`
- Annotations: `kpack.io/git: <url>`, `iaf.io/git-server: <url>`

**Tool inputs/outputs**:

`add_git_credential` input:
```json
{
  "session_id": "...",
  "name": "github-creds",
  "type": "basic-auth",
  "git_server_url": "https://github.com",
  "username": "myuser",
  "password": "ghp_xxxx"
}
```

`add_git_credential` output:
```json
{
  "name": "github-creds",
  "type": "basic-auth",
  "git_server_url": "https://github.com",
  "created": true
}
```

`list_git_credentials` output:
```json
{
  "credentials": [
    { "name": "github-creds", "type": "basic-auth", "git_server_url": "https://github.com", "created_at": "2024-01-01T00:00:00Z" }
  ]
}
```

`deploy_app` input addition:
```json
{ ..., "git_credential": "github-creds" }
```

### Multi-tenancy & Shared Resource Impact

- **Credential isolation**: Secrets are created in the session namespace. An agent in session A cannot reference credentials from session B because `ResolveNamespace(session_id)` returns only the calling agent's namespace.
- **SA patching is scoped**: `iaf-kpack-sa` is created per-namespace (in `auth.EnsureNamespace`). Patching it in one namespace does not affect other namespaces.
- **kpack credential matching**: kpack matches credentials to git URLs by the `kpack.io/git` annotation prefix. If two sessions both have credentials for `https://github.com`, kpack will use only the credentials in the SA of the Image CR's namespace — isolation is preserved.
- **No cross-namespace credential access**: the only cross-namespace scenario is `attach_data_source` (issue #2). Git credentials are entirely namespace-scoped.

### Security Considerations

- **No credential material in output**: `list_git_credentials` and all other tools must never include Secret `data` or `stringData`. The implementation must not call `client.Get` on the Secret and then include its `Data` field in the response.
- **No credential material in logs**: the handler must not log input fields (`password`, `private_key`) at any log level. Use structured logging and ensure these fields are not part of the input struct's `String()` or `Error()` method.
- **URL scheme validation**:
  - `basic-auth` `git_server_url`: must begin with `https://`. Reject `http://` (transmits credentials in plaintext), `ftp://`, `file://`, or other schemes. Reject IPs that could be internal cluster addresses (prevent SSRF via the kpack build system — if kpack can be directed to clone from `http://10.0.0.1/malicious`, it could exfiltrate cluster-internal data).
  - `ssh` `git_server_url`: must match `^git@[a-zA-Z0-9][a-zA-Z0-9.-]+:[a-zA-Z0-9_.-/]+$`. Reject patterns that could inject shell metacharacters into the SSH URI.
- **SSH key validation**: accept PEM-encoded keys only. Check that the input begins with `-----BEGIN ` and ends with `-----END `. Reject payloads > 16 KB (a legitimate SSH private key is never this large). Do NOT parse the key beyond format check — avoid PEM parsing vulnerabilities. Key type check (RSA vs Ed25519) is not needed; kpack handles all types.
- **Basic-auth token length**: reject tokens > 4096 bytes (reasonable maximum for a GitHub PAT or GitLab token).
- **Credential count limit**: 20 credentials per session. Prevents unbounded Secret creation that could exhaust etcd storage across many sessions.
- **Label check on delete**: `delete_git_credential` must verify `iaf.io/credential-type: git` label before deleting. This prevents a tool bug or injection attack from deleting other secrets (e.g., the kpack SA token or TLS certs) by name.
- **Operator vs agent supply of credentials**: the issue raises whether agents should supply raw credential material or reference operator-pre-created secrets. **Decision for this design**: agents supply raw material (simplest for autonomous agents with no human operator available). Operator-managed secrets can be added as a future enhancement. Document the security trade-off: raw credential material transits the MCP channel and the MCP server process has access to it at handling time.
- **Rate limiting**: the 20-credential limit is a soft rate limit. Operators should additionally consider network-level or admission-webhook throttling for high-risk deployments.

**⚠ Needs security review**:
- SSRF risk via `kpack.io/git` annotation with internal IP/cluster addresses
- Whether agents should supply raw credential material vs operator-managed secret references
- SSH key validation depth (format check vs full parse)
- Credential data residency: Secret is stored in etcd; is etcd encryption at rest enabled on the cluster?
- Token/password exposure window during MCP tool call (in-memory in the MCP server process)

### Resource & Performance Impact

- Per credential: 1 K8s Secret (~1 KB), 1 SA patch operation.
- `add_git_credential`: 2 K8s API calls (create Secret + patch SA). Negligible latency.
- `list_git_credentials`: 1 K8s API call (list Secrets with label selector).
- `delete_git_credential`: 3 K8s API calls (get Secret + delete Secret + patch SA).

### Migration / Compatibility

- Purely additive. Existing `deploy_app` behaviour unchanged (no `git_credential` parameter required).
- kpack SA patching requires `update;patch` RBAC on ServiceAccounts — this is an additive RBAC change; `make generate` regenerates the ClusterRole.
- New RBAC for Secrets (`create;get;list;delete`) — additive; no existing permissions changed.

### Open Questions for Developer

1. **SSRF via kpack**: if the `kpack.io/git` annotation points to an internal cluster address (e.g., `https://10.96.0.1`), kpack's build process might make requests to internal services. The URL validator should block RFC 1918 addresses and link-local ranges. Consider using a URL validation library that normalises the host before checking.
2. **SA `secrets` list vs `imagePullSecrets`**: kpack uses `sa.Secrets` (not `imagePullSecrets`) for git credentials. Confirm this is still the case with the installed kpack version before implementing.
3. **Credential update**: the issue does not include an `update_git_credential` tool. If a token rotates, the agent must delete and re-create. This is intentional — it keeps the tool surface minimal. Document this in the prompt.
4. **Namespace deletion cascade**: when `unregister` deletes the session namespace, all Secrets in it are deleted by Kubernetes. The kpack SA in the namespace is also deleted. No extra cleanup needed for credentials — confirm this assumption by checking the `unregister` implementation in `internal/auth/namespace.go`.
5. **MCP tool registration order**: register `add_git_credential` before `deploy_app` so agents discover credentials before deployment. Ordering in `server.go` does not affect functionality but improves agent-visible tool listing order.
