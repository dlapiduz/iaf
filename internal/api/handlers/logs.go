package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/labstack/echo/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LogsHandler struct {
	client    client.Client
	clientset kubernetes.Interface
	namespace string
}

func NewLogsHandler(c client.Client, cs kubernetes.Interface, namespace string) *LogsHandler {
	return &LogsHandler{
		client:    c,
		clientset: cs,
		namespace: namespace,
	}
}

// GetLogs returns logs for an application's pods.
func (h *LogsHandler) GetLogs(c echo.Context) error {
	name := c.Param("name")
	lines := int64(100)
	if l := c.QueryParam("lines"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil {
			lines = parsed
		}
	}

	// Verify application exists
	var app iafv1alpha1.Application
	if err := h.client.Get(c.Request().Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Get pods for the application
	podList := &corev1.PodList{}
	if err := h.client.List(c.Request().Context(), podList,
		client.InNamespace(h.namespace),
		client.MatchingLabels{"iaf.io/application": name},
	); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if len(podList.Items) == 0 {
		return c.JSON(http.StatusOK, map[string]any{
			"logs": "",
			"pods": 0,
		})
	}

	// Get logs from the first pod
	pod := podList.Items[0]
	logs, err := h.getPodLogs(c.Request().Context(), pod.Name, "app", lines)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"logs":    logs,
		"pods":    len(podList.Items),
		"podName": pod.Name,
	})
}

// GetBuildLogs returns kpack build logs for an application.
func (h *LogsHandler) GetBuildLogs(c echo.Context) error {
	name := c.Param("name")

	// Verify application exists
	var app iafv1alpha1.Application
	if err := h.client.Get(c.Request().Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "application not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Look for kpack build pods
	podList := &corev1.PodList{}
	if err := h.client.List(c.Request().Context(), podList,
		client.InNamespace(h.namespace),
		client.MatchingLabels{"image.kpack.io/image": name},
	); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if len(podList.Items) == 0 {
		return c.JSON(http.StatusOK, map[string]any{
			"buildLogs":   "",
			"buildStatus": app.Status.BuildStatus,
		})
	}

	// Get logs from the most recent build pod
	pod := podList.Items[len(podList.Items)-1]
	var allLogs string
	for _, container := range pod.Spec.InitContainers {
		logs, err := h.getPodLogs(c.Request().Context(), pod.Name, container.Name, 200)
		if err != nil {
			continue
		}
		allLogs += fmt.Sprintf("=== %s ===\n%s\n", container.Name, logs)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"buildLogs":   allLogs,
		"buildStatus": app.Status.BuildStatus,
		"podName":     pod.Name,
	})
}

func (h *LogsHandler) getPodLogs(ctx context.Context, podName, container string, lines int64) (string, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		TailLines: &lines,
	}
	req := h.clientset.CoreV1().Pods(h.namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("opening log stream: %w", err)
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("reading logs: %w", err)
	}
	return string(data), nil
}
