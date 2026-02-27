//go:build integration

// Package integration contains end-to-end tests that run against a live
// Kubernetes cluster with IAF deployed. They are gated by the "integration"
// build tag so they never execute during normal `make test` runs.
//
// Prerequisites:
//   - IAF deployed: make deploy-local
//   - Platform accessible: http://iaf.localhost
//   - Valid auth token: IAF_DEV_TOKEN env var (default "iaf-dev-key")
//
// Run with:
//
//	go test ./test/integration/... -v -tags=integration
//	go test ./test/integration/... -v -tags=integration -run TestRBAC
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	iafPlatformSA = "system:serviceaccount:iaf-system:iaf-platform"
	defaultAPIURL = "http://iaf.localhost"
	defaultToken  = "iaf-dev-key"
)

// ----- helpers ---------------------------------------------------------------

func newK8sClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
	if err != nil {
		t.Fatalf("loading kubeconfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("building k8s client: %v", err)
	}
	return cs
}

func apiURL() string {
	if u := os.Getenv("IAF_API_URL"); u != "" {
		return u
	}
	return defaultAPIURL
}

func apiToken() string {
	if t := os.Getenv("IAF_DEV_TOKEN"); t != "" {
		return t
	}
	return defaultToken
}

// authTransport injects a Bearer token into every outgoing request.
type authTransport struct {
	base  http.RoundTripper
	token string
}

func (a *authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(r)
}

// newMCPClient connects to the live IAF MCP endpoint and returns a ClientSession.
func newMCPClient(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	transport := &gomcp.StreamableClientTransport{
		Endpoint: apiURL() + "/mcp",
		HTTPClient: &http.Client{
			Transport: &authTransport{
				base:  http.DefaultTransport,
				token: apiToken(),
			},
		},
		DisableStandaloneSSE: true,
	}
	c := gomcp.NewClient(&gomcp.Implementation{Name: "integration-test", Version: "0.0.1"}, nil)
	cs, err := c.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connecting to MCP server at %s/mcp: %v", apiURL(), err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// callTool invokes an MCP tool and returns the parsed JSON result.
// Returns an error instead of calling t.Fatal so callers can handle failures
// gracefully (e.g. poll loops).
func callTool(cs *gomcp.ClientSession, name string, args map[string]any) (map[string]any, error) {
	res, err := cs.CallTool(context.Background(), &gomcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("tool %q: %w", name, err)
	}
	if res.IsError {
		msg := name + " returned error"
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*gomcp.TextContent); ok {
				msg = tc.Text
			}
		}
		return nil, fmt.Errorf("tool %q error: %s", name, msg)
	}
	var result map[string]any
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*gomcp.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), &result); err != nil {
				return nil, fmt.Errorf("parsing tool %q response: %w", name, err)
			}
		}
	}
	return result, nil
}

// mustCallTool calls callTool and fatals on error.
func mustCallTool(t *testing.T, cs *gomcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := callTool(cs, name, args)
	if err != nil {
		t.Fatalf("mustCallTool %q: %v", name, err)
	}
	return result
}

// checkSAR uses SubjectAccessReview to verify the iaf-platform service account
// has (or doesn't have) a specific permission.
func checkSAR(t *testing.T, cs kubernetes.Interface, verb, resource, group, namespace string) bool {
	t.Helper()
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User: iafPlatformSA,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      verb,
				Resource:  resource,
				Group:     group,
			},
		},
	}
	result, err := cs.AuthorizationV1().SubjectAccessReviews().Create(
		context.Background(), sar, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("SubjectAccessReview %s %s/%s: %v", verb, group, resource, err)
	}
	return result.Status.Allowed
}

// ----- RBAC tests ------------------------------------------------------------

// TestRBAC_PlatformSAPermissions uses SubjectAccessReview to verify the live
// cluster enforces every permission the platform requires.
// This is the integration-time counterpart to test/rbac/rbac_test.go.
func TestRBAC_PlatformSAPermissions(t *testing.T) {
	cs := newK8sClient(t)

	type check struct {
		verb      string
		resource  string
		group     string
		namespace string // "" = cluster-scoped
		reason    string // what platform operation needs this
	}

	checks := []check{
		// Session provisioning (register tool)
		{"create", "namespaces", "", "", "register: EnsureNamespace"},
		{"get", "namespaces", "", "", "register: EnsureNamespace idempotency"},
		// Pod log access (app_logs tool)
		{"list", "pods", "", "iaf-system", "app_logs: list build pods"},
		{"get", "pods", "", "iaf-system", "app_logs: get pod"},
		{"get", "pods/log", "", "iaf-system", "app_logs: stream logs"},
		// Credential secrets
		{"create", "secrets", "", "iaf-system", "data source credential copy"},
		{"get", "secrets", "", "iaf-system", "data source credential read"},
		// kpack builds
		{"create", "images", "kpack.io", "iaf-system", "push_code: create kpack Image"},
		{"get", "images", "kpack.io", "iaf-system", "push_code: read kpack Image status"},
		// Applications
		{"create", "applications", "iaf.io", "iaf-system", "push_code: create Application CR"},
		{"get", "applications", "iaf.io", "iaf-system", "app_status: read Application CR"},
		// Ingress
		{"create", "ingressroutes", "traefik.io", "iaf-system", "controller: reconcileIngressRoute"},
	}

	for _, c := range checks {
		t.Run(fmt.Sprintf("%s_%s/%s", c.verb, c.group, c.resource), func(t *testing.T) {
			allowed := checkSAR(t, cs, c.verb, c.resource, c.group, c.namespace)
			if !allowed {
				t.Errorf("iaf-platform SA is NOT allowed to %s %s/%s (ns=%q) — needed for: %s\n"+
					"Fix: add +kubebuilder:rbac marker in internal/controller/application_controller.go and run make generate && make deploy-local",
					c.verb, c.group, c.resource, c.namespace, c.reason)
			}
		})
	}
}

// ----- Deploy flow test ------------------------------------------------------

// TestDeploy_HelloWorld exercises the full deploy flow via MCP tools:
// register → push_code → poll until Running → verify URL responds → delete_app.
func TestDeploy_HelloWorld(t *testing.T) {
	cs := newMCPClient(t)

	// 1. Register a session.
	regResult := mustCallTool(t, cs, "register", map[string]any{"name": "integration-test"})
	sessionID, _ := regResult["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("register: no session_id in response: %v", regResult)
	}
	t.Logf("session_id: %s", sessionID)

	// Derive a unique app name from the session ID to avoid cross-namespace
	// hostname collisions if a previous test run didn't clean up.
	appName := "hw-" + sessionID[:8]
	t.Logf("app name: %s", appName)

	// 2. Push a minimal Go hello-world app.
	files := map[string]string{
		"main.go": `package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, ` + "`" + `{"status":"ok"}` + "`" + `)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, ` + "`" + `{"message":"Hello, World!"}` + "`" + `)
	})
	http.ListenAndServe(":"+port, nil)
}
`,
		"go.mod": "module hello\n\ngo 1.21\n",
	}
	pushResult := mustCallTool(t, cs, "push_code", map[string]any{
		"session_id": sessionID,
		"name":       appName,
		"port":       8080,
		"files":      files,
	})
	t.Logf("push_code response: %v", pushResult)

	// 3. Poll until Running (up to 5 minutes).
	deadline := time.Now().Add(5 * time.Minute)
	var appURL string
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Second)
		statusResult, err := callTool(cs, "app_status", map[string]any{
			"session_id": sessionID,
			"name":       appName,
		})
		if err != nil {
			t.Logf("app_status error: %v (still waiting)", err)
			continue
		}
		phase, _ := statusResult["phase"].(string)
		t.Logf("phase=%s buildStatus=%s", phase, statusResult["buildStatus"])
		if phase == "Running" {
			appURL, _ = statusResult["url"].(string)
			break
		}
		if phase == "Failed" {
			t.Fatalf("app entered Failed phase: %v", statusResult)
		}
	}
	if appURL == "" {
		t.Fatal("app did not reach Running within timeout")
	}

	// 4. Verify the app responds.
	resp, err := http.Get(appURL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", appURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("app responded with %d, expected 200", resp.StatusCode)
	}

	// 5. Cleanup — delete the application.
	deleteResult, err := callTool(cs, "delete_app", map[string]any{
		"session_id": sessionID,
		"name":       appName,
	})
	if err != nil {
		t.Logf("delete_app error (non-fatal): %v", err)
	} else {
		t.Logf("delete_app response: %v", deleteResult)
	}
}
