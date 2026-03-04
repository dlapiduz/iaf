package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/dlapiduz/iaf/internal/mcp/coach"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	token := os.Getenv("COACH_API_TOKEN")
	if token == "" {
		logger.Error("COACH_API_TOKEN is required — refusing to start without auth (an unauthenticated coach leaks org standards)")
		os.Exit(1)
	}

	deps := &coach.Dependencies{
		BaseDomain:           os.Getenv("COACH_BASE_DOMAIN"),
		LoggingStandardsFile: os.Getenv("COACH_LOGGING_STANDARDS_FILE"),
		MetricsStandardsFile: os.Getenv("COACH_METRICS_STANDARDS_FILE"),
		TracingStandardsFile: os.Getenv("COACH_TRACING_STANDARDS_FILE"),
	}

	orgFile := os.Getenv("COACH_ORG_STANDARDS_FILE")
	orgLoader := orgstandards.New(orgFile, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go orgLoader.Start(ctx)
	deps.OrgStandards = orgLoader

	server := coach.NewServer(deps)

	logger.Info("starting coach MCP server", "transport", "stdio")

	transport := &gomcp.StdioTransport{}
	if _, err := server.Connect(ctx, transport, nil); err != nil {
		logger.Error("failed to connect coach MCP server", "error", err)
		os.Exit(1)
	}

	// Block until stdin is closed
	select {}
}
