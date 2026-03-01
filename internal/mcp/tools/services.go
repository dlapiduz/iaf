package tools

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/validation"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// serviceEnvVarNames is the canonical list of env vars injected when binding a postgres service.
var serviceEnvVarNames = []string{
	"DATABASE_URL",
	"PGHOST",
	"PGPORT",
	"PGDATABASE",
	"PGUSER",
	"PGPASSWORD",
}

// validServiceTypes is the set of supported managed service types.
var validServiceTypes = map[string]bool{
	"postgres": true,
}

// validServicePlans is the set of supported service plans.
var validServicePlans = map[iafv1alpha1.ServicePlan]bool{
	iafv1alpha1.ServicePlanMicro: true,
	iafv1alpha1.ServicePlanSmall: true,
	iafv1alpha1.ServicePlanHA:    true,
}

// --- provision_service ---

type ProvisionServiceInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - service name (lowercase, hyphens allowed)"`
	Type      string `json:"type" jsonschema:"required - service type: 'postgres'"`
	Plan      string `json:"plan" jsonschema:"required - service plan: 'micro' (1 instance, 1Gi), 'small' (1 instance, 5Gi), 'ha' (3 instances, 10Gi)"`
}

// RegisterProvisionService registers the provision_service MCP tool.
func RegisterProvisionService(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "provision_service",
		Description: "Provision a managed backing service (e.g. PostgreSQL). Returns immediately; the service provisions asynchronously. Poll service_status every 10s until phase is Ready, then use bind_service to connect it to an application.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input ProvisionServiceInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid service name: %w", err)
		}
		if !validServiceTypes[input.Type] {
			return nil, nil, fmt.Errorf("unsupported service type %q — supported types: postgres", input.Type)
		}
		plan := iafv1alpha1.ServicePlan(input.Plan)
		if !validServicePlans[plan] {
			return nil, nil, fmt.Errorf("unsupported plan %q — supported plans: micro, small, ha", input.Plan)
		}

		svc := &iafv1alpha1.ManagedService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      input.Name,
				Namespace: namespace,
			},
			Spec: iafv1alpha1.ManagedServiceSpec{
				Type: input.Type,
				Plan: plan,
			},
		}
		if err := deps.Client.Create(ctx, svc); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, nil, fmt.Errorf("service %q already exists", input.Name)
			}
			return nil, nil, fmt.Errorf("provisioning service: %w", err)
		}

		result := map[string]any{
			"name":    input.Name,
			"type":    input.Type,
			"plan":    input.Plan,
			"message": "Provisioning started — poll service_status every 10s until phase is Ready, then use bind_service to connect it to an application.",
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// --- service_status ---

type ServiceStatusInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - service name"`
}

// RegisterServiceStatus registers the service_status MCP tool.
func RegisterServiceStatus(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "service_status",
		Description: "Get the current status of a managed service. When phase is Ready, also returns the list of environment variable names that will be injected when you call bind_service.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input ServiceStatusInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid service name: %w", err)
		}

		var svc iafv1alpha1.ManagedService
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: namespace}, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("service %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting service: %w", err)
		}

		result := map[string]any{
			"name":    svc.Name,
			"type":    svc.Spec.Type,
			"plan":    string(svc.Spec.Plan),
			"phase":   string(svc.Status.Phase),
			"message": svc.Status.Message,
		}
		if svc.Status.Phase == iafv1alpha1.ManagedServicePhaseReady {
			result["connectionEnvVars"] = serviceEnvVarNames
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// --- bind_service ---

type BindServiceInput struct {
	SessionID   string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	ServiceName string `json:"service_name" jsonschema:"required - name of the managed service"`
	AppName     string `json:"app_name" jsonschema:"required - name of the application to bind to"`
}

// RegisterBindService registers the bind_service MCP tool.
func RegisterBindService(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "bind_service",
		Description: "Bind a ready managed service to an application. Injects connection credentials as Kubernetes Secret references into the application's environment variables (DATABASE_URL, PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD). The service must be in Ready phase.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input BindServiceInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.ServiceName); err != nil {
			return nil, nil, fmt.Errorf("invalid service name: %w", err)
		}
		if err := validation.ValidateAppName(input.AppName); err != nil {
			return nil, nil, fmt.Errorf("invalid app name: %w", err)
		}

		// Fetch and validate the service.
		var svc iafv1alpha1.ManagedService
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.ServiceName, Namespace: namespace}, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("service %q not found", input.ServiceName)
			}
			return nil, nil, fmt.Errorf("getting service: %w", err)
		}
		if svc.Status.Phase != iafv1alpha1.ManagedServicePhaseReady {
			return nil, nil, fmt.Errorf("service %q is not ready (phase: %s) — poll service_status until phase is Ready", input.ServiceName, svc.Status.Phase)
		}

		// Validate the secret name matches the expected CNPG convention.
		expectedSecret := input.ServiceName + "-app"
		if svc.Status.ConnectionSecretRef != expectedSecret {
			return nil, nil, fmt.Errorf("service %q has unexpected connection secret %q (expected %q) — this is a platform error", input.ServiceName, svc.Status.ConnectionSecretRef, expectedSecret)
		}
		secretName := expectedSecret

		// Fetch and validate the application.
		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.AppName, Namespace: namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.AppName)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		// Check for duplicate binding.
		for _, bms := range app.Spec.BoundManagedServices {
			if bms.ServiceName == input.ServiceName {
				return nil, nil, fmt.Errorf("service %q is already bound to application %q", input.ServiceName, input.AppName)
			}
		}

		// Record the binding; the controller injects PG* env vars from the Secret.
		app.Spec.BoundManagedServices = append(app.Spec.BoundManagedServices, iafv1alpha1.BoundManagedService{
			ServiceName: input.ServiceName,
			SecretName:  secretName,
		})
		if err := deps.Client.Update(ctx, &app); err != nil {
			return nil, nil, fmt.Errorf("updating application bindings: %w", err)
		}

		// Update ManagedService.Status.BoundApps with optimistic retry on conflict.
		if err := addBoundApp(ctx, deps.Client, namespace, input.ServiceName, input.AppName); err != nil {
			return nil, nil, err
		}

		result := map[string]any{
			"bound":            true,
			"injectedEnvVars":  serviceEnvVarNames,
			"message": fmt.Sprintf("Application %q is now bound to service %q. Credentials are injected as K8s Secret references — actual values are never returned by tools.", input.AppName, input.ServiceName),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// --- unbind_service ---

type UnbindServiceInput struct {
	SessionID   string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	ServiceName string `json:"service_name" jsonschema:"required - name of the managed service"`
	AppName     string `json:"app_name" jsonschema:"required - name of the application to unbind"`
}

// RegisterUnbindService registers the unbind_service MCP tool.
func RegisterUnbindService(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "unbind_service",
		Description: "Remove the binding between a managed service and an application. Removes the injected environment variables from the application. Does not delete the service or its credentials.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input UnbindServiceInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.ServiceName); err != nil {
			return nil, nil, fmt.Errorf("invalid service name: %w", err)
		}
		if err := validation.ValidateAppName(input.AppName); err != nil {
			return nil, nil, fmt.Errorf("invalid app name: %w", err)
		}

		var svc iafv1alpha1.ManagedService
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.ServiceName, Namespace: namespace}, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("service %q not found", input.ServiceName)
			}
			return nil, nil, fmt.Errorf("getting service: %w", err)
		}

		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.AppName, Namespace: namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.AppName)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		// Remove this service from BoundManagedServices.
		filtered := make([]iafv1alpha1.BoundManagedService, 0, len(app.Spec.BoundManagedServices))
		for _, bms := range app.Spec.BoundManagedServices {
			if bms.ServiceName != input.ServiceName {
				filtered = append(filtered, bms)
			}
		}
		app.Spec.BoundManagedServices = filtered
		if err := deps.Client.Update(ctx, &app); err != nil {
			return nil, nil, fmt.Errorf("updating application bindings: %w", err)
		}

		// Remove app from BoundApps with optimistic retry.
		if err := removeBoundApp(ctx, deps.Client, namespace, input.ServiceName, input.AppName); err != nil {
			return nil, nil, err
		}

		result := map[string]any{
			"unbound": true,
			"message": fmt.Sprintf("Application %q has been unbound from service %q. Removed injected environment variables.", input.AppName, input.ServiceName),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// --- deprovision_service ---

type DeprovisionServiceInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - service name to deprovision"`
}

// RegisterDeprovisionService registers the deprovision_service MCP tool.
func RegisterDeprovisionService(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "deprovision_service",
		Description: "Delete a managed service and all its data. The service must have no bound applications (use unbind_service first). This action is irreversible.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input DeprovisionServiceInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid service name: %w", err)
		}

		var svc iafv1alpha1.ManagedService
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: namespace}, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("service %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting service: %w", err)
		}

		// UX guard: check bound apps. The controller finalizer is the security boundary.
		if len(svc.Status.BoundApps) > 0 {
			return nil, nil, fmt.Errorf("service %q is still bound to applications %v — use unbind_service to remove all bindings before deprovisioning", input.Name, svc.Status.BoundApps)
		}

		if err := deps.Client.Delete(ctx, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("service %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("deprovisioning service: %w", err)
		}

		result := map[string]any{
			"name":    input.Name,
			"status":  "deprovisioning",
			"message": fmt.Sprintf("Service %q is being deprovisioned. All data will be permanently deleted.", input.Name),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// --- list_services ---

type ListServicesInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
}

// RegisterListServices registers the list_services MCP tool.
func RegisterListServices(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "list_services",
		Description: "List all managed services in the current session's namespace.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input ListServicesInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}

		var list iafv1alpha1.ManagedServiceList
		if err := deps.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
			return nil, nil, fmt.Errorf("listing services: %w", err)
		}

		items := make([]map[string]any, 0, len(list.Items))
		for _, svc := range list.Items {
			items = append(items, map[string]any{
				"name":      svc.Name,
				"type":      svc.Spec.Type,
				"plan":      string(svc.Spec.Plan),
				"phase":     string(svc.Status.Phase),
				"boundApps": svc.Status.BoundApps,
			})
		}
		result := map[string]any{
			"services": items,
			"total":    len(items),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// --- helpers ---

// addBoundApp adds appName to svc.Status.BoundApps with optimistic-concurrency retry on conflict.
func addBoundApp(ctx context.Context, c client.Client, namespace, svcName, appName string) error {
	for attempt := 0; attempt < 3; attempt++ {
		var svc iafv1alpha1.ManagedService
		if err := c.Get(ctx, types.NamespacedName{Name: svcName, Namespace: namespace}, &svc); err != nil {
			return fmt.Errorf("getting service for bound-apps update: %w", err)
		}
		for _, name := range svc.Status.BoundApps {
			if name == appName {
				return nil // already in the list
			}
		}
		svc.Status.BoundApps = append(svc.Status.BoundApps, appName)
		err := c.Status().Update(ctx, &svc)
		if err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			return fmt.Errorf("updating service bound apps: %w", err)
		}
	}
	return fmt.Errorf("failed to update service bound apps after retries")
}

// removeBoundApp removes appName from svc.Status.BoundApps with optimistic-concurrency retry.
func removeBoundApp(ctx context.Context, c client.Client, namespace, svcName, appName string) error {
	for attempt := 0; attempt < 3; attempt++ {
		var svc iafv1alpha1.ManagedService
		if err := c.Get(ctx, types.NamespacedName{Name: svcName, Namespace: namespace}, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil // service already gone, nothing to do
			}
			return fmt.Errorf("getting service for bound-apps update: %w", err)
		}
		filtered := make([]string, 0, len(svc.Status.BoundApps))
		for _, name := range svc.Status.BoundApps {
			if name != appName {
				filtered = append(filtered, name)
			}
		}
		svc.Status.BoundApps = filtered
		err := c.Status().Update(ctx, &svc)
		if err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			return fmt.Errorf("updating service bound apps: %w", err)
		}
	}
	return fmt.Errorf("failed to update service bound apps after retries")
}

