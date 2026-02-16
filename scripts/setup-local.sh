#!/usr/bin/env bash
set -euo pipefail

# IAF Local Development Setup Script
# This script sets up the local development environment on Rancher Desktop with Kubernetes.
# Prerequisites: Rancher Desktop running with Kubernetes enabled, kubectl, and helm.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== IAF Local Development Setup ==="
echo ""

# 1. Create namespaces
echo "--- Creating namespaces ---"
kubectl create namespace iaf-system --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace iaf-apps --dry-run=client -o yaml | kubectl apply -f -
echo "Namespaces created."

# 2. Deploy local container registry
echo ""
echo "--- Deploying local container registry ---"
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry
  namespace: iaf-system
  labels:
    app: registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: registry
  template:
    metadata:
      labels:
        app: registry
    spec:
      containers:
        - name: registry
          image: registry:2
          ports:
            - containerPort: 5000
          volumeMounts:
            - name: registry-data
              mountPath: /var/lib/registry
          env:
            - name: REGISTRY_STORAGE_DELETE_ENABLED
              value: "true"
      volumes:
        - name: registry-data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: registry
  namespace: iaf-system
spec:
  selector:
    app: registry
  ports:
    - port: 5000
      targetPort: 5000
  type: ClusterIP
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: registry
  namespace: iaf-system
spec:
  entryPoints:
    - web
  routes:
    - match: Host(`registry.localhost`)
      kind: Rule
      services:
        - name: registry
          port: 5000
EOF
echo "Local container registry deployed at registry.localhost:5000"

# 3. Install CRDs
echo ""
echo "--- Installing IAF CRDs ---"
kubectl apply -f "$ROOT_DIR/config/crd/bases/"
echo "CRDs installed."

# 4. Create RBAC
echo ""
echo "--- Setting up RBAC ---"
kubectl apply -f "$ROOT_DIR/config/rbac/"
echo "RBAC configured."

# 5. Create service account for kpack in iaf-apps namespace
echo ""
echo "--- Creating kpack service account ---"
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: iaf-kpack-sa
  namespace: iaf-apps
---
apiVersion: v1
kind: Secret
metadata:
  name: iaf-registry-credentials
  namespace: iaf-apps
  annotations:
    kpack.io/docker: registry.localhost:5000
type: kubernetes.io/basic-auth
stringData:
  username: ""
  password: ""
EOF

# Patch the service account to use the registry secret
kubectl patch serviceaccount iaf-kpack-sa -n iaf-apps \
  -p '{"secrets": [{"name": "iaf-registry-credentials"}]}' 2>/dev/null || true

echo "kpack service account created."

# 6. Wait for registry to be ready
echo ""
echo "--- Waiting for registry to be ready ---"
kubectl rollout status deployment/registry -n iaf-system --timeout=120s
echo "Registry is ready."

echo ""
echo "=== Local setup complete ==="
echo ""
echo "Next steps:"
echo "  1. Run 'make setup-kpack' to install kpack and configure builders"
echo "  2. Run 'make run-controller' to start the IAF controller"
echo "  3. Run 'make run-apiserver' to start the API server"
echo "  4. Run 'make run-mcpserver' to start the MCP server"
echo ""
echo "Registry: http://registry.localhost:5000"
echo "API Server: http://localhost:8080"
