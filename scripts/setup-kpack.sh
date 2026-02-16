#!/usr/bin/env bash
set -euo pipefail

# IAF kpack Setup Script
# This script installs kpack and configures builders for automatic image builds.
# Prerequisites: Kubernetes cluster running, kubectl available.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

KPACK_VERSION="0.15.1"

echo "=== IAF kpack Setup ==="
echo ""

# 1. Install kpack
echo "--- Installing kpack v${KPACK_VERSION} ---"
kubectl apply -f "https://github.com/buildpacks-community/kpack/releases/download/v${KPACK_VERSION}/release-${KPACK_VERSION}.yaml"
echo "kpack installed. Waiting for kpack controller to be ready..."
kubectl rollout status deployment/kpack-controller -n kpack --timeout=180s
echo "kpack controller is ready."

# 2. Apply kpack configuration (ClusterStore, ClusterStack, ClusterBuilder)
echo ""
echo "--- Configuring kpack builders ---"
kubectl apply -f "$ROOT_DIR/config/kpack/"
echo "kpack builders configured."

# 3. Verify setup
echo ""
echo "--- Verifying kpack setup ---"
echo "ClusterStore:"
kubectl get clusterstore iaf-cluster-store 2>/dev/null || echo "  (not yet ready)"
echo "ClusterStack:"
kubectl get clusterstack iaf-cluster-stack 2>/dev/null || echo "  (not yet ready)"
echo "ClusterBuilder:"
kubectl get clusterbuilder iaf-cluster-builder 2>/dev/null || echo "  (not yet ready)"

echo ""
echo "=== kpack setup complete ==="
echo ""
echo "kpack will now automatically build container images from source code."
echo "Supported languages: Go, Node.js, Python, Java, Ruby"
