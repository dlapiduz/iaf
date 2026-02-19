package tools

import (
	"context"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Dependencies holds shared dependencies for MCP tools.
type Dependencies struct {
	Client     client.Client
	Store      *sourcestore.Store
	BaseDomain string
	Sessions   *auth.SessionStore
}

// ResolveNamespace looks up the session and returns its namespace.
func (d *Dependencies) ResolveNamespace(sessionID string) (string, error) {
	sess, ok := d.Sessions.Lookup(sessionID)
	if !ok {
		return "", fmt.Errorf("session not found, call the register tool first")
	}
	return sess.Namespace, nil
}

// CheckAppNameAvailable verifies that no application with the given name exists
// in any other namespace. This prevents hostname collisions since all apps
// share the same base domain regardless of namespace.
func (d *Dependencies) CheckAppNameAvailable(ctx context.Context, appName, currentNamespace string) error {
	var allApps iafv1alpha1.ApplicationList
	if err := d.Client.List(ctx, &allApps); err != nil {
		return fmt.Errorf("checking application name availability: %w", err)
	}
	for _, app := range allApps.Items {
		if app.Name == appName && app.Namespace != currentNamespace {
			return fmt.Errorf("application name %q is already in use in namespace %q — choose a different name", appName, app.Namespace)
		}
	}
	return nil
}
