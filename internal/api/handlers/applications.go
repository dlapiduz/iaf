package handlers

import (
	"fmt"
	"net/http"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"github.com/dlapiduz/iaf/internal/validation"
	"github.com/labstack/echo/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ApplicationHandler struct {
	client   client.Client
	sessions *auth.SessionStore
	store    *sourcestore.Store
}

func NewApplicationHandler(c client.Client, sessions *auth.SessionStore, store *sourcestore.Store) *ApplicationHandler {
	return &ApplicationHandler{
		client:   c,
		sessions: sessions,
		store:    store,
	}
}

func (h *ApplicationHandler) resolveNamespace(c echo.Context) (string, error) {
	sessionID := c.Request().Header.Get("X-IAF-Session")
	if sessionID == "" {
		sessionID = c.QueryParam("session_id")
	}
	if sessionID == "" {
		return "", fmt.Errorf("missing session ID: provide X-IAF-Session header or session_id query parameter")
	}
	sess, ok := h.sessions.Lookup(sessionID)
	if !ok {
		return "", fmt.Errorf("session not found, call register first")
	}
	return sess.Namespace, nil
}

// ApplicationResponse is the API representation of an Application.
type ApplicationResponse struct {
	Name              string                        `json:"name"`
	Phase             string                        `json:"phase"`
	URL               string                        `json:"url"`
	Image             string                        `json:"image,omitempty"`
	GitURL            string                        `json:"gitUrl,omitempty"`
	GitRevision       string                        `json:"gitRevision,omitempty"`
	Blob              string                        `json:"blob,omitempty"`
	Port              int32                         `json:"port"`
	Replicas          int32                         `json:"replicas"`
	AvailableReplicas int32                         `json:"availableReplicas"`
	LatestImage       string                        `json:"latestImage,omitempty"`
	BuildStatus       string                        `json:"buildStatus,omitempty"`
	Env               []iafv1alpha1.EnvVar          `json:"env,omitempty"`
	Host              string                        `json:"host,omitempty"`
	Conditions        []metav1.Condition            `json:"conditions,omitempty"`
	CreatedAt         string                        `json:"createdAt"`
}

// CreateApplicationRequest is the request body for creating an application.
type CreateApplicationRequest struct {
	Name        string               `json:"name" validate:"required"`
	Image       string               `json:"image,omitempty"`
	GitURL      string               `json:"gitUrl,omitempty"`
	GitRevision string               `json:"gitRevision,omitempty"`
	Port        int32                `json:"port,omitempty"`
	Replicas    int32                `json:"replicas,omitempty"`
	Env         []iafv1alpha1.EnvVar `json:"env,omitempty"`
	Host        string               `json:"host,omitempty"`
}

// UploadSourceRequest is the request body for uploading source files as JSON.
type UploadSourceRequest struct {
	Files map[string]string `json:"files" validate:"required"`
}

func toResponse(app *iafv1alpha1.Application) ApplicationResponse {
	resp := ApplicationResponse{
		Name:              app.Name,
		Phase:             string(app.Status.Phase),
		URL:               app.Status.URL,
		Image:             app.Spec.Image,
		Blob:              app.Spec.Blob,
		Port:              app.Spec.Port,
		Replicas:          app.Spec.Replicas,
		AvailableReplicas: app.Status.AvailableReplicas,
		LatestImage:       app.Status.LatestImage,
		BuildStatus:       app.Status.BuildStatus,
		Env:               app.Spec.Env,
		Host:              app.Spec.Host,
		Conditions:        app.Status.Conditions,
		CreatedAt:         app.CreationTimestamp.Format("2006-01-02T15:04:05Z"),
	}
	if app.Spec.Git != nil {
		resp.GitURL = app.Spec.Git.URL
		resp.GitRevision = app.Spec.Git.Revision
	}
	return resp
}

// List returns all applications.
func (h *ApplicationHandler) List(c echo.Context) error {
	namespace, err := h.resolveNamespace(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	var list iafv1alpha1.ApplicationList
	if err := h.client.List(c.Request().Context(), &list, client.InNamespace(namespace)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	apps := make([]ApplicationResponse, 0, len(list.Items))
	for i := range list.Items {
		apps = append(apps, toResponse(&list.Items[i]))
	}
	return c.JSON(http.StatusOK, apps)
}

// Get returns a single application.
func (h *ApplicationHandler) Get(c echo.Context) error {
	namespace, err := h.resolveNamespace(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	name := c.Param("name")
	var app iafv1alpha1.Application
	if err := h.client.Get(c.Request().Context(), types.NamespacedName{Name: name, Namespace: namespace}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toResponse(&app))
}

// Create creates a new application.
func (h *ApplicationHandler) Create(c echo.Context) error {
	namespace, err := h.resolveNamespace(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	var req CreateApplicationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	if err := validation.ValidateAppName(req.Name); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	for _, e := range req.Env {
		if err := validation.ValidateEnvVarName(e.Name); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
	}
	if req.Image == "" && req.GitURL == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "either image or gitUrl is required"})
	}

	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: namespace,
		},
		Spec: iafv1alpha1.ApplicationSpec{
			Image:    req.Image,
			Port:     req.Port,
			Replicas: req.Replicas,
			Env:      req.Env,
			Host:     req.Host,
		},
	}

	if req.GitURL != "" {
		app.Spec.Git = &iafv1alpha1.GitSource{
			URL:      req.GitURL,
			Revision: req.GitRevision,
		}
	}

	if app.Spec.Port == 0 {
		app.Spec.Port = 8080
	}
	if app.Spec.Replicas == 0 {
		app.Spec.Replicas = 1
	}

	if err := h.client.Create(c.Request().Context(), app); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "application already exists"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, toResponse(app))
}

// Update updates an existing application.
func (h *ApplicationHandler) Update(c echo.Context) error {
	namespace, err := h.resolveNamespace(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	name := c.Param("name")
	if err := validation.ValidateAppName(name); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	var req CreateApplicationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	for _, e := range req.Env {
		if err := validation.ValidateEnvVarName(e.Name); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
	}

	var app iafv1alpha1.Application
	if err := h.client.Get(c.Request().Context(), types.NamespacedName{Name: name, Namespace: namespace}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if req.Image != "" {
		app.Spec.Image = req.Image
		app.Spec.Git = nil
		app.Spec.Blob = ""
	}
	if req.GitURL != "" {
		app.Spec.Git = &iafv1alpha1.GitSource{
			URL:      req.GitURL,
			Revision: req.GitRevision,
		}
		app.Spec.Image = ""
		app.Spec.Blob = ""
	}
	if req.Port > 0 {
		app.Spec.Port = req.Port
	}
	if req.Replicas > 0 {
		app.Spec.Replicas = req.Replicas
	}
	if req.Env != nil {
		app.Spec.Env = req.Env
	}
	if req.Host != "" {
		app.Spec.Host = req.Host
	}

	if err := h.client.Update(c.Request().Context(), &app); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, toResponse(&app))
}

// Delete deletes an application.
func (h *ApplicationHandler) Delete(c echo.Context) error {
	namespace, err := h.resolveNamespace(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	name := c.Param("name")
	app := &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := h.client.Delete(c.Request().Context(), app); err != nil {
		if apierrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Clean up source store
	_ = h.store.Delete(namespace, name)

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("application %s deleted", name)})
}

// UploadSource handles source code upload for an application.
func (h *ApplicationHandler) UploadSource(c echo.Context) error {
	namespace, err := h.resolveNamespace(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	name := c.Param("name")

	// Check if application exists
	var app iafv1alpha1.Application
	if err := h.client.Get(c.Request().Context(), types.NamespacedName{Name: name, Namespace: namespace}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	contentType := c.Request().Header.Get("Content-Type")

	var blobURL string

	if contentType == "application/json" {
		// JSON body with file contents
		var req UploadSourceRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if len(req.Files) == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "files map is required"})
		}
		blobURL, err = h.store.StoreFiles(namespace, name, req.Files)
	} else {
		// Raw tarball upload
		blobURL, err = h.store.StoreTarball(namespace, name, c.Request().Body)
	}

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Update application with blob URL
	app.Spec.Blob = blobURL
	app.Spec.Image = ""
	app.Spec.Git = nil
	if err := h.client.Update(c.Request().Context(), &app); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "source uploaded",
		"blobUrl": blobURL,
	})
}
