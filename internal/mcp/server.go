package mcp

import (
	"github.com/dlapiduz/iaf/internal/mcp/prompts"
	"github.com/dlapiduz/iaf/internal/mcp/resources"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewServer creates and configures the MCP server with all tools.
// If clientset is non-nil, app_logs will stream real logs from pods.
func NewServer(k8sClient client.Client, namespace string, store *sourcestore.Store, baseDomain string, clientset ...kubernetes.Interface) *gomcp.Server {
	server := gomcp.NewServer(
		&gomcp.Implementation{
			Name:    "iaf",
			Version: "0.1.0",
		},
		nil,
	)

	deps := &tools.Dependencies{
		Client:     k8sClient,
		Namespace:  namespace,
		Store:      store,
		BaseDomain: baseDomain,
	}

	tools.RegisterDeployApp(server, deps)
	tools.RegisterPushCode(server, deps)
	tools.RegisterAppStatus(server, deps)
	if len(clientset) > 0 && clientset[0] != nil {
		tools.RegisterAppLogsWithClientset(server, deps, clientset[0])
	} else {
		tools.RegisterAppLogs(server, deps)
	}
	tools.RegisterListApps(server, deps)
	tools.RegisterDeleteApp(server, deps)

	prompts.RegisterDeployGuide(server, deps)
	prompts.RegisterLanguageGuide(server, deps)

	resources.RegisterPlatformInfo(server, deps)
	resources.RegisterLanguageResources(server, deps)
	resources.RegisterApplicationSpec(server, deps)

	return server
}
