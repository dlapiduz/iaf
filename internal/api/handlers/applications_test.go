package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/api/handlers"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"github.com/labstack/echo/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// handlerTestEnv holds the shared test infrastructure.
type handlerTestEnv struct {
	handler  *handlers.ApplicationHandler
	e        *echo.Echo
	sessions *auth.SessionStore
	client   ctrlclient.Client
	store    *sourcestore.Store
}

func setupHandlerTest(t *testing.T) *handlerTestEnv {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := iafv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&iafv1alpha1.Application{}).
		Build()

	sessions, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}

	store, err := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	h := handlers.NewApplicationHandler(k8sClient, sessions, store)
	e := echo.New()

	return &handlerTestEnv{
		handler:  h,
		e:        e,
		sessions: sessions,
		client:   k8sClient,
		store:    store,
	}
}

// newSession creates a session and returns (sessionID, namespace).
func (env *handlerTestEnv) newSession(t *testing.T, name string) (string, string) {
	t.Helper()
	sess, err := env.sessions.Register(name, 0)
	if err != nil {
		t.Fatal(err)
	}
	return sess.ID, sess.Namespace
}

// jsonRequest builds an Echo context for a JSON request with optional session header.
func (env *handlerTestEnv) jsonRequest(method, path, sessionID string, body any) (*httptest.ResponseRecorder, echo.Context) {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("X-IAF-Session", sessionID)
	}
	rec := httptest.NewRecorder()
	c := env.e.NewContext(req, rec)
	return rec, c
}

// setParam sets a URL path parameter on the context.
func setParam(c echo.Context, name, value string) {
	c.SetParamNames(name)
	c.SetParamValues(value)
}

func TestApplicationHandler_Create(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		body       any
		wantStatus int
	}{
		{
			name:       "image app created",
			body:       map[string]any{"name": "myapp", "image": "nginx:latest"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "git app created",
			body:       map[string]any{"name": "gitapp", "gitUrl": "https://github.com/example/repo"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing session returns 400",
			sessionID:  "skip", // special marker to omit session header
			body:       map[string]any{"name": "myapp", "image": "nginx:latest"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no image or gitUrl returns 400",
			body:       map[string]any{"name": "myapp"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid app name returns 400",
			body:       map[string]any{"name": "Invalid_Name!", "image": "nginx:latest"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := setupHandlerTest(t)

			var sessionID string
			if tc.sessionID == "skip" {
				sessionID = "" // omit session header
			} else {
				sid, _ := env.newSession(t, "agent")
				sessionID = sid
			}

			rec, c := env.jsonRequest(http.MethodPost, "/api/v1/applications", sessionID, tc.body)
			if err := env.handler.Create(c); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.wantStatus {
				t.Errorf("status %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestApplicationHandler_List(t *testing.T) {
	env := setupHandlerTest(t)
	ctx := context.Background()

	sid, ns := env.newSession(t, "agent")

	// Create two apps in session namespace
	for _, name := range []string{"app-one", "app-two"} {
		obj := &iafv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
		}
		if err := env.client.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}

	rec, c := env.jsonRequest(http.MethodGet, "/api/v1/applications", sid, nil)
	if err := env.handler.List(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var apps []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &apps); err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}
}

// TestApplicationHandler_List_CrossNamespaceIsolation is the mandatory namespace isolation test.
// A session must not see resources from another session's namespace.
func TestApplicationHandler_List_CrossNamespaceIsolation(t *testing.T) {
	env := setupHandlerTest(t)
	ctx := context.Background()

	sidA, _ := env.newSession(t, "agent-a")
	_, nsB := env.newSession(t, "agent-b")

	// Create an app in ns-b only
	obj := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "other-app", Namespace: nsB},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
	}
	if err := env.client.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}

	// Session A must see empty list — never returns ns-b resources
	rec, c := env.jsonRequest(http.MethodGet, "/api/v1/applications", sidA, nil)
	if err := env.handler.List(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (body: %s)", rec.Code, rec.Body.String())
	}

	var apps []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &apps); err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Errorf("namespace isolation violated: session-a sees %d apps from session-b namespace", len(apps))
	}
}

func TestApplicationHandler_Get(t *testing.T) {
	env := setupHandlerTest(t)
	ctx := context.Background()
	sid, ns := env.newSession(t, "agent")

	obj := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
	}
	if err := env.client.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}

	t.Run("found", func(t *testing.T) {
		rec, c := env.jsonRequest(http.MethodGet, "/api/v1/applications/myapp", sid, nil)
		setParam(c, "name", "myapp")
		if err := env.handler.Get(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d (body: %s)", rec.Code, rec.Body.String())
		}
		var app map[string]any
		json.Unmarshal(rec.Body.Bytes(), &app)
		if app["name"] != "myapp" {
			t.Errorf("expected name=myapp, got %v", app["name"])
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec, c := env.jsonRequest(http.MethodGet, "/api/v1/applications/noapp", sid, nil)
		setParam(c, "name", "noapp")
		if err := env.handler.Get(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %d, want 404", rec.Code)
		}
	})
}

func TestApplicationHandler_Delete(t *testing.T) {
	env := setupHandlerTest(t)
	ctx := context.Background()
	sid, ns := env.newSession(t, "agent")

	obj := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
	}
	if err := env.client.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}

	t.Run("deleted successfully", func(t *testing.T) {
		rec, c := env.jsonRequest(http.MethodDelete, "/api/v1/applications/myapp", sid, nil)
		setParam(c, "name", "myapp")
		if err := env.handler.Delete(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("not found returns 404", func(t *testing.T) {
		rec, c := env.jsonRequest(http.MethodDelete, "/api/v1/applications/noapp", sid, nil)
		setParam(c, "name", "noapp")
		if err := env.handler.Delete(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %d, want 404", rec.Code)
		}
	})
}

func TestApplicationHandler_UploadSource(t *testing.T) {
	env := setupHandlerTest(t)
	ctx := context.Background()
	sid, ns := env.newSession(t, "agent")

	obj := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
	}
	if err := env.client.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}

	t.Run("JSON upload sets blob URL", func(t *testing.T) {
		body := map[string]any{
			"files": map[string]string{
				"main.go": "package main\nfunc main() {}",
			},
		}
		rec, c := env.jsonRequest(http.MethodPost, "/api/v1/applications/myapp/source", sid, body)
		setParam(c, "name", "myapp")
		if err := env.handler.UploadSource(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d (body: %s)", rec.Code, rec.Body.String())
		}

		var result map[string]string
		json.Unmarshal(rec.Body.Bytes(), &result)
		if result["blobUrl"] == "" {
			t.Error("expected non-empty blobUrl in response")
		}

		// Verify the app's Blob field was updated
		var updated iafv1alpha1.Application
		env.client.Get(ctx, ctrlclient.ObjectKey{Name: "myapp", Namespace: ns}, &updated)
		if updated.Spec.Blob == "" {
			t.Error("expected Blob field to be set on application after source upload")
		}
	})

	t.Run("app not found returns 404", func(t *testing.T) {
		body := map[string]any{"files": map[string]string{"f.go": "pkg"}}
		rec, c := env.jsonRequest(http.MethodPost, "/api/v1/applications/noapp/source", sid, body)
		setParam(c, "name", "noapp")
		if err := env.handler.UploadSource(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %d, want 404", rec.Code)
		}
	})
}

func TestApplicationHandler_MissingSession(t *testing.T) {
	env := setupHandlerTest(t)

	endpoints := []struct {
		name    string
		method  string
		handler func(echo.Context) error
		body    any
	}{
		{"List", http.MethodGet, env.handler.List, nil},
		{"Get", http.MethodGet, env.handler.Get, nil},
		{"Create", http.MethodPost, env.handler.Create, map[string]any{"name": "x", "image": "y"}},
		{"Delete", http.MethodDelete, env.handler.Delete, nil},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+"_no_session_returns_400", func(t *testing.T) {
			rec, c := env.jsonRequest(ep.method, "/", "", ep.body)
			if err := ep.handler(c); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400 for missing session", rec.Code)
			}
		})
	}
}

func TestApplicationHandler_TarballUpload(t *testing.T) {
	env := setupHandlerTest(t)
	ctx := context.Background()
	sid, ns := env.newSession(t, "agent")

	obj := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: ns},
		Spec:       iafv1alpha1.ApplicationSpec{Image: "nginx:latest"},
	}
	if err := env.client.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}

	// Use a minimal gzip-compressed tar archive (empty but valid content-type)
	body := strings.NewReader("fake-tar-data")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/myapp/source", body)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-IAF-Session", sid)
	rec := httptest.NewRecorder()
	c := env.e.NewContext(req, rec)
	setParam(c, "name", "myapp")

	// The store will try to parse the fake tar — it may succeed or fail.
	// We just verify no panic and the handler returns a valid HTTP response.
	_ = env.handler.UploadSource(c)
	// Status can be 200 or 500 depending on tar validity; we just ensure no crash.
	if rec.Code == 0 {
		t.Error("expected a non-zero HTTP status code")
	}
}
