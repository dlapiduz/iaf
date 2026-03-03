package mcp

import (
	"github.com/dlapiduz/iaf/internal/auth"
	iafgithub "github.com/dlapiduz/iaf/internal/github"
	"github.com/dlapiduz/iaf/internal/mcp/prompts"
	"github.com/dlapiduz/iaf/internal/mcp/resources"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	"github.com/dlapiduz/iaf/internal/orgstandards"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// serverInstructions is sent to MCP clients during initialization and injected
// into the LLM's system prompt. This is the primary way agents learn how to
// use IAF.
const serverInstructions = `You are connected to the IAF (Intelligent Application Fabric) MCP server — a platform for deploying applications on Kubernetes.

CRITICAL FIRST STEP: Before calling any other tool, you MUST call the "register" tool to get a session_id. Every other tool requires session_id as a parameter. Without it, all tool calls will fail.

QUICK START:
1. Call "register" (with an optional friendly name) → returns a session_id
2. Call "push_code" with your session_id, an app name, and a map of file paths to contents → builds and deploys your code
3. Call "app_status" with your session_id and app name → check build/deploy progress
4. Once status is "Running", your app is live at http://<app-name>.<base-domain>

AVAILABLE TOOLS (all require session_id except register):
- register: Get a session_id (CALL THIS FIRST)
- push_code: Upload source code files to build and deploy (provide files as {"path": "content"} map)
- deploy_app: Deploy from a container image or git repo (use git_credential for private repos)
- list_apps: See all your deployed apps
- app_status: Check build/deploy progress for an app
- app_logs: View application or build logs
- delete_app: Remove an app and its resources
- add_git_credential: Store a git credential (username/password or SSH key) for private repo access
- list_git_credentials: List stored git credentials (no secrets returned)
- delete_git_credential: Remove a git credential
- list_data_sources: List all platform data sources (databases, APIs, etc.)
- get_data_source: Get details about a specific data source including env var names
- attach_data_source: Attach a data source to your app (injects credentials as env vars)
- provision_service: Provision a managed backing service (e.g. PostgreSQL) — poll service_status every 10s until Ready
- service_status: Check provisioning status; returns connectionEnvVars when Ready
- bind_service: Inject service credentials into an app as K8s Secret references
- unbind_service: Remove service credentials from an app
- deprovision_service: Delete a managed service (must unbind all apps first)
- list_services: List all managed services in your namespace

KEY DETAILS:
- Apps are built automatically using Cloud Native Buildpacks (Go, Node.js, Python, Java, Ruby)
- Default port is 8080 — your app must listen on this port
- Your app will be available at http://<app-name>.<base-domain> once Running
- Each session gets its own isolated Kubernetes namespace
- Use app_status to monitor builds (typically ~2 minutes)
- Use app_logs with build_logs=true to debug build failures

CODING STANDARDS:
- Read the coding-guide prompt for organisation coding standards before writing any code
- Read iaf://org/coding-standards for the machine-readable standards document`

// NewServer creates and configures the MCP server with all tools.
// If loader is non-nil, org standards are served from that loader; otherwise platform defaults are used.
// ghClient may be nil — GitHub tools are omitted when it is not set.
// If clientset is non-nil, app_logs will stream real logs from pods.
func NewServer(k8sClient client.Client, sessions *auth.SessionStore, store *sourcestore.Store, baseDomain string, loader *orgstandards.Loader, ghClient iafgithub.Client, ghOrg, ghToken string, tempoURL string, clientset ...kubernetes.Interface) *gomcp.Server {
	server := gomcp.NewServer(
		&gomcp.Implementation{
			Name:    "iaf",
			Version: "0.1.0",
		},
		&gomcp.ServerOptions{
			Instructions: serverInstructions,
		},
	)

	deps := &tools.Dependencies{
		Client:       k8sClient,
		Store:        store,
		BaseDomain:   baseDomain,
		Sessions:     sessions,
		OrgStandards: loader,
		GitHub:       ghClient,
		GitHubOrg:    ghOrg,
		GitHubToken:  ghToken,
		TempoURL:     tempoURL,
	}

	tools.RegisterRegisterTool(server, deps)
	tools.RegisterDeployApp(server, deps)
	tools.RegisterPushCode(server, deps)
	tools.RegisterAddGitCredential(server, deps)
	tools.RegisterListGitCredentials(server, deps)
	tools.RegisterDeleteGitCredential(server, deps)
	tools.RegisterAppStatus(server, deps)
	if len(clientset) > 0 && clientset[0] != nil {
		tools.RegisterAppLogsWithClientset(server, deps, clientset[0])
	} else {
		tools.RegisterAppLogs(server, deps)
	}
	tools.RegisterListApps(server, deps)
	tools.RegisterDeleteApp(server, deps)
	tools.RegisterListDataSources(server, deps)
	tools.RegisterGetDataSource(server, deps)
	tools.RegisterAttachDataSource(server, deps)
	tools.RegisterProvisionService(server, deps)
	tools.RegisterServiceStatus(server, deps)
	tools.RegisterBindService(server, deps)
	tools.RegisterUnbindService(server, deps)
	tools.RegisterDeprovisionService(server, deps)
	tools.RegisterListServices(server, deps)

	prompts.RegisterDeployGuide(server, deps)
	prompts.RegisterLanguageGuide(server, deps)
	prompts.RegisterCodingGuide(server, deps)
	prompts.RegisterScaffoldGuide(server, deps)
	prompts.RegisterServicesGuide(server, deps)
	prompts.RegisterLoggingGuide(server, deps)
	prompts.RegisterMetricsGuide(server, deps)
	prompts.RegisterTracingGuide(server, deps)

	resources.RegisterPlatformInfo(server, deps)
	resources.RegisterLanguageResources(server, deps)
	resources.RegisterApplicationSpec(server, deps)
	resources.RegisterOrgStandards(server, deps)
	resources.RegisterScaffoldResource(server, deps)
	resources.RegisterDataCatalog(server, deps)
	resources.RegisterLoggingStandards(server, deps)
	resources.RegisterMetricsStandards(server, deps)
	resources.RegisterTracingStandards(server, deps)

	// GitHub components — registered only when a token and org are configured.
	if deps.GitHub != nil {
		tools.RegisterSetupGithubRepo(server, deps)
		resources.RegisterGitHubStandards(server, deps)
		prompts.RegisterGitHubGuide(server, deps)
	}

	return server
}
