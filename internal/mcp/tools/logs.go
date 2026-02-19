package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AppLogsInput struct {
	SessionID string `json:"session_id" jsonschema:"required - session ID returned by the register tool"`
	Name      string `json:"name" jsonschema:"required - application name to get logs for"`
	Lines     int64  `json:"lines,omitempty" jsonschema:"number of log lines to return (default: 100)"`
	BuildLogs bool   `json:"build_logs,omitempty" jsonschema:"set to true to get build logs instead of application runtime logs"`
}

// RegisterAppLogs registers the app_logs tool. It needs both the controller-runtime
// client (for listing pods) and the kubernetes clientset (for reading logs).
func RegisterAppLogs(server *gomcp.Server, deps *Dependencies) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "app_logs",
		Description: "Get logs from an application's running pods, or build logs if build_logs=true. Requires session_id from the register tool and the application name. Use build_logs=true to debug build failures. Default: last 100 lines.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input AppLogsInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		lines := input.Lines
		if lines <= 0 {
			lines = 100
		}

		// Verify application exists
		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		result := map[string]any{
			"name":  input.Name,
			"phase": string(app.Status.Phase),
		}

		if input.BuildLogs {
			result["logType"] = "build"
			result["buildStatus"] = app.Status.BuildStatus
			result["logs"] = "Build logs require a kubernetes clientset. Use the API server endpoint GET /api/v1/applications/{name}/build for full build logs."
		} else {
			result["logType"] = "application"
			result["logs"] = "Application logs require a kubernetes clientset. Use the API server endpoint GET /api/v1/applications/{name}/logs for full logs."
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}

// RegisterAppLogsWithClientset registers the app_logs tool with full log streaming support.
func RegisterAppLogsWithClientset(server *gomcp.Server, deps *Dependencies, clientset kubernetes.Interface) {
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "app_logs",
		Description: "Get logs from an application's running pods, or build logs if build_logs=true. Requires session_id from the register tool and the application name. Use build_logs=true to debug build failures. Default: last 100 lines.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input AppLogsInput) (*gomcp.CallToolResult, any, error) {
		namespace, err := deps.ResolveNamespace(input.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if input.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		lines := input.Lines
		if lines <= 0 {
			lines = 100
		}

		var app iafv1alpha1.Application
		if err := deps.Client.Get(ctx, types.NamespacedName{Name: input.Name, Namespace: namespace}, &app); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("application %q not found", input.Name)
			}
			return nil, nil, fmt.Errorf("getting application: %w", err)
		}

		var labelKey string
		var container string
		if input.BuildLogs {
			labelKey = "image.kpack.io/image"
			container = ""
		} else {
			labelKey = "iaf.io/application"
			container = "app"
		}

		podList := &corev1.PodList{}
		if err := deps.Client.List(ctx, podList,
			client.InNamespace(namespace),
			client.MatchingLabels{labelKey: input.Name},
		); err != nil {
			return nil, nil, fmt.Errorf("listing pods: %w", err)
		}

		if len(podList.Items) == 0 {
			result := map[string]any{
				"name":  input.Name,
				"logs":  "No pods found",
				"phase": string(app.Status.Phase),
			}
			text, _ := json.MarshalIndent(result, "", "  ")
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
			}, nil, nil
		}

		pod := podList.Items[len(podList.Items)-1]
		opts := &corev1.PodLogOptions{
			TailLines: &lines,
		}
		if container != "" {
			opts.Container = container
		}

		stream, err := clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, opts).Stream(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("opening log stream: %w", err)
		}
		defer stream.Close()

		data, err := io.ReadAll(stream)
		if err != nil {
			return nil, nil, fmt.Errorf("reading logs: %w", err)
		}

		result := map[string]any{
			"name":    input.Name,
			"logs":    string(data),
			"podName": pod.Name,
			"phase":   string(app.Status.Phase),
		}

		text, _ := json.MarshalIndent(result, "", "  ")
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: string(text)}},
		}, nil, nil
	})
}
