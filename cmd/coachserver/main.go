package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/coach"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	token := os.Getenv("COACH_API_TOKEN")
	if token == "" {
		logger.Error("COACH_API_TOKEN is required — refusing to start without auth")
		os.Exit(1)
	}

	port := os.Getenv("COACH_PORT")
	if port == "" {
		port = "8082"
	}

	deps := &coach.Dependencies{
		BaseDomain:            os.Getenv("COACH_BASE_DOMAIN"),
		LoggingStandardsFile:  os.Getenv("COACH_LOGGING_STANDARDS_FILE"),
		MetricsStandardsFile:  os.Getenv("COACH_METRICS_STANDARDS_FILE"),
		TracingStandardsFile:  os.Getenv("COACH_TRACING_STANDARDS_FILE"),
		GitHubStandardsFile:   os.Getenv("COACH_GITHUB_STANDARDS_FILE"),
		CICDStandardsFile:     os.Getenv("COACH_CICD_STANDARDS_FILE"),
		SecurityStandardsFile: os.Getenv("COACH_SECURITY_STANDARDS_FILE"),
		LicensePolicyFile:     os.Getenv("COACH_LICENSE_POLICY_FILE"),
	}

	orgFile := os.Getenv("COACH_ORG_STANDARDS_FILE")
	orgLoader := orgstandards.New(orgFile, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go orgLoader.Start(ctx)
	deps.OrgStandards = orgLoader

	server := coach.NewServer(deps)

	mcpHandler := gomcp.NewStreamableHTTPHandler(func(r *http.Request) *gomcp.Server {
		return server
	}, &gomcp.StreamableHTTPOptions{Stateless: true})

	mux := http.NewServeMux()
	mux.Handle("/mcp", bearerAuth(token, mcpHandler))

	addr := fmt.Sprintf(":%s", port)
	logger.Info("starting coach MCP server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("coach server exited", "error", err)
		os.Exit(1)
	}
}

// bearerAuth wraps an http.Handler with Bearer token authentication.
func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		incoming := strings.TrimPrefix(auth, "Bearer ")
		if incoming == auth || subtle.ConstantTimeCompare([]byte(incoming), []byte(token)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
