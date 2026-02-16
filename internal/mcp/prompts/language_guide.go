package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// languageAliases maps common aliases to canonical language names.
var languageAliases = map[string]string{
	"go":       "go",
	"golang":   "go",
	"node":     "nodejs",
	"nodejs":   "nodejs",
	"node.js":  "nodejs",
	"js":       "nodejs",
	"javascript": "nodejs",
	"python":   "python",
	"py":       "python",
	"python3":  "python",
	"java":     "java",
	"ruby":     "ruby",
	"rb":       "ruby",
}

type languageGuide struct {
	buildpackID    string
	detectionFiles []string
	requiredFiles  string
	example        string
	bestPractices  string
}

var languageGuides = map[string]languageGuide{
	"go": {
		buildpackID:    "paketo-buildpacks/go",
		detectionFiles: []string{"go.mod"},
		requiredFiles: `- go.mod — Go module definition (required for detection)
- main.go (or cmd/*/main.go) — Application entry point with package main`,
		example: `// go.mod
module myapp

go 1.22

// main.go
package main

import (
    "fmt"
    "net/http"
    "os"
)

func main() {
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, "Hello from IAF!")
    })
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })
    http.ListenAndServe(":"+port, nil)
}`,
		bestPractices: `- Listen on the PORT environment variable (the platform sets this; defaults to 8080).
- Use Go modules (go.mod) — GOPATH mode is not supported.
- Provide a /health endpoint for readiness probes.
- The buildpack compiles statically; CGO is disabled by default.`,
	},
	"nodejs": {
		buildpackID:    "paketo-buildpacks/nodejs",
		detectionFiles: []string{"package.json"},
		requiredFiles: `- package.json — Node.js project manifest (required for detection)
- A start script defined in package.json ("scripts": {"start": "..."})
- server.js, index.js, or app.js — Application entry point`,
		example: `// package.json
{
  "name": "myapp",
  "version": "1.0.0",
  "scripts": {
    "start": "node server.js"
  },
  "dependencies": {
    "express": "^4.18.0"
  }
}

// server.js
const express = require('express');
const app = express();
const port = process.env.PORT || 8080;

app.get('/', (req, res) => res.send('Hello from IAF!'));
app.get('/health', (req, res) => res.sendStatus(200));

app.listen(port, () => console.log('Listening on port ' + port));`,
		bestPractices: `- Define a "start" script in package.json — the buildpack uses this to launch your app.
- Listen on the PORT environment variable (defaults to 8080).
- Include a package-lock.json for reproducible builds.
- Do not include node_modules in uploaded source.
- Provide a /health endpoint for readiness probes.`,
	},
	"python": {
		buildpackID:    "paketo-buildpacks/python",
		detectionFiles: []string{"requirements.txt", "setup.py", "pyproject.toml"},
		requiredFiles: `- requirements.txt, setup.py, or pyproject.toml — Dependency manifest (at least one required for detection)
- Procfile with a "web" process type, OR an app.py/main.py entry point`,
		example: `# requirements.txt
flask>=3.0

# Procfile
web: python app.py

# app.py
import os
from flask import Flask

app = Flask(__name__)

@app.route("/")
def hello():
    return "Hello from IAF!"

@app.route("/health")
def health():
    return "ok", 200

if __name__ == "__main__":
    port = int(os.environ.get("PORT", 8080))
    app.run(host="0.0.0.0", port=port)`,
		bestPractices: `- Use a Procfile to explicitly declare the web process: "web: python app.py"
- Listen on the PORT environment variable (defaults to 8080).
- Bind to 0.0.0.0, not localhost, so the container is reachable.
- Include requirements.txt with pinned versions for reproducible builds.
- Provide a /health endpoint for readiness probes.`,
	},
	"java": {
		buildpackID:    "paketo-buildpacks/java",
		detectionFiles: []string{"pom.xml", "build.gradle", "build.gradle.kts"},
		requiredFiles: `- pom.xml (Maven) or build.gradle/build.gradle.kts (Gradle) — Build manifest (required for detection)
- src/main/java/ — Source tree with application entry point`,
		example: `<!-- pom.xml (Spring Boot example) -->
<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <parent>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-starter-parent</artifactId>
    <version>3.2.0</version>
  </parent>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-actuator</artifactId>
    </dependency>
  </dependencies>
</project>

// src/main/java/com/example/myapp/Application.java
package com.example.myapp;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.web.bind.annotation.*;

@SpringBootApplication
@RestController
public class Application {
    public static void main(String[] args) {
        SpringApplication.run(Application.class, args);
    }

    @GetMapping("/")
    public String hello() { return "Hello from IAF!"; }
}`,
		bestPractices: `- The buildpack auto-detects Maven or Gradle and runs the build.
- Use server.port=${PORT:8080} in application.properties to respect the PORT variable.
- Spring Boot Actuator provides /actuator/health automatically — recommended for readiness probes.
- The buildpack applies memory calculator settings for JVM tuning.
- Executable JARs are preferred; the buildpack handles layer extraction.`,
	},
	"ruby": {
		buildpackID:    "paketo-buildpacks/ruby",
		detectionFiles: []string{"Gemfile"},
		requiredFiles: `- Gemfile — Ruby dependency manifest (required for detection)
- Gemfile.lock — Locked dependency versions
- config.ru (Rack) or Procfile with a web process`,
		example: `# Gemfile
source "https://rubygems.org"
gem "sinatra", "~> 3.0"
gem "puma", "~> 6.0"

# config.ru
require './app'
run Sinatra::Application

# app.rb
require 'sinatra'

set :port, ENV['PORT'] || 8080
set :bind, '0.0.0.0'

get '/' do
  'Hello from IAF!'
end

get '/health' do
  status 200
  'ok'
end`,
		bestPractices: `- Include Gemfile.lock for reproducible builds (run "bundle lock" locally).
- Use Puma as the web server for production concurrency.
- Listen on the PORT environment variable (defaults to 8080).
- Bind to 0.0.0.0 so the container is reachable.
- Use config.ru for Rack-based apps or a Procfile for custom start commands.
- Provide a /health endpoint for readiness probes.`,
	},
}

// RegisterLanguageGuide registers the language-guide prompt that provides
// per-language buildpack-compatible application structure guidance.
func RegisterLanguageGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "language-guide",
		Description: "Per-language buildpack-compatible app structure: required files, minimal example, detection rules, and best practices.",
		Arguments: []*gomcp.PromptArgument{
			{
				Name:        "language",
				Description: "Target language (go, nodejs, python, java, ruby). Aliases like 'golang', 'node', 'py', 'rb' are accepted.",
				Required:    true,
			},
		},
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		lang := strings.ToLower(strings.TrimSpace(req.Params.Arguments["language"]))
		canonical, ok := languageAliases[lang]
		if !ok {
			supported := []string{"go", "nodejs", "python", "java", "ruby"}
			return nil, fmt.Errorf("unsupported language %q; supported languages: %s", lang, strings.Join(supported, ", "))
		}

		guide := languageGuides[canonical]
		text := fmt.Sprintf(`# %s Application Guide for IAF

## Buildpack
%s (auto-detected by Cloud Native Buildpacks on the Paketo Jammy LTS stack)

## Detection Files
The buildpack detects a %s application when it finds: %s

## Required Files
%s

## Minimal Example
%s

## Best Practices
%s

## Deployment
After writing your code, use push_code to upload the source, then deploy_app to create the Application. The platform builds the image automatically and deploys it at http://<name>.%s.
`, strings.ToUpper(canonical[:1])+canonical[1:],
			guide.buildpackID,
			canonical,
			strings.Join(guide.detectionFiles, ", "),
			guide.requiredFiles,
			guide.example,
			guide.bestPractices,
			deps.BaseDomain)

		return &gomcp.GetPromptResult{
			Description: fmt.Sprintf("Buildpack-compatible %s application guide for IAF.", canonical),
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}
