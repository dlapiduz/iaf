package tools

import (
	"context"
	"fmt"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Dependencies holds shared dependencies for MCP tools.
type Dependencies struct {
	Client     client.Client
	Store      *sourcestore.Store
	BaseDomain string
	Sessions   *auth.SessionStore
	// GitHub fields — all three must be set for GitHub tools to be registered.
	GitHub      iafgithub.Client
	GitHubToken string // stored but never surfaced in output or logs
	GitHubOrg   string
	// TempoURL is the Grafana base URL used to build traceExploreUrl in
	// app_status responses. Set from IAF_TEMPO_URL. Empty = feature disabled.
	TempoURL string
	// SessionTTL is the idle TTL for new sessions. 0 = sessions never expire.
	SessionTTL time.Duration
}

// ResolveNamespace looks up the session and returns its namespace.
// It also updates the session's LastActivityAt to extend the TTL.
func (d *Dependencies) ResolveNamespace(sessionID string) (string, error) {
	sess, ok := d.Sessions.Lookup(sessionID)
	if !ok {
		return "", fmt.Errorf("session not found, call the register tool first")
	}
	d.Sessions.Touch(sessionID)
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
