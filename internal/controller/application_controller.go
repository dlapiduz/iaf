// Package controller implements the Application CRD controller.
package controller

import (
	"context"
	"fmt"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	iafk8s "github.com/dlapiduz/iaf/internal/k8s"
	iafvalidation "github.com/dlapiduz/iaf/internal/validation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=iaf.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=iaf.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=iaf.io,resources=applications/finalizers,verbs=update
// +kubebuilder:rbac:groups=iaf.io,resources=datasources,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;get;list;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;get;update;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create;get
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups=kpack.io,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=traefik.io,resources=ingressroutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

// managedServicePGEnvVars maps CNPG Secret keys to PG* environment variable names
// injected when a ManagedService is bound to an Application.
var managedServicePGEnvVars = map[string]string{
	"uri":      "DATABASE_URL",
	"host":     "PGHOST",
	"port":     "PGPORT",
	"dbname":   "PGDATABASE",
	"username": "PGUSER",
	"password": "PGPASSWORD",
}

// ApplicationReconciler reconciles Application CRs.
type ApplicationReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ClusterBuilder string
	RegistryPrefix string
	BaseDomain     string
	// TLSIssuer is the name of the ClusterIssuer used to provision TLS certificates.
	// Defaults to "selfsigned-issuer". Set to "" to disable certificate reconciliation
	// (e.g., when cert-manager is not installed).
	TLSIssuer string
}

// Reconcile is the main reconciliation loop for Application CRs.
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var app iafv1alpha1.Application
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting application: %w", err)
	}

	// Resolve the container image to deploy.
	image, buildStatus, err := r.resolveImage(ctx, &app)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If we are still waiting for a build, update build status and requeue.
	if image == "" {
		if err := r.setBuildingStatus(ctx, &app, buildStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Set Deploying phase before creating/updating the Deployment (if not already past that).
	if app.Status.Phase == iafv1alpha1.ApplicationPhaseBuilding ||
		app.Status.Phase == iafv1alpha1.ApplicationPhasePending ||
		app.Status.Phase == "" {
		if err := r.setDeployingPhaseOnly(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// TLS requires both the app opting in (default true) AND a TLSIssuer being configured.
	// When TLSIssuer is empty (cert-manager not installed) the controller degrades gracefully
	// to HTTP-only mode without crashing.
	tlsEnabled := iafv1alpha1.IsTLSEnabled(&app) && r.TLSIssuer != ""

	// Create or update the Deployment, Service, Certificate, and IngressRoute.
	dep, err := r.reconcileDeployment(ctx, &app, image)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileService(ctx, &app); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileCertificate(ctx, &app, tlsEnabled); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileIngressRoute(ctx, &app, tlsEnabled); err != nil {
		return ctrl.Result{}, err
	}

	// Update status based on current Deployment availability.
	return r.reconcileStatus(ctx, &app, image, buildStatus, dep, tlsEnabled)
}

// resolveImage returns the container image to deploy.
// For pre-built images, it returns immediately. For kpack builds, it reads
// the kpack Image CR status. Returns ("", ...) while the build is in progress.
func (r *ApplicationReconciler) resolveImage(ctx context.Context, app *iafv1alpha1.Application) (image, buildStatus string, err error) {
	if app.Spec.Image != "" {
		return app.Spec.Image, "NotRequired", nil
	}

	if app.Spec.Git == nil && app.Spec.Blob == "" {
		return "", "Unknown", fmt.Errorf("application %q has no image, git, or blob source", app.Name)
	}

	// Ensure kpack Image CR exists.
	kpackImage := iafk8s.BuildKpackImage(app, r.ClusterBuilder, r.RegistryPrefix)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(iafk8s.KpackImageGVK)
	err = r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return "", "", fmt.Errorf("getting kpack image: %w", err)
		}
		if err := r.Create(ctx, kpackImage); err != nil && !apierrors.IsAlreadyExists(err) {
			return "", "", fmt.Errorf("creating kpack image: %w", err)
		}
		return "", "Building", nil
	}

	// Update source URL if the blob changed (re-push).
	existingSpec, _ := existing.Object["spec"].(map[string]any)
	newSpec := kpackImage.Object["spec"].(map[string]any)
	existingSource, _ := existingSpec["source"].(map[string]any)
	newSource, _ := newSpec["source"].(map[string]any)
	if fmt.Sprintf("%v", existingSource) != fmt.Sprintf("%v", newSource) {
		existing.Object["spec"] = newSpec
		if err := r.Update(ctx, existing); err != nil {
			return "", "", fmt.Errorf("updating kpack image: %w", err)
		}
	}

	buildSt, latestImage := iafk8s.GetKpackImageStatus(existing)
	if latestImage == "" {
		return "", buildSt, nil
	}
	return latestImage, buildSt, nil
}

// setBuildingStatus updates the Application status to Building phase.
func (r *ApplicationReconciler) setBuildingStatus(ctx context.Context, app *iafv1alpha1.Application, buildStatus string) error {
	app.Status.Phase = iafv1alpha1.ApplicationPhaseBuilding
	app.Status.BuildStatus = buildStatus
	setCondition(app, "Ready", metav1.ConditionFalse, "Building", "Waiting for container image build to complete")
	return r.Status().Update(ctx, app)
}

// setDeployingPhaseOnly sets phase to Deploying without touching AvailableReplicas.
// Called once before reconcileDeployment to give agents an accurate intermediate state.
func (r *ApplicationReconciler) setDeployingPhaseOnly(ctx context.Context, app *iafv1alpha1.Application) error {
	app.Status.Phase = iafv1alpha1.ApplicationPhaseDeploying
	setCondition(app, "Ready", metav1.ConditionFalse, "Deploying", "Waiting for pod replicas to become available")
	return r.Status().Update(ctx, app)
}

// reconcileDeployment creates or updates the Deployment for the application.
// Returns the current Deployment object (with up-to-date status).
func (r *ApplicationReconciler) reconcileDeployment(ctx context.Context, app *iafv1alpha1.Application, image string) (*appsv1.Deployment, error) {
	port := app.Spec.Port
	if port == 0 {
		port = 8080
	}
	replicas := app.Spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	envVars := make([]corev1.EnvVar, 0, len(app.Spec.Env))
	for _, e := range app.Spec.Env {
		envVars = append(envVars, corev1.EnvVar{Name: e.Name, Value: e.Value})
	}

	// Inject env vars from attached data sources.
	logger := log.FromContext(ctx)
	for _, ads := range app.Spec.AttachedDataSources {
		var ds iafv1alpha1.DataSource
		if err := r.Get(ctx, types.NamespacedName{Name: ads.DataSourceName}, &ds); err != nil {
			if apierrors.IsNotFound(err) {
				// DataSource may have been deleted after attachment — skip gracefully.
				logger.V(1).Info("DataSource not found, skipping env injection", "datasource", ads.DataSourceName)
				continue
			}
			return nil, fmt.Errorf("getting datasource %q: %w", ads.DataSourceName, err)
		}
		for secretKey, envVarName := range ds.Spec.EnvVarMapping {
			if err := iafvalidation.ValidateEnvVarName(envVarName); err != nil {
				// Defence-in-depth: skip invalid env var names added by misconfigured operators.
				logger.V(1).Info("invalid env var name in DataSource mapping, skipping",
					"datasource", ads.DataSourceName, "envVarName", envVarName)
				continue
			}
			envVars = append(envVars, corev1.EnvVar{
				Name: envVarName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ads.SecretName},
						Key:                 secretKey,
					},
				},
			})
		}
	}

	// Inject env vars from bound managed services (postgres: CNPG secret keys → PG* env vars).
	for _, bms := range app.Spec.BoundManagedServices {
		for secretKey, envVarName := range managedServicePGEnvVars {
			envVars = append(envVars, corev1.EnvVar{
				Name: envVarName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: bms.SecretName},
						Key:                 secretKey,
					},
				},
			})
		}
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "iaf",
				"iaf.io/application":          app.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: iafv1alpha1.GroupVersion.String(),
					Kind:       "Application",
					Name:       app.Name,
					UID:        app.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"iaf.io/application": app.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"iaf.io/application": app.Name},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
					},
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: port, Protocol: corev1.ProtocolTCP},
							},
							Env: envVars,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: boolPtr(false),
							},
						},
					},
				},
			},
		},
	}

	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("getting deployment: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil {
			return nil, fmt.Errorf("creating deployment: %w", err)
		}
		// Return the just-created Deployment (status will be empty, so available=0).
		return desired, nil
	}

	// Update the existing Deployment spec.
	existing.Spec = desired.Spec
	if err := r.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("updating deployment: %w", err)
	}
	return existing, nil
}

// reconcileService creates or updates the Service for the application.
func (r *ApplicationReconciler) reconcileService(ctx context.Context, app *iafv1alpha1.Application) error {
	port := app.Spec.Port
	if port == 0 {
		port = 8080
	}

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "iaf",
				"iaf.io/application":          app.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: iafv1alpha1.GroupVersion.String(),
					Kind:       "Application",
					Name:       app.Name,
					UID:        app.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"iaf.io/application": app.Name},
			Ports: []corev1.ServicePort{
				{Port: port, Protocol: corev1.ProtocolTCP},
			},
		},
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting service: %w", err)
		}
		return r.Create(ctx, desired)
	}
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	return r.Update(ctx, existing)
}

// reconcileCertificate creates or updates the cert-manager Certificate for the application.
// It is a no-op when TLS is disabled or when TLSIssuer is not configured (cert-manager absent).
func (r *ApplicationReconciler) reconcileCertificate(ctx context.Context, app *iafv1alpha1.Application, tlsEnabled bool) error {
	if !tlsEnabled || r.TLSIssuer == "" {
		return nil
	}

	host := app.Spec.Host
	if host == "" {
		host = fmt.Sprintf("%s.%s", app.Name, r.BaseDomain)
	}

	desired := iafk8s.BuildCertificate(app, host, r.TLSIssuer)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(iafk8s.CertificateGVK)
	err := r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting certificate: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating certificate: %w", err)
		}
		return nil
	}
	existing.Object["spec"] = desired.Object["spec"]
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating certificate: %w", err)
	}
	return nil
}

// reconcileIngressRoute creates or updates the Traefik IngressRoute for the application.
func (r *ApplicationReconciler) reconcileIngressRoute(ctx context.Context, app *iafv1alpha1.Application, tlsEnabled bool) error {
	desired := iafk8s.BuildIngressRoute(app, r.BaseDomain, tlsEnabled)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(iafk8s.TraefikIngressRouteGVK)
	err := r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting ingressroute: %w", err)
		}
		return r.Create(ctx, desired)
	}
	existing.Object["spec"] = desired.Object["spec"]
	return r.Update(ctx, existing)
}

// reconcileStatus reads the current Deployment availability and updates the Application status.
// It sets phase to Running if at least one replica is available, or Deploying otherwise.
func (r *ApplicationReconciler) reconcileStatus(ctx context.Context, app *iafv1alpha1.Application, image, buildStatus string, dep *appsv1.Deployment, tlsEnabled bool) (ctrl.Result, error) {
	available := dep.Status.AvailableReplicas

	host := app.Spec.Host
	if host == "" {
		host = fmt.Sprintf("%s.%s", app.Name, r.BaseDomain)
	}

	scheme := "https"
	if !tlsEnabled {
		scheme = "http"
	}

	// Always write accurate status fields.
	app.Status.AvailableReplicas = available
	app.Status.LatestImage = image
	app.Status.BuildStatus = buildStatus
	app.Status.URL = fmt.Sprintf("%s://%s", scheme, host)

	if available >= 1 {
		app.Status.Phase = iafv1alpha1.ApplicationPhaseRunning
		setCondition(app, "Ready", metav1.ConditionTrue, "Available", fmt.Sprintf("%d replica(s) available", available))
		if err := r.Status().Update(ctx, app); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status to Running: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// No replicas available: stay in (or return to) Deploying.
	app.Status.Phase = iafv1alpha1.ApplicationPhaseDeploying
	setCondition(app, "Ready", metav1.ConditionFalse, "Deploying", "Waiting for pod replicas to become available")
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status to Deploying: %w", err)
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// SetupWithManager registers the controller with the manager and configures watches.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch kpack Image CRs so build completion triggers immediate reconciliation.
	kpackImageType := &unstructured.Unstructured{}
	kpackImageType.SetGroupVersionKind(iafk8s.KpackImageGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(&iafv1alpha1.Application{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(
			kpackImageType,
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&iafv1alpha1.Application{},
				handler.OnlyControllerOwner(),
			),
		).
		Complete(r)
}

// setCondition upserts a condition on the Application status.
func setCondition(app *iafv1alpha1.Application, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range app.Status.Conditions {
		if c.Type == condType {
			app.Status.Conditions[i].Status = status
			app.Status.Conditions[i].Reason = reason
			app.Status.Conditions[i].Message = message
			app.Status.Conditions[i].LastTransitionTime = now
			return
		}
	}
	app.Status.Conditions = append(app.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}

func boolPtr(b bool) *bool { return &b }
