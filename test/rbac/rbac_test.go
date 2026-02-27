// Package rbac contains tests that validate the RBAC ClusterRole manifest
// defines every permission the iaf-platform service account needs. These tests
// run without a cluster — they parse config/rbac/role.yaml and check the rules.
//
// This is a "permission contract" test: if a required verb/resource pair is
// ever dropped from the role (e.g., because controller-gen regenerates the file
// after a marker is removed), the test fails immediately, before any deployment.
package rbac

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// permCheck describes a single required permission.
type permCheck struct {
	Group    string
	Resource string
	Verb     string
}

// required is the minimal permission set the iaf-platform ClusterRole must have.
// Each entry maps to a concrete operation the platform performs:
//
//   - namespaces create/get       — register tool: EnsureNamespace
//   - pods get/list               — app_logs tool: list build and runtime pods
//   - pods/log get                — app_logs tool: stream log content
//   - secrets create/get/list/delete — copy data-source credentials into session namespaces
//   - serviceaccounts create/...  — EnsureNamespace: create iaf-kpack-sa
//   - services create/...         — controller: reconcileService
//   - deployments create/...      — controller: reconcileDeployment
//   - kpack.io images create/...  — controller: build via kpack
//   - traefik.io ingressroutes    — controller: reconcileIngressRoute
var required = []permCheck{
	// Session provisioning
	{Group: "", Resource: "namespaces", Verb: "create"},
	{Group: "", Resource: "namespaces", Verb: "get"},
	// Pod log access for app_logs tool
	{Group: "", Resource: "pods", Verb: "get"},
	{Group: "", Resource: "pods", Verb: "list"},
	{Group: "", Resource: "pods/log", Verb: "get"},
	// Credential management
	{Group: "", Resource: "secrets", Verb: "create"},
	{Group: "", Resource: "secrets", Verb: "get"},
	{Group: "", Resource: "secrets", Verb: "list"},
	{Group: "", Resource: "secrets", Verb: "delete"},
	// kpack service account per session
	{Group: "", Resource: "serviceaccounts", Verb: "create"},
	{Group: "", Resource: "serviceaccounts", Verb: "get"},
	// Networking
	{Group: "", Resource: "services", Verb: "create"},
	{Group: "", Resource: "services", Verb: "get"},
	{Group: "", Resource: "services", Verb: "delete"},
	// Workloads
	{Group: "apps", Resource: "deployments", Verb: "create"},
	{Group: "apps", Resource: "deployments", Verb: "get"},
	{Group: "apps", Resource: "deployments", Verb: "delete"},
	// IAF CRDs
	{Group: "iaf.io", Resource: "applications", Verb: "create"},
	{Group: "iaf.io", Resource: "applications", Verb: "get"},
	{Group: "iaf.io", Resource: "applications", Verb: "list"},
	{Group: "iaf.io", Resource: "applications", Verb: "update"},
	{Group: "iaf.io", Resource: "applications", Verb: "delete"},
	// kpack builds
	{Group: "kpack.io", Resource: "images", Verb: "create"},
	{Group: "kpack.io", Resource: "images", Verb: "get"},
	{Group: "kpack.io", Resource: "images", Verb: "delete"},
	// Ingress
	{Group: "traefik.io", Resource: "ingressroutes", Verb: "create"},
	{Group: "traefik.io", Resource: "ingressroutes", Verb: "get"},
	{Group: "traefik.io", Resource: "ingressroutes", Verb: "delete"},
}

// TestClusterRoleHasRequiredPermissions parses config/rbac/role.yaml and
// verifies every entry in required is covered by a rule in the ClusterRole.
//
// This catches two classes of failures:
//  1. A +kubebuilder:rbac marker was removed from the controller, causing
//     controller-gen to drop a permission from the regenerated file.
//  2. A new platform operation was added but the marker was not added.
func TestClusterRoleHasRequiredPermissions(t *testing.T) {
	role := loadClusterRole(t)

	// Build a fast-lookup map: "group/resource/verb" → true.
	granted := make(map[string]bool)
	for _, rule := range role.Rules {
		for _, group := range rule.APIGroups {
			for _, resource := range rule.Resources {
				for _, verb := range rule.Verbs {
					granted[key(group, resource, verb)] = true
					if verb == "*" {
						// wildcard — grant all standard verbs
						for _, v := range []string{"get", "list", "watch", "create", "update", "patch", "delete"} {
							granted[key(group, resource, v)] = true
						}
					}
				}
			}
		}
	}

	for _, p := range required {
		if !granted[key(p.Group, p.Resource, p.Verb)] {
			t.Errorf("MISSING permission: group=%q resource=%q verb=%q — "+
				"add a +kubebuilder:rbac marker in internal/controller/application_controller.go "+
				"and run make generate", p.Group, p.Resource, p.Verb)
		}
	}
}

func key(group, resource, verb string) string {
	return group + "/" + resource + "/" + verb
}

func loadClusterRole(t *testing.T) rbacv1.ClusterRole {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	rolePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "config", "rbac", "role.yaml")

	data, err := os.ReadFile(rolePath)
	if err != nil {
		t.Fatalf("reading %s: %v — run 'make generate' first", rolePath, err)
	}

	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(data, &role); err != nil {
		t.Fatalf("parsing %s: %v", rolePath, err)
	}
	if role.Kind != "ClusterRole" {
		t.Fatalf("expected Kind=ClusterRole, got %q", role.Kind)
	}
	return role
}
