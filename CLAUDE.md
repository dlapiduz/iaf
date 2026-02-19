# IAF - Intelligent Application Fabric

## Build & Test Commands
- `make build` — Build all three binaries (apiserver, mcpserver, controller)
- `make test` or `go test ./... -v` — Run all tests
- `go test ./internal/mcp/... -v` — Run MCP-related tests only
- `go test ./internal/mcp/prompts/ -v` — Run a specific package's tests
- `make generate` — Regenerate deepcopy and CRD YAML
- `make fmt` — Format code
- `make vet` — Run go vet

## Testing Requirements
- **All new code must have tests.** When creating new files or features, create corresponding `_test.go` files in the same package.
- Use table-driven tests and subtests (`t.Run`) where appropriate.
- For MCP prompts and resources, test via the MCP client-server in-memory transport:
  ```go
  server := gomcp.NewServer(...)
  st, ct := gomcp.NewInMemoryTransports()
  server.Connect(ctx, st, nil)
  cs, _ := client.Connect(ctx, ct, nil)
  // Use cs.GetPrompt(), cs.ReadResource(), cs.ListTools(), etc.
  ```
- For code requiring a Kubernetes client, use the controller-runtime fake client:
  ```go
  scheme := runtime.NewScheme()
  iafv1alpha1.AddToScheme(scheme)
  k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
  ```
- Run `make test` before considering any implementation complete.

## Local Deployment
After making code changes, deploy to the local Kubernetes cluster (Rancher Desktop):
- `make deploy-local` — Build the Docker image, apply CRDs/RBAC, deploy to K8s, and restart pods
- `make setup-local` — One-time setup: creates namespaces, registry, CRDs, RBAC, and kpack SA
- `make setup-kpack` — One-time setup: installs kpack and configures cluster builders
- `make install-crds` — Apply only CRD changes (faster than full deploy)

The `deploy-local` target builds the image via `nerdctl` into the `k8s.io` namespace (so K8s can pull with `imagePullPolicy: Never`), applies all manifests, and restarts the controller and apiserver deployments.

The platform is accessible at:
- API/MCP: `http://iaf.localhost` (via Traefik IngressRoute)
- Auth: `Authorization: Bearer iaf-dev-key` (default dev token)

## Project Structure
- `api/v1alpha1/` — CRD types (Application)
- `cmd/` — Binary entry points (apiserver, mcpserver, controller)
- `internal/mcp/tools/` — MCP tool implementations (deploy, push_code, status, logs, list, delete)
- `internal/mcp/prompts/` — MCP prompt implementations (deploy-guide, language-guide)
- `internal/mcp/resources/` — MCP resource implementations (platform-info, language-spec, application-spec)
- `internal/mcp/server.go` — MCP server wiring (registers all tools, prompts, resources)
- `internal/auth/` — Session management and namespace provisioning
- `internal/controller/` — Kubernetes controller
- `internal/api/` — REST API handlers
- `internal/sourcestore/` — Source code tarball storage

## Code Conventions
- MCP tools, prompts, and resources each get their own file with a `RegisterXxx(server, deps)` function.
- All registration functions take `(*gomcp.Server, *tools.Dependencies)`.
- Use the `tools.Dependencies` struct from `internal/mcp/tools/deps.go` for shared dependencies.
- CRD types require `+groupName=iaf.io` in the package doc.
- Use controller-gen v0.17.2 for code generation.
