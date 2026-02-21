package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	iafvalidation "github.com/dlapiduz/iaf/internal/validation"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// rejectedSecretTypes are Secret types that must never be copied into session namespaces.
var rejectedSecretTypes = map[corev1.SecretType]bool{
	corev1.SecretTypeServiceAccountToken: true,
	corev1.SecretTypeDockerConfigJson:    true,
	corev1.SecretTypeDockercfg:           true,
}

// dsSecretName returns the name to use for the copied credential Secret in the session namespace.
// Prefixed with "iaf-ds-" and truncated to 63 characters (Kubernetes DNS label limit).
func dsSecretName(dataSourceName string) string {
	name := "iaf-ds-" + dataSourceName
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// ---- list_data_sources -------------------------------------------------------

type ListDataSourcesInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Kind      string `json:"kind,omitempty" jsonschema:"filter by data source kind (e.g. postgres, mysql, s3)"`
	Tags      string `json:"tags,omitempty" jsonschema:"comma-separated list of tags to filter by (all must match)"`
}

// RegisterListDataSources registers the list_data_sources MCP tool.
func RegisterListDataSources(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "list_data_sources",
		Description: "List all data sources registered on the platform. Returns metadata only — no credentials are returned. Optionally filter by kind or tags. Use get_data_source for details on a specific source, and attach_data_source to make one available to your app.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input ListDataSourcesInput) (*gomcp.CallToolResult, any, error) {
		if _, err := deps.ResolveNamespace(input.SessionID); err != nil {
			return nil, nil, err
		}

		var list iafv1alpha1.DataSourceList
		if err := deps.Client.List(ctx, &list); err != nil {
			return nil, nil, fmt.Errorf("listing data sources: %w", err)
		}

		// Parse tag filter.
		var filterTags []string
		if input.Tags != "" {
			for _, t := range strings.Split(input.Tags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					filterTags = append(filterTags, t)
				}
			}
		}

		var entries []map[string]any
		for _, ds := range list.Items {
			if input.Kind != "" && ds.Spec.Kind != input.Kind {
				continue
			}
			if len(filterTags) > 0 && !hasAllTags(ds.Spec.Tags, filterTags) {
				continue
			}

			envVarNames := envVarNamesFromMapping(ds.Spec.EnvVarMapping)
			entry := map[string]any{
				"name":        ds.Name,
				"kind":        ds.Spec.Kind,
				"envVarNames": envVarNames,
			}
			if ds.Spec.Description != "" {
				entry["description"] = ds.Spec.Description
			}
			if len(ds.Spec.Tags) > 0 {
				entry["tags"] = ds.Spec.Tags
			}
			entries = append(entries, entry)
		}
		if entries == nil {
			entries = []map[string]any{}
		}

		text, _ := json.MarshalIndent(map[string]any{
			"dataSources": entries,
			"total":       len(entries),
		}, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// ---- get_data_source --------------------------------------------------------

type GetDataSourceInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - name of the data source"`
}

// RegisterGetDataSource registers the get_data_source MCP tool.
func RegisterGetDataSource(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "get_data_source",
		Description: "Get details about a specific data source: kind, description, schema, tags, and the environment variable names that will be injected into your app. Credential values are never returned.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input GetDataSourceInput) (*gomcp.CallToolResult, any, error) {
		if _, err := deps.ResolveNamespace(input.SessionID); err != nil {
			return nil, nil, err
		}
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		var ds iafv1alpha1.DataSource
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name}, &ds); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("data source %q not found; use list_data_sources to see available sources", input.Name)
			}
			return nil, nil, fmt.Errorf("getting data source: %w", err)
		}

		envVarNames := envVarNamesFromMapping(ds.Spec.EnvVarMapping)
		result := map[string]any{
			"name":        ds.Name,
			"kind":        ds.Spec.Kind,
			"envVarNames": envVarNames,
		}
		if ds.Spec.Description != "" {
			result["description"] = ds.Spec.Description
		}
		if ds.Spec.Schema != "" {
			result["schema"] = ds.Spec.Schema
		}
		if len(ds.Spec.Tags) > 0 {
			result["tags"] = ds.Spec.Tags
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// ---- attach_data_source -----------------------------------------------------

type AttachDataSourceInput struct {
	SessionID      string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	AppName        string `json:"app_name" jsonschema:"required - name of the application to attach the data source to"`
	DataSourceName string `json:"datasource_name" jsonschema:"required - name of the data source to attach"`
}

// RegisterAttachDataSource registers the attach_data_source MCP tool.
func RegisterAttachDataSource(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "attach_data_source",
		Description: "Attach a data source to an application. The platform copies the data source credentials into your namespace and injects them as environment variables into the app container. The app will restart to pick up the new variables. Credential values are never returned.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input AttachDataSourceInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}

		if err := iafvalidation.ValidateAppName(input.AppName); err != nil {
			return nil, nil, fmt.Errorf("invalid app_name: %w", err)
		}
		if input.DataSourceName == "" {
			return nil, nil, fmt.Errorf("datasource_name is required")
		}

		// Get the Application CR.
		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.AppName, Namespace: namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found in your session namespace; use list_apps to see your apps", input.AppName)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		// Idempotency: if already attached, return success.
		for _, ads := range app.Spec.AttachedDataSources {
			if ads.DataSourceName == input.DataSourceName {
				envVarNames := []string{}
				// Re-fetch DataSource to return accurate env var names.
				var ds iafv1alpha1.DataSource
				if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.DataSourceName}, &ds); err == nil {
					envVarNames = envVarNamesFromMapping(ds.Spec.EnvVarMapping)
				}
				result := map[string]any{
					"datasource":  input.DataSourceName,
					"app":         input.AppName,
					"envVarNames": envVarNames,
					"message":     fmt.Sprintf("Data source %q is already attached to app %q.", input.DataSourceName, input.AppName),
					"alreadyAttached": true,
				}
				text, _ := json.MarshalIndent(result, "", "  ")
				return &gomcp.CallToolResult{
					Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
				}, nil, nil
			}
		}

		// Get DataSource CR (cluster-scoped, no namespace).
		var ds iafv1alpha1.DataSource
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.DataSourceName}, &ds); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("data source %q not found; use list_data_sources to see available sources", input.DataSourceName)
			}
			return nil, nil, fmt.Errorf("getting data source: %w", err)
		}

		// Check env var collisions.
		if err := checkEnvVarCollisions(ctx, deps.Client, &app, &ds); err != nil {
			return nil, nil, err
		}

		// Get source Secret from the operator-specified namespace.
		var srcSecret corev1.Secret
		if err := deps.Client.Get(ctx, types.NamespacedName{
			Name:      ds.Spec.SecretRef.Name,
			Namespace: ds.Spec.SecretRef.Namespace,
		}, &srcSecret); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("data source %q credential secret not found — contact your platform administrator", input.DataSourceName)
			}
			return nil, nil, fmt.Errorf("reading data source credentials: %w", err)
		}

		// Validate source Secret type.
		if rejectedSecretTypes[srcSecret.Type] {
			return nil, nil, fmt.Errorf("data source %q references a secret of type %q which is not allowed for use as a data source credential", input.DataSourceName, srcSecret.Type)
		}

		// Create a copy of the Secret in the session namespace, owned by the Application.
		secretName := dsSecretName(input.DataSourceName)
		copiedSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "iaf",
					"iaf.io/datasource":            input.DataSourceName,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: iafv1alpha1.GroupVersion.String(),
						Kind:       "Application",
						Name:       app.Name,
						UID:        app.UID,
						Controller: boolPtrDS(true),
					},
				},
			},
			Type: srcSecret.Type,
			Data: srcSecret.Data,
		}

		existingCopy := &corev1.Secret{}
		getErr := deps.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, existingCopy)
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return nil, nil, fmt.Errorf("checking for existing credential copy: %w", getErr)
		}
		if apierrors.IsNotFound(getErr) {
			if err := deps.Client.Create(ctx, copiedSecret); err != nil {
				return nil, nil, fmt.Errorf("copying data source credentials: %w", err)
			}
		}
		// If the Secret already exists (e.g., from a previous partial attach), reuse it.

		// Patch the Application CR to record the attachment.
		original := app.DeepCopy()
		app.Spec.AttachedDataSources = append(app.Spec.AttachedDataSources, iafv1alpha1.AttachedDataSource{
			DataSourceName: input.DataSourceName,
			SecretName:     secretName,
		})
		if err := deps.Client.Patch(ctx, &app, client.MergeFrom(original)); err != nil {
			// Clean up the copied Secret on patch failure to avoid orphans.
			if apierrors.IsNotFound(getErr) { // only delete if we just created it
				_ = deps.Client.Delete(ctx, copiedSecret)
			}
			return nil, nil, fmt.Errorf("attaching data source to application: %w", err)
		}

		// Audit log: every attachment is logged.
		slog.Info("data source attached",
			"session", input.SessionID,
			"datasource", input.DataSourceName,
			"app", input.AppName,
			"namespace", namespace,
		)

		envVarNames := envVarNamesFromMapping(ds.Spec.EnvVarMapping)
		result := map[string]any{
			"datasource":  input.DataSourceName,
			"app":         input.AppName,
			"envVarNames": envVarNames,
			"message":     fmt.Sprintf("Data source %q attached to app %q. The app will restart to pick up the new environment variables: %v.", input.DataSourceName, input.AppName, envVarNames),
		}
		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// checkEnvVarCollisions returns an error if any env var in ds.Spec.EnvVarMapping
// collides with env vars in app.Spec.Env or in already-attached data sources.
func checkEnvVarCollisions(ctx context.Context, k8sClient client.Client, app *iafv1alpha1.Application, ds *iafv1alpha1.DataSource) error {
	// Build a map of already-used env var names → source label.
	used := map[string]string{}

	for _, e := range app.Spec.Env {
		used[e.Name] = "app env var"
	}

	for _, ads := range app.Spec.AttachedDataSources {
		var existingDS iafv1alpha1.DataSource
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ads.DataSourceName}, &existingDS); err != nil {
			// If the existing DataSource is gone, skip — no collision possible.
			continue
		}
		for _, envVarName := range existingDS.Spec.EnvVarMapping {
			used[envVarName] = fmt.Sprintf("data source %q", ads.DataSourceName)
		}
	}

	for _, envVarName := range ds.Spec.EnvVarMapping {
		if source, conflict := used[envVarName]; conflict {
			return fmt.Errorf("env var %q is already defined by %s; choose a data source that uses different variable names or detach the conflicting source first", envVarName, source)
		}
	}
	return nil
}

// envVarNamesFromMapping returns a sorted slice of env var names from a mapping.
func envVarNamesFromMapping(mapping map[string]string) []string {
	names := make([]string, 0, len(mapping))
	for _, v := range mapping {
		names = append(names, v)
	}
	return names
}

// hasAllTags returns true if item contains every tag in required.
func hasAllTags(itemTags, required []string) bool {
	tagSet := make(map[string]bool, len(itemTags))
	for _, t := range itemTags {
		tagSet[t] = true
	}
	for _, t := range required {
		if !tagSet[t] {
			return false
		}
	}
	return true
}

// boolPtrDS is a local helper to avoid import cycle with the controller package.
func boolPtrDS(b bool) *bool { return &b }
