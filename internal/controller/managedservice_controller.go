package controller

import (
	"context"
	"fmt"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	iafk8s "github.com/dlapiduz/iaf/internal/k8s"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const managedServiceFinalizer = "iaf.io/managed-service-protection"

// +kubebuilder:rbac:groups=iaf.io,resources=managedservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=iaf.io,resources=managedservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=iaf.io,resources=managedservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// ManagedServiceReconciler reconciles ManagedService CRs.
type ManagedServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is the main reconciliation loop for ManagedService CRs.
func (r *ManagedServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var svc iafv1alpha1.ManagedService
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting managed service: %w", err)
	}

	// Handle deletion.
	if !svc.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &svc)
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(&svc, managedServiceFinalizer) {
		controllerutil.AddFinalizer(&svc, managedServiceFinalizer)
		if err := r.Update(ctx, &svc); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		// Requeue to continue reconciliation after adding finalizer.
		return ctrl.Result{Requeue: true}, nil
	}

	// Create or update the CNPG Cluster CR.
	if err := r.reconcileCNPGCluster(ctx, &svc); err != nil {
		return ctrl.Result{}, err
	}

	// Create or update the NetworkPolicy.
	if err := r.reconcileNetworkPolicy(ctx, &svc); err != nil {
		return ctrl.Result{}, err
	}

	// Read cluster status and mirror it to ManagedService.Status.
	phase, secretName, err := r.readClusterStatus(ctx, &svc)
	if err != nil {
		logger.V(1).Info("cluster status not yet available", "error", err)
		phase = string(iafv1alpha1.ManagedServicePhaseProvisioning)
	}

	svc.Status.Phase = iafv1alpha1.ManagedServicePhase(phase)
	if phase == string(iafv1alpha1.ManagedServicePhaseReady) {
		svc.Status.ConnectionSecretRef = secretName
		svc.Status.Message = "Service is ready. Use bind_service to inject credentials into an application."
	} else {
		svc.Status.Message = "Provisioning in progress. Poll service_status every 10s."
	}

	if err := r.Status().Update(ctx, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating managed service status: %w", err)
	}

	if phase != string(iafv1alpha1.ManagedServicePhaseReady) {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// reconcileDelete handles deletion of a ManagedService, enforcing the finalizer guard.
func (r *ManagedServiceReconciler) reconcileDelete(ctx context.Context, svc *iafv1alpha1.ManagedService) (ctrl.Result, error) {
	if len(svc.Status.BoundApps) > 0 {
		// Keep finalizer: service has bound apps.
		svc.Status.Phase = iafv1alpha1.ManagedServicePhaseFailed
		svc.Status.Message = fmt.Sprintf(
			"Cannot delete: service is still bound to applications %v. Use unbind_service to remove all bindings first.",
			svc.Status.BoundApps,
		)
		if err := r.Status().Update(ctx, svc); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status for blocked deletion: %w", err)
		}
		return ctrl.Result{}, fmt.Errorf("service %q still bound to applications %v", svc.Name, svc.Status.BoundApps)
	}

	// Safe to remove finalizer — owner references will cascade delete CNPG Cluster + NetworkPolicy.
	controllerutil.RemoveFinalizer(svc, managedServiceFinalizer)
	if err := r.Update(ctx, svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// reconcileCNPGCluster creates or updates the CloudNativePG Cluster CR.
func (r *ManagedServiceReconciler) reconcileCNPGCluster(ctx context.Context, svc *iafv1alpha1.ManagedService) error {
	desired := iafk8s.BuildCNPGCluster(svc)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(iafk8s.CNPGClusterGVK)
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting CNPG cluster: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating CNPG cluster: %w", err)
		}
		return nil
	}
	existing.Object["spec"] = desired.Object["spec"]
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating CNPG cluster: %w", err)
	}
	return nil
}

// reconcileNetworkPolicy creates or updates the NetworkPolicy for the CNPG cluster.
func (r *ManagedServiceReconciler) reconcileNetworkPolicy(ctx context.Context, svc *iafv1alpha1.ManagedService) error {
	desired := iafk8s.BuildNetworkPolicy(svc)

	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: svc.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting network policy: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating network policy: %w", err)
		}
		return nil
	}
	existing.Spec = desired.Spec
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating network policy: %w", err)
	}
	return nil
}

// readClusterStatus fetches the CNPG Cluster CR and extracts its phase and secret name.
func (r *ManagedServiceReconciler) readClusterStatus(ctx context.Context, svc *iafv1alpha1.ManagedService) (phase, secretName string, err error) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(iafk8s.CNPGClusterGVK)
	if err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, existing); err != nil {
		return "", "", err
	}
	ph, sec := iafk8s.GetCNPGClusterStatus(existing)
	return ph, sec, nil
}

// SetupWithManager registers the controller with the manager.
func (r *ManagedServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&iafv1alpha1.ManagedService{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Complete(r)
}
