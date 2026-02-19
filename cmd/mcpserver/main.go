package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/config"
	"github.com/dlapiduz/iaf/internal/k8s"
	iafmcp "github.com/dlapiduz/iaf/internal/mcp"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	k8sClient, err := k8s.NewClient(cfg.KubeConfig)
	if err != nil {
		logger.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	store, err := sourcestore.New(cfg.SourceStoreDir, cfg.SourceStoreURL, logger)
	if err != nil {
		logger.Error("failed to create source store", "error", err)
		os.Exit(1)
	}

	sessionsPath := filepath.Join(cfg.SourceStoreDir, "sessions.json")
	sessions, err := auth.NewSessionStore(sessionsPath)
	if err != nil {
		logger.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	server := iafmcp.NewServer(k8sClient, sessions, store, cfg.BaseDomain)

	logger.Info("starting MCP server", "transport", cfg.MCPTransport)

	ctx := context.Background()
	transport := &gomcp.StdioTransport{}
	if _, err := server.Connect(ctx, transport, nil); err != nil {
		logger.Error("failed to connect MCP server", "error", err)
		os.Exit(1)
	}

	// Block until stdin is closed
	select {}
}
