package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/validation"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type AppStatusInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - application name to check status for"`
}

func RegisterAppStatus(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "app_status",
		Description: "Check the current status of an application — phase (Pending/Building/Deploying/Running/Failed), URL, build progress, and replica count. Requires session_id from the register tool and the application name. Use this after push_code or deploy_app to monitor progress.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input AppStatusInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if err := validation.ValidateAppName(input.Name); err != nil {
			return nil, nil, err
		}

		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		result := map[string]any{
			"name":              app.Name,
			"phase":             string(app.Status.Phase),
			"url":               app.Status.URL,
			"latestImage":       app.Status.LatestImage,
			"buildStatus":       app.Status.BuildStatus,
			"availableReplicas": app.Status.AvailableReplicas,
			"replicas":          app.Spec.Replicas,
			"port":              app.Spec.Port,
		}

		// Add source info
		if app.Spec.Image != "" {
			result["sourceType"] = "image"
			result["image"] = app.Spec.Image
		} else if app.Spec.Git != nil {
			result["sourceType"] = "git"
			result["gitUrl"] = app.Spec.Git.URL
			result["gitRevision"] = app.Spec.Git.Revision
		} else if app.Spec.Blob != "" {
			result["sourceType"] = "code"
		}

		if len(app.Status.Conditions) > 0 {
			conditions := make([]map[string]string, 0, len(app.Status.Conditions))
			for _, c := range app.Status.Conditions {
				conditions = append(conditions, map[string]string{
					"type":    c.Type,
					"status":  string(c.Status),
					"reason":  c.Reason,
					"message": c.Message,
				})
			}
			result["conditions"] = conditions
		}

		// Add Grafana Explore deep link when Tempo is configured.
		if deps.TempoURL != "" {
			result["traceExploreUrl"] = buildTraceExploreURL(deps.TempoURL, app.Name)
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// buildTraceExploreURL constructs a Grafana Explore deep link pre-filtered to
// the given application's service.name using TraceQL. The grafanaURL comes from
// platform config (IAF_TEMPO_URL), never from agent input.
func buildTraceExploreURL(grafanaURL, appName string) string {
	// TraceQL query scoped to this app's service.name.
	query := fmt.Sprintf(`{service.name="%s"}`, appName)
	left := fmt.Sprintf(`{"datasource":"Tempo","queries":[{"query":%s,"queryType":"traceql"}],"range":{"from":"now-1h","to":"now"}}`,
		string(mustMarshalJSON(query)))
	params := url.Values{}
	params.Set("orgId", "1")
	params.Set("left", left)
	return grafanaURL + "/explore?" + params.Encode()
}

func mustMarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
