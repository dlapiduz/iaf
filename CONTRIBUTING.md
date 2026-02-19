# Contributing to IAF

Thank you for your interest in contributing to IAF (Intelligent Application Fabric)!

## Getting Started

### Prerequisites

- Go 1.22+
- Docker or nerdctl (for building images)
- A Kubernetes cluster (Rancher Desktop, k3s, or similar for local development)
- [kpack](https://github.com/buildpacks-community/kpack) installed in the cluster
- [Traefik](https://traefik.io/) as the ingress controller

### Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/dlapiduz/iaf.git
   cd iaf
   ```

2. Install dependencies:
   ```bash
   go mod download
   cd web && npm install && cd ..
   ```

3. Build:
   ```bash
   make build
   ```

4. Run tests:
   ```bash
   make test
   ```

## Development Workflow

### Building

```bash
make build        # Build all binaries (apiserver, mcpserver, controller)
make generate     # Regenerate deepcopy, CRD YAML, and RBAC
make fmt          # Format code
make vet          # Run go vet
```

### Testing

All new code must have tests. Run the full test suite before submitting:

```bash
make test
```

For MCP tools, prompts, and resources, use the in-memory transport for testing:

```go
server := gomcp.NewServer(...)
st, ct := gomcp.NewInMemoryTransports()
server.Connect(ctx, st, nil)
cs, _ := client.Connect(ctx, ct, nil)
```

For code that interacts with Kubernetes, use the controller-runtime fake client:

```go
scheme := runtime.NewScheme()
iafv1alpha1.AddToScheme(scheme)
k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
```

### Local Deployment

Deploy to a local Kubernetes cluster (e.g., Rancher Desktop):

```bash
make deploy-local
```

This builds the container image, applies CRDs and RBAC, and deploys the platform.

## Project Structure

```
api/v1alpha1/          - CRD types (Application)
cmd/                   - Binary entry points (apiserver, mcpserver, controller)
internal/mcp/tools/    - MCP tool implementations
internal/mcp/prompts/  - MCP prompt implementations
internal/mcp/resources/- MCP resource implementations
internal/mcp/server.go - MCP server wiring
internal/controller/   - Kubernetes controller
internal/api/          - REST API handlers
internal/auth/         - Session management and namespace isolation
internal/sourcestore/  - Source code tarball storage
internal/k8s/          - Kubernetes resource builders
web/                   - Next.js dashboard
config/                - Kubernetes manifests (CRDs, RBAC, deployment)
```

## Code Conventions

- MCP tools, prompts, and resources each get their own file with a `RegisterXxx(server, deps)` function
- All registration functions take `(*gomcp.Server, *tools.Dependencies)`
- Use the `tools.Dependencies` struct from `internal/mcp/tools/deps.go` for shared dependencies
- Use table-driven tests with `t.Run` subtests
- Keep error messages lowercase and descriptive

## Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Make your changes with tests
4. Run `make test` and ensure all tests pass
5. Run `make build` to verify compilation
6. Commit your changes with a clear commit message
7. Push to your fork and open a pull request

### Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if behavior changes
- Describe what the PR does and why in the description

## Reporting Issues

Please use GitHub Issues to report bugs or request features. Include:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Relevant logs or error messages

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
