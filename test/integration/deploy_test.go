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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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

func apiCall(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling request: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiURL()+path, r)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
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

// TestDeploy_HelloWorld exercises the full deploy flow via the REST API:
// register → push_code → poll until Running → verify URL responds → cleanup.
func TestDeploy_HelloWorld(t *testing.T) {
	// 1. Register a session.
	code, body := apiCall(t, "POST", "/api/v1/sessions", map[string]any{"name": "integration-test"})
	if code != 200 && code != 201 {
		t.Fatalf("register: expected 200/201, got %d body=%v", code, body)
	}
	sessionID, _ := body["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("register: no session_id in response: %v", body)
	}
	t.Logf("session_id: %s", sessionID)

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
		fmt.Fprintln(w, `+"`"+`{"status":"ok"}`+"`"+`)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `+"`"+`{"message":"Hello, World!"}`+"`"+`)
	})
	http.ListenAndServe(":"+port, nil)
}
`,
		"go.mod": "module hello\n\ngo 1.21\n",
	}
	code, body = apiCall(t, "POST", "/api/v1/sessions/"+sessionID+"/applications", map[string]any{
		"name":  "iaf-integration-test",
		"port":  8080,
		"files": files,
	})
	if code != 200 && code != 201 {
		t.Fatalf("push_code: expected 200/201, got %d body=%v", code, body)
	}
	t.Logf("push_code response: %v", body)

	// 3. Poll until Running (up to 5 minutes).
	deadline := time.Now().Add(5 * time.Minute)
	var appURL string
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Second)
		code, statusBody := apiCall(t, "GET",
			"/api/v1/sessions/"+sessionID+"/applications/iaf-integration-test", nil)
		if code != 200 {
			t.Logf("app_status: %d %v (still waiting)", code, statusBody)
			continue
		}
		phase, _ := statusBody["phase"].(string)
		t.Logf("phase=%s buildStatus=%s", phase, statusBody["buildStatus"])
		if phase == "Running" {
			appURL, _ = statusBody["url"].(string)
			break
		}
		if phase == "Failed" {
			t.Fatalf("app entered Failed phase: %v", statusBody)
		}
	}
	if appURL == "" {
		t.Fatal("app did not reach Running within timeout")
	}

	// 4. Verify the app responds.
	resp, err := http.Get(appURL)
	if err != nil {
		t.Fatalf("GET %s: %v", appURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("app responded with %d, expected 200", resp.StatusCode)
	}

	// 5. Cleanup — delete the application.
	code, _ = apiCall(t, "DELETE",
		"/api/v1/sessions/"+sessionID+"/applications/iaf-integration-test", nil)
	if code != 200 && code != 204 {
		t.Logf("delete app returned %d (non-fatal)", code)
	}
}
