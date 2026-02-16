package api

import (
	"log/slog"

	"github.com/dlapiduz/iaf/internal/middleware"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
)

// NewServer creates a new Echo server with middleware configured.
func NewServer(apiKey string, logger *slog.Logger) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(echomiddleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
	}))
	e.Use(middleware.Auth(apiKey))
	e.Use(middleware.Audit(logger))

	return e
}
