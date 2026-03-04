package coach

import "github.com/dlapiduz/iaf/internal/orgstandards"

// Dependencies holds shared dependencies for the coach MCP server.
// Unlike the platform Dependencies, this struct has no K8s client and no session store —
// the coach is stateless and read-only.
type Dependencies struct {
	BaseDomain            string
	OrgStandards          *orgstandards.Loader
	LoggingStandardsFile  string
	MetricsStandardsFile  string
	TracingStandardsFile  string
	GitHubStandardsFile   string
	CICDStandardsFile     string
	SecurityStandardsFile string
}
