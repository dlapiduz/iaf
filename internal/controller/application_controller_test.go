package controller

import (
	"context"
	"testing"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := iafv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func newReconciler(scheme *runtime.Scheme, objs ...runtime.Object) *ApplicationReconciler {
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.Application{}).
		Build()

	return &ApplicationReconciler{
		Client:         k8sClient,
		Scheme:         scheme,
		ClusterBuilder: "default",
		RegistryPrefix: "registry.example.com",
		BaseDomain:     "example.com",
	}
}

func makeApp(name, namespace string) *iafv1alpha1.Application {
	return &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid",
		},
		Spec: iafv1alpha1.ApplicationSpec{
			Image:    "nginx:latest",
			Port:     8080,
			Replicas: 1,
		},
	}
}

func reconcileApp(t *testing.T, r *ApplicationReconciler, name, namespace string) ctrl.Result {
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

// TestReconcile_ImageApp_SetsDeployingThenRunning verifies the phase progression
// for a pre-built image app: Pending → Deploying after first reconcile, then
// Running once the Deployment has available replicas.
func TestReconcile_ImageApp_SetsDeployingThenRunning(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	app := makeApp("myapp", "test-ns")
	if err := r.Create(ctx, app); err != nil {
		t.Fatal(err)
	}

	// First reconcile: app has no Deployment yet → should set Deploying.
	reconcileApp(t, r, "myapp", "test-ns")

	var result iafv1alpha1.Application
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status.Phase != iafv1alpha1.ApplicationPhaseDeploying {
		t.Errorf("expected phase Deploying after first reconcile, got %q", result.Status.Phase)
	}
	if result.Status.URL == "" {
		t.Error("expected URL to be set during Deploying phase")
	}

	// Verify a Deployment was created.
	var dep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &dep); err != nil {
		t.Fatalf("expected Deployment to be created: %v", err)
	}

	// Simulate the Deployment becoming available (1 pod ready).
	dep.Status.AvailableReplicas = 1
	if err := r.Status().Update(ctx, &dep); err != nil {
		t.Fatal(err)
	}

	// Second reconcile: should set Running.
	reconcileApp(t, r, "myapp", "test-ns")

	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status.Phase != iafv1alpha1.ApplicationPhaseRunning {
		t.Errorf("expected phase Running after replicas available, got %q", result.Status.Phase)
	}
	if result.Status.AvailableReplicas != 1 {
		t.Errorf("expected availableReplicas=1, got %d", result.Status.AvailableReplicas)
	}
}

// TestReconcile_AvailableReplicasAlwaysAccurate verifies that availableReplicas
// is updated on every reconcile pass.
func TestReconcile_AvailableReplicasAlwaysAccurate(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	app := makeApp("myapp", "test-ns")
	if err := r.Create(ctx, app); err != nil {
		t.Fatal(err)
	}

	// First reconcile to create Deployment.
	reconcileApp(t, r, "myapp", "test-ns")

	var dep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &dep); err != nil {
		t.Fatal(err)
	}

	// Set replicas to 2.
	dep.Status.AvailableReplicas = 2
	if err := r.Status().Update(ctx, &dep); err != nil {
		t.Fatal(err)
	}

	reconcileApp(t, r, "myapp", "test-ns")

	var result iafv1alpha1.Application
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status.AvailableReplicas != 2 {
		t.Errorf("expected availableReplicas=2, got %d", result.Status.AvailableReplicas)
	}
}

// TestReconcile_RunningToDeploying verifies that if replicas drop to 0 while
// the app is Running, the controller transitions back to Deploying.
func TestReconcile_RunningToDeploying(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	app := makeApp("myapp", "test-ns")
	if err := r.Create(ctx, app); err != nil {
		t.Fatal(err)
	}

	// Reconcile and get the Deployment ready.
	reconcileApp(t, r, "myapp", "test-ns")

	var dep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &dep); err != nil {
		t.Fatal(err)
	}
	dep.Status.AvailableReplicas = 1
	if err := r.Status().Update(ctx, &dep); err != nil {
		t.Fatal(err)
	}

	// Reconcile to Running.
	reconcileApp(t, r, "myapp", "test-ns")

	var result iafv1alpha1.Application
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status.Phase != iafv1alpha1.ApplicationPhaseRunning {
		t.Fatalf("expected Running, got %q", result.Status.Phase)
	}

	// Re-fetch the Deployment to get the latest resourceVersion before updating status.
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &dep); err != nil {
		t.Fatal(err)
	}

	// Simulate pods crashing (replicas drop to 0).
	dep.Status.AvailableReplicas = 0
	if err := r.Status().Update(ctx, &dep); err != nil {
		t.Fatal(err)
	}

	// Reconcile should regress to Deploying.
	res := reconcileApp(t, r, "myapp", "test-ns")

	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status.Phase != iafv1alpha1.ApplicationPhaseDeploying {
		t.Errorf("expected Deploying after replicas dropped to 0, got %q", result.Status.Phase)
	}
	if res.RequeueAfter != 10*time.Second {
		t.Errorf("expected RequeueAfter=10s, got %v", res.RequeueAfter)
	}
}

// TestReconcile_DeployingRequeues verifies that the controller requeues with
// a 10s delay when in Deploying phase with no available replicas.
func TestReconcile_DeployingRequeues(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	app := makeApp("myapp", "test-ns")
	if err := r.Create(ctx, app); err != nil {
		t.Fatal(err)
	}

	// First reconcile: Deploying with 0 available replicas.
	res := reconcileApp(t, r, "myapp", "test-ns")

	if res.RequeueAfter != 10*time.Second {
		t.Errorf("expected RequeueAfter=10s while Deploying, got %v", res.RequeueAfter)
	}
}

// TestReconcile_CreatesService verifies a Service is created alongside the Deployment.
func TestReconcile_CreatesService(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	app := makeApp("myapp", "test-ns")
	if err := r.Create(ctx, app); err != nil {
		t.Fatal(err)
	}

	reconcileApp(t, r, "myapp", "test-ns")

	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &svc); err != nil {
		t.Fatalf("expected Service to be created: %v", err)
	}
	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != 8080 {
		t.Errorf("expected Service port 8080, got %v", svc.Spec.Ports)
	}
}

// TestReconcile_URLSetDuringDeploying verifies the URL field is populated
// during the Deploying phase, not just at Running.
func TestReconcile_URLSetDuringDeploying(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	app := makeApp("myapp", "test-ns")
	if err := r.Create(ctx, app); err != nil {
		t.Fatal(err)
	}

	reconcileApp(t, r, "myapp", "test-ns")

	var result iafv1alpha1.Application
	if err := r.Get(ctx, types.NamespacedName{Name: "myapp", Namespace: "test-ns"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status.URL == "" {
		t.Error("expected URL to be set during Deploying phase")
	}
	expected := "http://myapp.example.com"
	if result.Status.URL != expected {
		t.Errorf("expected URL %q, got %q", expected, result.Status.URL)
	}
}

// TestReconcile_NotFound is a sanity check that reconciling a deleted app
// does not return an error.
func TestReconcile_NotFound(t *testing.T) {
	scheme := newTestScheme(t)
	r := newReconciler(scheme)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone", Namespace: "test-ns"},
	})
	if err != nil {
		t.Fatalf("expected no error for missing app, got %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Errorf("expected empty result for missing app, got %+v", result)
	}
}
