package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/config"
	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/k8s"
	iafmcp "github.com/dlapiduz/iaf/internal/mcp"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orgLoader := orgstandards.New(cfg.OrgStandardsFile, logger)
	go orgLoader.Start(ctx)

	var ghClient iafgithub.Client
	if cfg.GitHubToken != "" && cfg.GitHubOrg != "" {
		ghClient = iafgithub.NewHTTPClient(cfg.GitHubToken)
	}

	// Attempt to create a Kubernetes clientset for log streaming.
	// Failure is a soft degradation — all other tools still work.
	var clientset kubernetes.Interface
	restCfg, err := k8s.GetConfig(cfg.KubeConfig)
	if err != nil {
		logger.Warn("log streaming: degraded (could not get REST config)", "error", err)
	} else {
		cs, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			logger.Warn("log streaming: degraded (could not create clientset)", "error", err)
		} else {
			clientset = cs
			logger.Info("log streaming: enabled")
		}
	}

	server := iafmcp.NewServer(k8sClient, sessions, store, cfg.BaseDomain, orgLoader, ghClient, cfg.GitHubOrg, cfg.GitHubToken, cfg.TempoURL, clientset)

	logger.Info("starting MCP server", "transport", cfg.MCPTransport)

	transport := &gomcp.StdioTransport{}
	if _, err := server.Connect(ctx, transport, nil); err != nil {
		logger.Error("failed to connect MCP server", "error", err)
		os.Exit(1)
	}

	// Block until stdin is closed
	select {}
}
