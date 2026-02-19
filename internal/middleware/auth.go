package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Auth returns an Echo middleware that validates Bearer token authentication
// against a list of valid tokens.
func Auth(tokens []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth for health and source store endpoints
			path := c.Request().URL.Path
			if path == "/health" || path == "/ready" || strings.HasPrefix(path, "/sources/") {
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

			if !matchToken(token, tokens) {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API token",
				})
			}

			return next(c)
		}
	}
}

func matchToken(token string, valid []string) bool {
	for _, v := range valid {
		if subtle.ConstantTimeCompare([]byte(token), []byte(v)) == 1 {
			return true
		}
	}
	return false
}
