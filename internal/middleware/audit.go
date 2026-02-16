package middleware

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// Audit returns an Echo middleware that logs API requests for audit purposes.
func Audit(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()

			// Get request ID from context
			requestID := c.Response().Header().Get(echo.HeaderXRequestID)

			err := next(c)

			logger.Info("api_request",
				"request_id", requestID,
				"method", req.Method,
				"path", req.URL.Path,
				"status", c.Response().Status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", req.RemoteAddr,
				"user_agent", req.UserAgent(),
			)

			return err
		}
	}
}
