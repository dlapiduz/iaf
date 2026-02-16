package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for IAF components.
type Config struct {
	// API server settings
	APIPort    int    `mapstructure:"api_port"`
	APIKey     string `mapstructure:"api_key"`

	// MCP server settings
	MCPTransport string `mapstructure:"mcp_transport"` // "stdio" or "http"
	MCPPort      int    `mapstructure:"mcp_port"`

	// Kubernetes settings
	Namespace       string `mapstructure:"namespace"`
	KubeConfig      string `mapstructure:"kubeconfig"`

	// kpack settings
	ClusterBuilder string `mapstructure:"cluster_builder"`
	RegistryPrefix string `mapstructure:"registry_prefix"`

	// Source store settings
	SourceStoreDir string `mapstructure:"source_store_dir"`
	SourceStoreURL string `mapstructure:"source_store_url"`

	// Routing
	BaseDomain string `mapstructure:"base_domain"`
}

// Load reads configuration from environment variables and defaults.
func Load() (*Config, error) {
	v := viper.New()

	v.SetDefault("api_port", 8080)
	v.SetDefault("api_key", "iaf-dev-key")
	v.SetDefault("mcp_transport", "stdio")
	v.SetDefault("mcp_port", 8081)
	v.SetDefault("namespace", "iaf-apps")
	v.SetDefault("cluster_builder", "iaf-cluster-builder")
	v.SetDefault("registry_prefix", "registry.localhost:5000/iaf")
	v.SetDefault("source_store_dir", "/tmp/iaf-sources")
	v.SetDefault("source_store_url", "http://iaf-source-store.iaf-system.svc.cluster.local")
	v.SetDefault("base_domain", "localhost")

	v.SetEnvPrefix("IAF")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
