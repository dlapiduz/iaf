package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dlapiduz/iaf/internal/middleware"
	"github.com/labstack/echo/v4"
)

func okHandler(c echo.Context) error {
	return c.String(http.StatusOK, "ok")
}

func makeAuthRequest(method, path, auth string) (*httptest.ResponseRecorder, echo.Context) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	return rec, e.NewContext(req, rec)
}

func TestAuth(t *testing.T) {
	tokens := []string{"valid-token", "another-token"}
	mw := middleware.Auth(tokens)
	handler := mw(okHandler)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "valid token passes",
			path:       "/api/v1/applications",
			authHeader: "Bearer valid-token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "second valid token passes",
			path:       "/api/v1/applications",
			authHeader: "Bearer another-token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid token rejected",
			path:       "/api/v1/applications",
			authHeader: "Bearer bad-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing auth header rejected",
			path:       "/api/v1/applications",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "non-bearer format rejected",
			path:       "/api/v1/applications",
			authHeader: "Basic dXNlcjpwYXNz",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "health endpoint bypasses auth",
			path:       "/health",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "ready endpoint bypasses auth",
			path:       "/ready",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "sources path bypasses auth",
			path:       "/sources/myapp-abc123.tar.gz",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, c := makeAuthRequest(http.MethodGet, tc.path, tc.authHeader)
			if err := handler(c); err != nil {
				// Echo error handler writes status
				c.Echo().DefaultHTTPErrorHandler(err, c)
			}
			if rec.Code != tc.wantStatus {
				t.Errorf("got status %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
