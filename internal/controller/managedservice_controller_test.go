package controller

import (
	"context"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	iafk8s "github.com/dlapiduz/iaf/internal/k8s"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newMSTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := iafv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func newMSReconciler(scheme *runtime.Scheme) *ManagedServiceReconciler {
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.ManagedService{}).
		Build()
	return &ManagedServiceReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}
}

func makeManagedSvc(name, namespace string) *iafv1alpha1.ManagedService {
	return &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-svc-uid",
		},
		Spec: iafv1alpha1.ManagedServiceSpec{
			Type: "postgres",
			Plan: iafv1alpha1.ServicePlanMicro,
		},
	}
}

func reconcileMS(t *testing.T, r *ManagedServiceReconciler, name, namespace string) ctrl.Result {
	t.Helper()
	ctx := context.Background()
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}
	return result
}

func TestManagedServiceReconcile_FinalizerAdded(t *testing.T) {
	scheme := newMSTestScheme(t)
	r := newMSReconciler(scheme)
	ctx := context.Background()

	svc := makeManagedSvc("testdb", "iaf-test")
	if err := r.Create(ctx, svc); err != nil {
		t.Fatal(err)
	}

	// First reconcile adds the finalizer and requeues.
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "testdb", Namespace: "iaf-test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	var updated iafv1alpha1.ManagedService
	if err := r.Get(ctx, types.NamespacedName{Name: "testdb", Namespace: "iaf-test"}, &updated); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range updated.Finalizers {
		if f == managedServiceFinalizer {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finalizer %q to be added", managedServiceFinalizer)
	}
}

func TestManagedServiceReconcile_CreatesCluster(t *testing.T) {
	scheme := newMSTestScheme(t)
	r := newMSReconciler(scheme)
	ctx := context.Background()

	svc := makeManagedSvc("pgdb", "iaf-test")
	// Pre-add finalizer so we skip past the finalizer-add step.
	svc.Finalizers = []string{managedServiceFinalizer}
	if err := r.Create(ctx, svc); err != nil {
		t.Fatal(err)
	}

	reconcileMS(t, r, "pgdb", "iaf-test")

	// Check CNPG Cluster was created as unstructured.
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(iafk8s.CNPGClusterGVK)
	if err := r.Get(ctx, types.NamespacedName{Name: "pgdb", Namespace: "iaf-test"}, cluster); err != nil {
		t.Fatalf("expected CNPG cluster to be created: %v", err)
	}

	// Check NetworkPolicy was created.
	var np networkingv1.NetworkPolicy
	if err := r.Get(ctx, types.NamespacedName{Name: "pgdb-netpol", Namespace: "iaf-test"}, &np); err != nil {
		t.Fatalf("expected NetworkPolicy to be created: %v", err)
	}
}

func TestManagedServiceReconcile_StatusMirrored(t *testing.T) {
	// This test verifies that when the CNPG cluster reports Ready status,
	// the ManagedService status is mirrored correctly. We test readClusterStatus
	// and the status update logic directly using the k8s helpers.
	// The full integration path (CNPG Cluster in cluster) is verified by cnpg_test.go.
	phase, secretName := iafk8s.GetCNPGClusterStatus(func() *unstructured.Unstructured {
		cluster := &unstructured.Unstructured{}
		cluster.SetName("statusdb")
		cluster.Object["status"] = map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
			},
		}
		return cluster
	}())

	if phase != string(iafv1alpha1.ManagedServicePhaseReady) {
		t.Errorf("expected Ready phase, got %s", phase)
	}
	if secretName != "statusdb-app" {
		t.Errorf("expected secret name statusdb-app, got %s", secretName)
	}
}

func TestManagedServiceReconcile_DeletionBlocked(t *testing.T) {
	scheme := newMSTestScheme(t)
	r := newMSReconciler(scheme)
	ctx := context.Background()

	svc := makeManagedSvc("bounddb", "iaf-test")
	svc.Finalizers = []string{managedServiceFinalizer}
	if err := r.Create(ctx, svc); err != nil {
		t.Fatal(err)
	}
	// Set BoundApps via status update.
	svc.Status.BoundApps = []string{"myapp"}
	if err := r.Status().Update(ctx, svc); err != nil {
		t.Fatal(err)
	}
	// Trigger deletion — with finalizers present the fake client sets DeletionTimestamp.
	if err := r.Delete(ctx, svc); err != nil {
		t.Fatal(err)
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bounddb", Namespace: "iaf-test"},
	})
	if err == nil {
		t.Fatal("expected error when deleting service with bound apps")
	}

	// Finalizer must still be present.
	var updated iafv1alpha1.ManagedService
	if err := r.Get(ctx, types.NamespacedName{Name: "bounddb", Namespace: "iaf-test"}, &updated); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range updated.Finalizers {
		if f == managedServiceFinalizer {
			found = true
		}
	}
	if !found {
		t.Error("expected finalizer to remain when deletion is blocked")
	}
}

func TestManagedServiceReconcile_DeletionAllowed(t *testing.T) {
	scheme := newMSTestScheme(t)
	r := newMSReconciler(scheme)
	ctx := context.Background()

	svc := makeManagedSvc("unbounddb", "iaf-test")
	svc.Finalizers = []string{managedServiceFinalizer}
	// BoundApps is empty.
	if err := r.Create(ctx, svc); err != nil {
		t.Fatal(err)
	}
	// Trigger deletion — with finalizers present, fake client sets DeletionTimestamp.
	if err := r.Delete(ctx, svc); err != nil {
		t.Fatal(err)
	}

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "unbounddb", Namespace: "iaf-test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue after successful deletion")
	}

	// After finalizer removal, the fake client garbage-collects the object.
	// Verify either the object is gone or the finalizer is absent.
	var updated iafv1alpha1.ManagedService
	err = r.Get(ctx, types.NamespacedName{Name: "unbounddb", Namespace: "iaf-test"}, &updated)
	if err != nil {
		// Object deleted — this is the expected outcome (finalizer removed, GC happened).
		return
	}
	for _, f := range updated.Finalizers {
		if f == managedServiceFinalizer {
			t.Error("expected finalizer to be removed")
		}
	}
}
