package api

import (
	"github.com/dlapiduz/iaf/internal/api/handlers"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"github.com/labstack/echo/v4"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterRoutes registers all API routes on the Echo server.
func RegisterRoutes(e *echo.Echo, c client.Client, cs kubernetes.Interface, sessions *auth.SessionStore, store *sourcestore.Store) {
	health := handlers.NewHealthHandler()
	e.GET("/health", health.Health)
	e.GET("/ready", health.Ready)

	apps := handlers.NewApplicationHandler(c, sessions, store)
	api := e.Group("/api/v1")
	api.GET("/applications", apps.List)
	api.POST("/applications", apps.Create)
	api.GET("/applications/:name", apps.Get)
	api.PUT("/applications/:name", apps.Update)
	api.DELETE("/applications/:name", apps.Delete)
	api.POST("/applications/:name/source", apps.UploadSource)

	logs := handlers.NewLogsHandler(c, cs, sessions)
	api.GET("/applications/:name/logs", logs.GetLogs)
	api.GET("/applications/:name/build", logs.GetBuildLogs)
}
