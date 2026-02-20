package resources

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed all:scaffolds
var scaffoldEmbedFS embed.FS

// allowedFrameworks is the allowlist of supported scaffold names.
// The raw URI segment is validated against this list before any FS access.
var allowedFrameworks = map[string]bool{
	"nextjs": true,
	"html":   true,
}

// RegisterScaffoldResource registers the iaf://scaffold/{framework} resource
// template. The handler walks the embedded FS for the validated framework and
// returns a JSON file map that agents can pass directly to push_code.
func RegisterScaffoldResource(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResourceTemplate(&gomcp.ResourceTemplate{
		URITemplate: "iaf://scaffold/{framework}",
		Name:        "scaffold",
		Description: "UI scaffold file map for a given framework (nextjs or html). Returns a {\"path\": \"content\"} JSON object ready to pass to push_code.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		uri := req.Params.URI

		// Extract and validate the framework segment — never pass raw URI to FS.
		framework := strings.TrimPrefix(uri, "iaf://scaffold/")
		if !allowedFrameworks[framework] {
			supported := []string{"nextjs", "html"}
			return nil, fmt.Errorf("unknown scaffold framework %q; supported: %s", framework, strings.Join(supported, ", "))
		}

		fsRoot := "scaffolds/" + framework
		files, err := collectFiles(scaffoldEmbedFS, fsRoot)
		if err != nil {
			return nil, fmt.Errorf("reading scaffold %q: %w", framework, err)
		}

		data, err := json.MarshalIndent(files, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling scaffold: %w", err)
		}

		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: uri, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}

// collectFiles walks fsys rooted at root and returns a map of
// relative-path → file-content for all regular files.
func collectFiles(fsys embed.FS, root string) (map[string]string, error) {
	files := make(map[string]string)
	prefix := root + "/"

	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		f, err := fsys.Open(path)
		if err != nil {
			return fmt.Errorf("open %q: %w", path, err)
		}
		defer f.Close()

		data, err := io.ReadAll(f)
		if err != nil {
			return fmt.Errorf("read %q: %w", path, err)
		}

		// Strip the "scaffolds/{framework}/" prefix so agents receive
		// "pages/index.js" not "scaffolds/nextjs/pages/index.js".
		key := strings.TrimPrefix(path, prefix)
		files[key] = string(data)
		return nil
	})
	return files, err
}
