package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Auth returns an Echo middleware that validates API key authentication.
func Auth(apiKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth for health, MCP, and source store endpoints
			path := c.Request().URL.Path
			if path == "/health" || path == "/ready" || path == "/mcp" || strings.HasPrefix(path, "/sources/") {
				return next(c)
			}

			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "missing authorization header",
				})
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token == auth {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid authorization format, expected Bearer token",
				})
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API key",
				})
			}

			return next(c)
		}
	}
}
