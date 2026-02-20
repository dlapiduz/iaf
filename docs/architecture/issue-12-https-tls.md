# Architecture: HTTPS/TLS — Certificate Management for Deployed Apps (Issue #12)

## Technical Design

### Approach

Integrate cert-manager into the platform so every deployed application is served over HTTPS by default. The controller creates a cert-manager `Certificate` CR in the app's namespace alongside the IngressRoute; cert-manager issues the certificate and stores it as a K8s Secret; the Traefik IngressRoute is updated to use `entryPoints: ["websecure"]` and reference that Secret. HTTP→HTTPS redirect is handled at the Traefik entrypoint level (cluster-wide `redirectTo: websecure` on the `web` entrypoint) rather than per-app IngressRoute, to keep per-app manifests simple. A new optional `tls` field on `ApplicationSpec` allows agents to opt out for internal/non-routable apps. The `IAF_TLS_ISSUER` env var names the `ClusterIssuer` to use (`selfsigned-issuer` for local dev, `letsencrypt-prod` for production).

**⚠ This design touches shared ingress infrastructure and TLS certificate issuance — escalating `needs-security-review`.**

### Changes Required

**CRD changes (`api/v1alpha1/application_types.go`)**:
- Add `TLSConfig` struct:
  ```go
  type TLSConfig struct {
      // Enabled controls whether TLS is enabled for this application.
      // Defaults to true when the TLS field is set.
      // +optional
      Enabled *bool `json:"enabled,omitempty"`
  }
  ```
- Add `TLS *TLSConfig` field to `ApplicationSpec` (optional; nil = TLS on by default)
- Add helper (in types file or controller): `func IsTLSEnabled(app *Application) bool` — returns `true` if `TLS == nil` or `TLS.Enabled == nil` or `*TLS.Enabled == true`

**New `internal/k8s/certificate.go`**:
- `CertificateGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}`
- `BuildCertificate(app *iafv1alpha1.Application, host, issuerName string) *unstructured.Unstructured` — creates a cert-manager Certificate CR:
  - Name: `app.Name`
  - Namespace: `app.Namespace`
  - `spec.secretName`: `{app.Name}-tls`
  - `spec.dnsNames`: `[host]`
  - `spec.issuerRef.name`: `issuerName`
  - `spec.issuerRef.kind`: `ClusterIssuer`
  - OwnerReference pointing to the Application

**Updated `internal/k8s/traefik.go`**:
- Change `BuildIngressRoute` signature to `BuildIngressRoute(app *iafv1alpha1.Application, baseDomain string, tlsEnabled bool) *unstructured.Unstructured`
- When `tlsEnabled == true`:
  - Set `entryPoints: ["websecure"]`
  - Add `tls: { secretName: "{app.Name}-tls" }` to spec
- When `tlsEnabled == false`:
  - Keep `entryPoints: ["web"]`
  - No `tls` section

**Updated `internal/controller/application_controller.go`**:
- Add `TLSIssuer string` field to `ApplicationReconciler`
- Add RBAC marker:
  ```go
  // +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
  ```
- Add `reconcileCertificate(ctx, *app) error` method:
  - If `!IsTLSEnabled(app)`, return nil
  - Build desired Certificate CR
  - Create if not exists; update `spec.dnsNames` if host changed
- Update `Reconcile` to call `reconcileCertificate` before `reconcileIngressRoute`
- Update `reconcileIngressRoute` call to pass `iafv1alpha1.IsTLSEnabled(&app)` as third arg
- Update `setRunning`: set `scheme = "https"` when TLS enabled, `scheme = "http"` otherwise
  ```go
  scheme := "http"
  if iafv1alpha1.IsTLSEnabled(&app) {
      scheme = "https"
  }
  app.Status.URL = fmt.Sprintf("%s://%s", scheme, host)
  ```
- Update `SetupWithManager` to watch cert-manager Certificate CRs owned by Applications (same `Owns` pattern — but cert-manager Certificates are unstructured, so use `Watches` with `handler.EnqueueRequestForOwner` as designed in issue #10/#11)

**`cmd/controller/main.go`**:
- Read `IAF_TLS_ISSUER` from viper config (default: `"selfsigned-issuer"`)
- Pass to `ApplicationReconciler{TLSIssuer: cfg.TLSIssuer}`

**`config/deploy/platform.yaml`** (or Traefik deployment manifest):
- Add `--entrypoints.web.http.redirections.entrypoint.to=websecure` and `--entrypoints.web.http.redirections.entrypoint.scheme=https` to Traefik's command args. This redirects all HTTP → HTTPS at the Traefik level, no per-app IngressRoute needed.

**`make setup-local` / `Makefile`**:
- Add cert-manager installation step:
  ```
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml
  kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=120s
  ```
- Apply self-signed ClusterIssuer (add to `config/deploy/` as `clusterissuer.yaml`):
  ```yaml
  apiVersion: cert-manager.io/v1
  kind: ClusterIssuer
  metadata:
    name: selfsigned-issuer
  spec:
    selfSigned: {}
  ```

**`internal/mcp/resources/platform_info.go`**:
- Update `routing.protocol` from `"http"` to `"https"` (with note that HTTP redirects to HTTPS)
- Add `"tls": { "enabled": true, "issuer": "selfsigned-issuer (local) / letsencrypt-prod (production)" }`

**`internal/mcp/prompts/deploy_guide.go`**:
- Update all URL examples from `http://` to `https://`
- Add note: "HTTP requests are automatically redirected to HTTPS. Browsers may show a self-signed certificate warning in local development environments; this is expected."

**`internal/mcp/tools/deploy.go`** and **`internal/mcp/server.go`**:
- Update URL construction in `deploy_app` result message from `http://` to `https://`

**Tests**:
- `internal/k8s/` — `TestBuildCertificate`: verify GVK, secretName, dnsNames, issuerRef, ownerReference; `TestBuildIngressRoute_TLS`: verify websecure entrypoint and tls secretName; `TestBuildIngressRoute_NoTLS`: verify web entrypoint and no tls field
- `internal/controller/` — `TestReconcileCertificate`: fake client test; TLS enabled → Certificate CR created with correct spec; TLS disabled → no Certificate CR created

### Data / API Changes

**CRD change** (`api/v1alpha1`):
- New optional field `tls` on `ApplicationSpec`:
  ```yaml
  spec:
    tls:
      enabled: false  # opt out of TLS for internal apps
  ```
  Default (omitted): TLS on.
- `ApplicationStatus.URL` changes from `http://...` to `https://...` when TLS is enabled.

**New `iaf.io/v1alpha1` CRD generated field**: `tls.enabled` (boolean, optional).

**New platform component**: cert-manager ClusterIssuer `selfsigned-issuer` (local) / `letsencrypt-prod` (production).

**New `IAF_TLS_ISSUER` env var** for the controller (default: `selfsigned-issuer`).

**`make generate`** must be re-run after CRD type changes to regenerate deepcopy and CRD YAML.

### Multi-tenancy & Shared Resource Impact

- **Certificate isolation**: each app gets its own cert-manager `Certificate` CR and TLS Secret, in its own namespace. No shared certificates across tenants. Revocation or expiry of one cert does not affect others.
- **Wildcard cert trade-off**: wildcard cert (`*.{namespace}.{baseDomain}`) would reduce cert-manager load and avoid Let's Encrypt rate limits at scale, but requires DNS-01 challenge (not compatible with HTTP-01 on local dev) and means all apps in a namespace share a cert — if one is revoked, all are affected. **Decision: per-app certs for initial implementation.** Add a note in the design about Let's Encrypt rate limits (50 certs/registered domain/week) and recommend wildcard certs for production deployments with more than ~10 apps/domain/week.
- **Traefik redirect**: global HTTP→HTTPS redirect at the entrypoint level affects ALL traffic through Traefik, not just IAF apps. This is the correct behaviour in a production cluster but must be documented. The platform YAML should configure this redirect only if the operator intends it to be cluster-wide.
- **Shared ClusterIssuer**: `selfsigned-issuer` and `letsencrypt-prod` are cluster-scoped and shared by all sessions. No tenant can modify them through the platform (agents have no RBAC on ClusterIssuers).

### Security Considerations

- **Self-signed vs ACME**: local dev uses `selfsigned-issuer`. This produces certificates that browsers will not trust by default. Document that developers should import the CA into their browser/OS trust store or use `mkcert` to create a locally-trusted CA. `make setup-local` should print instructions.
- **Let's Encrypt rate limits**: HTTP-01 challenge requires the domain to be publicly reachable. If the cluster is not publicly accessible, only DNS-01 works. Document that `letsencrypt-prod` requires a public cluster and a DNS-01 capable solver configuration. This is operator responsibility; the platform provides the mechanism, not the ACME configuration.
- **Certificate secrets in app namespace**: cert-manager stores the TLS cert in the same namespace as the app. The cert Secret is owned by the Certificate CR. Agents cannot read these Secrets via MCP (no Secret read tool exposed). The controller reads them only indirectly (Traefik reads them). No extra protection needed.
- **TLS opt-out**: `tls.enabled: false` allows HTTP-only apps. This should only be used for internal services on private networks. The `deploy-guide` should warn agents that opting out exposes user data in plaintext.
- **ACME account key**: cert-manager stores Let's Encrypt account keys in the cert-manager namespace as Secrets. These are outside IAF's control; operators are responsible for securing the cert-manager namespace.
- **RBAC**: cert-manager `Certificate` RBAC markers are added to the controller. The controller needs `get;list;watch;create;update;patch;delete` on `cert-manager.io/certificates`. This is namespace-scoped (controller watches only the namespaces it manages).

**Escalate for security review**:
- ACME challenge type selection and Let's Encrypt rate limit risk
- Whether global HTTP→HTTPS redirect in Traefik is appropriate for all cluster deployments
- Cert Secret access controls in multi-tenant namespaces

### Resource & Performance Impact

- cert-manager adds ~3 pods and ~50 MB of cluster memory.
- Per app: 1 `Certificate` CR + 1 TLS Secret (a few KB). Certificate issuance: ~5–30 seconds (self-signed is instant; HTTP-01/DNS-01 takes longer).
- cert-manager renews certificates ~30 days before expiry; no operator action needed.
- Controller reconcile: adding one extra K8s API call (`reconcileCertificate` does a `Get` + possibly a `Create`). Negligible performance impact.

### Migration / Compatibility

- **Existing apps** (before this feature is deployed): their IngressRoutes use `entryPoints: ["web"]`. After deployment, the Traefik HTTP→HTTPS redirect will redirect HTTP requests for these apps too — but since they have no HTTPS entrypoint or TLS cert, HTTPS connections will fail until the controller reconciles them. **The controller reconcile loop will update all existing Application CRs on startup**, creating Certificates and updating IngressRoutes. This is the desired behaviour.
- **Apps with `tls.enabled: false`**: will have their IngressRoutes updated to keep `entryPoints: ["web"]` — no change in routing behaviour.
- **`make generate`** must be run after the CRD type changes.
- **Rollout order**: deploy cert-manager + ClusterIssuer BEFORE deploying the updated controller. If the controller is deployed first with the cert-manager RBAC markers but cert-manager is not installed, the reconciler will fail with `no matches for kind "Certificate"` — add a startup check or graceful degradation.

### Open Questions for Developer

1. **HTTP-01 vs DNS-01**: for local Rancher Desktop, HTTP-01 is not applicable (non-public). Self-signed issuer works fine. For production, document both options. Should the platform default to HTTP-01 when `letsencrypt-prod` is configured? Recommend: leave ACME solver configuration to the operator via the ClusterIssuer definition; IAF just specifies the issuer name.
2. **Wildcard cert for production**: if an operator wants wildcard certs, they should set `IAF_TLS_WILDCARD=true` and configure a DNS-01 `ClusterIssuer`. This is a future issue — the current design supports per-app certs only. Document the upgrade path.
3. **`mkcert` for local dev**: `make setup-local` could run `mkcert -install` to add the local CA to the trust store. This is convenient but requires `mkcert` to be installed. Recommend: print instructions, don't auto-install.
4. **`SetupWithManager` watch for cert-manager Certificates**: cert-manager CRDs may not be installed when the controller starts. Use a dynamic watch that falls back gracefully if cert-manager CRDs are absent — or require cert-manager as a prerequisite and fail fast on startup if the CRDs are missing.
5. **Traefik entrypoint redirect**: verify the exact Traefik v3 syntax for HTTP→HTTPS redirections. The flags differ between Traefik v2 and v3. Check the Traefik version in the current `config/deploy/platform.yaml`.
