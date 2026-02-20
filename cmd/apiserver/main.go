package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dlapiduz/iaf/internal/api"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/config"
	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/k8s"
	iafmcp "github.com/dlapiduz/iaf/internal/mcp"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	"github.com/labstack/echo/v4"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Create K8s clients
	k8sClient, err := k8s.NewClient(cfg.KubeConfig)
	if err != nil {
		logger.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	restConfig, err := k8s.GetConfig(cfg.KubeConfig)
	if err != nil {
		logger.Error("failed to get kubernetes config", "error", err)
		os.Exit(1)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		logger.Error("failed to create kubernetes clientset", "error", err)
		os.Exit(1)
	}

	// Create source store
	store, err := sourcestore.New(cfg.SourceStoreDir, cfg.SourceStoreURL, logger)
	if err != nil {
		logger.Error("failed to create source store", "error", err)
		os.Exit(1)
	}

	// Create session store
	sessionsPath := filepath.Join(cfg.SourceStoreDir, "sessions.json")
	sessions, err := auth.NewSessionStore(sessionsPath)
	if err != nil {
		logger.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	// Create and configure Echo server
	e := api.NewServer(cfg.APITokens, logger)

	// Register REST API routes
	api.RegisterRoutes(e, k8sClient, clientset, sessions, store)

	// Mount source store file server
	e.GET("/sources/*", echo.WrapHandler(http.StripPrefix("/sources/", store.Handler())))

	// Create org standards loader (hot-reloads from IAF_ORG_STANDARDS_FILE)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	orgLoader := orgstandards.New(cfg.OrgStandardsFile, logger)
	go orgLoader.Start(ctx)

	// Create GitHub client if configured.
	var ghClient iafgithub.Client
	if cfg.GitHubToken != "" && cfg.GitHubOrg != "" {
		ghClient = iafgithub.NewHTTPClient(cfg.GitHubToken)
	}

	// Create MCP server and mount as Streamable HTTP endpoint
	mcpServer := iafmcp.NewServer(k8sClient, sessions, store, cfg.BaseDomain, orgLoader, ghClient, cfg.GitHubOrg, cfg.GitHubToken, clientset)
	mcpHandler := gomcp.NewStreamableHTTPHandler(func(r *http.Request) *gomcp.Server {
		return mcpServer
	}, &gomcp.StreamableHTTPOptions{Stateless: true})
	e.Any("/mcp", echo.WrapHandler(mcpHandler))

	addr := fmt.Sprintf(":%d", cfg.APIPort)
	logger.Info("starting API server", "addr", addr, "mcp", fmt.Sprintf("http://localhost%s/mcp", addr))
	if err := e.Start(addr); err != nil {
		logger.Error("API server exited with error", "error", err)
		os.Exit(1)
	}
}
