package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type languageSpec struct {
	Language       string            `json:"language"`
	BuildpackID    string            `json:"buildpackId"`
	DetectionFiles []string          `json:"detectionFiles"`
	RequiredFiles  []string          `json:"requiredFiles"`
	RecommendedFiles []string        `json:"recommendedFiles"`
	EnvVars        map[string]string `json:"envVars"`
}

var languages = map[string]languageSpec{
	"go": {
		Language:       "go",
		BuildpackID:    "paketo-buildpacks/go",
		DetectionFiles: []string{"go.mod"},
		RequiredFiles:  []string{"go.mod", "main.go or cmd/*/main.go"},
		RecommendedFiles: []string{"go.sum"},
		EnvVars:        map[string]string{"PORT": "8080"},
	},
	"nodejs": {
		Language:       "nodejs",
		BuildpackID:    "paketo-buildpacks/nodejs",
		DetectionFiles: []string{"package.json"},
		RequiredFiles:  []string{"package.json", "server.js or index.js or app.js"},
		RecommendedFiles: []string{"package-lock.json", ".node-version"},
		EnvVars:        map[string]string{"PORT": "8080", "NODE_ENV": "production"},
	},
	"python": {
		Language:       "python",
		BuildpackID:    "paketo-buildpacks/python",
		DetectionFiles: []string{"requirements.txt", "setup.py", "pyproject.toml"},
		RequiredFiles:  []string{"requirements.txt or setup.py or pyproject.toml", "Procfile or app.py"},
		RecommendedFiles: []string{"runtime.txt", "Procfile"},
		EnvVars:        map[string]string{"PORT": "8080"},
	},
	"java": {
		Language:       "java",
		BuildpackID:    "paketo-buildpacks/java",
		DetectionFiles: []string{"pom.xml", "build.gradle", "build.gradle.kts"},
		RequiredFiles:  []string{"pom.xml or build.gradle", "src/main/java/**/*.java"},
		RecommendedFiles: []string{"mvnw or gradlew", "src/main/resources/application.properties"},
		EnvVars:        map[string]string{"PORT": "8080", "SERVER_PORT": "8080"},
	},
	"ruby": {
		Language:       "ruby",
		BuildpackID:    "paketo-buildpacks/ruby",
		DetectionFiles: []string{"Gemfile"},
		RequiredFiles:  []string{"Gemfile", "Gemfile.lock", "config.ru or Procfile"},
		RecommendedFiles: []string{".ruby-version"},
		EnvVars:        map[string]string{"PORT": "8080", "RACK_ENV": "production"},
	},
}

// languageAliases maps aliases to canonical language names.
var langAliases = map[string]string{
	"go": "go", "golang": "go",
	"nodejs": "nodejs", "node": "nodejs", "node.js": "nodejs", "js": "nodejs", "javascript": "nodejs",
	"python": "python", "py": "python", "python3": "python",
	"java": "java",
	"ruby": "ruby", "rb": "ruby",
}

// RegisterLanguageResources registers the iaf://languages/{language} resource
// template that provides structured per-language buildpack data.
func RegisterLanguageResources(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddResourceTemplate(&gomcp.ResourceTemplate{
		URITemplate: "iaf://languages/{language}",
		Name:        "language-spec",
		Description: "Structured language specification — buildpack ID, detection files, required/recommended files, and environment variables.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		// Extract language from URI: iaf://languages/<language>
		uri := req.Params.URI
		lang := ""
		if idx := strings.LastIndex(uri, "/"); idx >= 0 {
			lang = strings.ToLower(uri[idx+1:])
		}

		canonical, ok := langAliases[lang]
		if !ok {
			return nil, gomcp.ResourceNotFoundError(uri)
		}

		spec, ok := languages[canonical]
		if !ok {
			return nil, gomcp.ResourceNotFoundError(uri)
		}

		data, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling language spec: %w", err)
		}

		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{
				{URI: uri, MIMEType: "application/json", Text: string(data)},
			},
		}, nil
	})
}
