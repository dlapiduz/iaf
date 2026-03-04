package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for IAF components.
type Config struct {
	// API server settings
	APIPort   int      `mapstructure:"api_port"`
	APITokens []string `mapstructure:"api_tokens"`

	// MCP server settings
	MCPTransport string `mapstructure:"mcp_transport"` // "stdio" or "http"
	MCPPort      int    `mapstructure:"mcp_port"`

	// Kubernetes settings
	DefaultNamespace string `mapstructure:"default_namespace"`
	KubeConfig       string `mapstructure:"kubeconfig"`

	// kpack settings
	ClusterBuilder string `mapstructure:"cluster_builder"`
	RegistryPrefix string `mapstructure:"registry_prefix"`

	// Source store settings
	SourceStoreDir string `mapstructure:"source_store_dir"`
	SourceStoreURL string `mapstructure:"source_store_url"`

	// Routing
	BaseDomain string `mapstructure:"base_domain"`
	// TLSIssuer is the ClusterIssuer name for cert-manager. Default: "selfsigned-issuer".
	// Set to "" to disable TLS certificate provisioning (e.g., cert-manager not installed).
	TLSIssuer string `mapstructure:"tls_issuer"`

	// Org standards
	OrgStandardsFile string `mapstructure:"org_standards_file"`

	// Session lifecycle — optional. Zero values disable GC (sessions never expire).
	// IAF_SESSION_TTL: idle TTL for new sessions (e.g. "24h"). 0 = no expiry.
	// IAF_SESSION_GC_INTERVAL: how often to check for expired sessions (e.g. "1h"). 0 = disabled.
	SessionTTL        time.Duration `mapstructure:"session_ttl"`
	SessionGCInterval time.Duration `mapstructure:"session_gc_interval"`

	// GitHub integration (optional — GitHub features are disabled when token is empty)
	GitHubToken string `mapstructure:"github_token"`
	GitHubOrg   string `mapstructure:"github_org"`

	// Observability (optional — features are disabled when URLs are empty)
	// TempoURL is the Grafana base URL for trace explore links (IAF_TEMPO_URL).
	TempoURL string `mapstructure:"tempo_url"`

	// Coach server proxy (optional — coaching proxy is disabled when CoachURL is empty).
	// IAF_COACH_URL:   Streamable-HTTP MCP endpoint of the coach server (e.g. http://coach.iaf-system/mcp).
	// IAF_COACH_TOKEN: Bearer token for authenticating platform → coach requests. Mount from K8s Secret.
	CoachURL   string `mapstructure:"coach_url"`
	CoachToken string `mapstructure:"coach_token"`
}

// Load reads configuration from environment variables and defaults.
func Load() (*Config, error) {
	v := viper.New()

	v.SetDefault("api_port", 8080)
	v.SetDefault("api_tokens", []string{"iaf-dev-key"})
	v.SetDefault("mcp_transport", "stdio")
	v.SetDefault("mcp_port", 8081)
	v.SetDefault("default_namespace", "iaf-apps")
	v.SetDefault("cluster_builder", "iaf-cluster-builder")
	v.SetDefault("registry_prefix", "registry.localhost:5000/iaf")
	v.SetDefault("source_store_dir", "/tmp/iaf-sources")
	v.SetDefault("source_store_url", "http://iaf-source-store.iaf-system.svc.cluster.local")
	v.SetDefault("base_domain", "localhost")
	v.SetDefault("tls_issuer", "")
	v.SetDefault("org_standards_file", "")
	v.SetDefault("github_token", "")
	v.SetDefault("github_org", "")
	v.SetDefault("tempo_url", "")
	v.SetDefault("session_ttl", 0)
	v.SetDefault("session_gc_interval", 0)
	v.SetDefault("coach_url", "")
	v.SetDefault("coach_token", "")

	v.SetEnvPrefix("IAF")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
