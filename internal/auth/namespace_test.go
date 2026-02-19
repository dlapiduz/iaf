package auth

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	if err := EnsureNamespace(ctx, k8sClient, "iaf-test123"); err != nil {
		t.Fatal(err)
	}

	// Verify namespace was created
	var ns corev1.Namespace
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "iaf-test123"}, &ns); err != nil {
		t.Fatalf("namespace not created: %v", err)
	}
	if ns.Labels["app.kubernetes.io/managed-by"] != "iaf" {
		t.Error("expected managed-by label")
	}

	// Verify service account was created
	var sa corev1.ServiceAccount
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "iaf-kpack-sa", Namespace: "iaf-test123"}, &sa); err != nil {
		t.Fatalf("service account not created: %v", err)
	}
}

func TestEnsureNamespaceIdempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	// Call twice — should not error
	if err := EnsureNamespace(ctx, k8sClient, "iaf-test123"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNamespace(ctx, k8sClient, "iaf-test123"); err != nil {
		t.Fatalf("second call should be idempotent: %v", err)
	}
}
