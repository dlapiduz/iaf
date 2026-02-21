package resources

import (
	"context"
	"encoding/json"
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterDataCatalog registers the iaf://catalog/data-sources MCP resource.
// Returns a JSON index of all cluster-registered DataSources. No credential data is included.
func RegisterDataCatalog(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResource(&gomcp.Resource{
		URI:         "iaf://catalog/data-sources",
		Name:        "data-catalog",
		Description: "JSON index of all data sources registered on the platform. Contains metadata only — no credentials. Use list_data_sources or get_data_source tools for filtered access.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		var list iafv1alpha1.DataSourceList
		if err := deps.Client.List(ctx, &list); err != nil {
			return nil, fmt.Errorf("listing data sources: %w", err)
		}

		entries := make([]map[string]any, 0, len(list.Items))
		for _, ds := range list.Items {
			envVarNames := make([]string, 0, len(ds.Spec.EnvVarMapping))
			for _, v := range ds.Spec.EnvVarMapping {
				envVarNames = append(envVarNames, v)
			}
			entry := map[string]any{
				"name":        ds.Name,
				"kind":        ds.Spec.Kind,
				"envVarNames": envVarNames,
			}
			if ds.Spec.Description != "" {
				entry["description"] = ds.Spec.Description
			}
			if ds.Spec.Schema != "" {
				entry["schema"] = ds.Spec.Schema
			}
			if len(ds.Spec.Tags) > 0 {
				entry["tags"] = ds.Spec.Tags
			}
			entries = append(entries, entry)
		}

		payload := map[string]any{
			"dataSources": entries,
			"total":       len(entries),
			"note":        "Credential values are never included. Use attach_data_source to make a data source available to your app.",
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling data catalog: %w", err)
		}

		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}
