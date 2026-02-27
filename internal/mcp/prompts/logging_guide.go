package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type loggingGuide struct {
	library string
	install string
	init    string
	request string
	errLog  string
}

var loggingGuides = map[string]loggingGuide{
	"go": {
		library: "log/slog (stdlib, Go 1.21+)",
		install: "No installation needed — log/slog is part of the Go standard library (Go 1.21+).",
		init: `import (
    "log/slog"
    "os"
)

// In main() or init():
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo, // or read from LOG_LEVEL env var
}))
slog.SetDefault(logger)`,
		request: `slog.Info("request completed",
    "method", r.Method,
    "path",   r.URL.Path,
    "status", statusCode,
    "duration_ms", durationMS,
    "req_id", r.Header.Get("X-Request-ID"),
)`,
		errLog: `slog.Error("operation failed",
    "error", err.Error(),
    "path",  r.URL.Path,
)`,
	},
	"nodejs": {
		library: "pino",
		install: "npm install pino",
		init: `const pino = require('pino');
const logger = pino({
  level: process.env.LOG_LEVEL || 'info',
  // pino outputs NDJSON to stdout by default — no further config needed
});`,
		request: `logger.info({
  method: req.method,
  path:   req.path,
  status: res.statusCode,
  duration_ms: durationMS,
  req_id: req.headers['x-request-id'],
}, 'request completed');`,
		errLog: `logger.error({ error: err.message, path: req.path }, 'operation failed');`,
	},
	"python": {
		library: "python-json-logger",
		install: "pip install python-json-logger",
		init: `import logging
import os
from pythonjsonlogger import jsonlogger

logger = logging.getLogger()
handler = logging.StreamHandler()  # stdout
formatter = jsonlogger.JsonFormatter(
    fmt='%(asctime)s %(levelname)s %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%SZ',
    rename_fields={'asctime': 'time', 'levelname': 'level', 'message': 'msg'},
)
handler.setFormatter(formatter)
logger.addHandler(handler)
logger.setLevel(os.environ.get('LOG_LEVEL', 'INFO').upper())`,
		request: `logger.info('request completed', extra={
    'method': request.method,
    'path':   request.path,
    'status': response.status_code,
    'duration_ms': duration_ms,
    'req_id': request.headers.get('X-Request-ID'),
})`,
		errLog: `logger.error('operation failed', extra={'error': str(e), 'path': request.path})`,
	},
	"java": {
		library: "logback + logstash-logback-encoder",
		install: `<!-- pom.xml -->
<dependency>
  <groupId>net.logstash.logback</groupId>
  <artifactId>logstash-logback-encoder</artifactId>
  <version>7.4</version>
</dependency>`,
		init: `<!-- src/main/resources/logback.xml -->
<configuration>
  <appender name="STDOUT" class="ch.qos.logback.core.ConsoleAppender">
    <encoder class="net.logstash.logback.encoder.LogstashEncoder"/>
  </appender>
  <root level="${LOG_LEVEL:-INFO}">
    <appender-ref ref="STDOUT"/>
  </root>
</configuration>`,
		request: `import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import net.logstash.logback.argument.StructuredArguments;

Logger log = LoggerFactory.getLogger(MyController.class);
log.info("request completed",
    StructuredArguments.kv("method", method),
    StructuredArguments.kv("path",   path),
    StructuredArguments.kv("status", statusCode),
    StructuredArguments.kv("duration_ms", durationMs),
    StructuredArguments.kv("req_id", reqId));`,
		errLog: `log.error("operation failed",
    StructuredArguments.kv("error", e.getMessage()),
    StructuredArguments.kv("path", path));`,
	},
	"ruby": {
		library: "semantic_logger",
		install: "gem 'amazing_print', 'semantic_logger'  # Gemfile\nbundle install",
		init: `require 'semantic_logger'

SemanticLogger.default_level = ENV.fetch('LOG_LEVEL', 'info').downcase.to_sym
SemanticLogger.add_appender(io: $stdout, formatter: :json)

logger = SemanticLogger['MyApp']`,
		request: `logger.info 'request completed',
  method: request.request_method,
  path:   request.path,
  status: response.status,
  duration_ms: duration_ms,
  req_id: request.env['HTTP_X_REQUEST_ID']`,
		errLog: `logger.error 'operation failed', error: e.message, path: request.path`,
	},
}

// RegisterLoggingGuide registers the logging-guide prompt that provides
// per-language structured logging setup for IAF applications.
func RegisterLoggingGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "logging-guide",
		Description: "Per-language structured logging setup: library choice, initialization, request logging, error logging, and security rules for IAF apps.",
		Arguments: []*gomcp.PromptArgument{
			{
				Name:        "language",
				Description: "Target language (go, nodejs, python, java, ruby). Aliases like 'golang', 'node', 'py' are accepted.",
				Required:    false,
			},
		},
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		lang := strings.ToLower(strings.TrimSpace(req.Params.Arguments["language"]))

		var text string
		if lang == "" {
			text = loggingGuideOverview()
		} else {
			canonical, ok := languageAliases[lang]
			if !ok {
				return nil, fmt.Errorf("unsupported language %q; supported languages: go, nodejs, python, java, ruby", lang)
			}
			guide := loggingGuides[canonical]
			text = renderLoggingGuide(canonical, guide)
		}

		return &gomcp.GetPromptResult{
			Description: "Structured logging guide for IAF applications.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}

func loggingGuideOverview() string {
	return `# IAF Logging Guide

## The One Rule
**Log to stdout in JSON Lines (NDJSON) format — one JSON object per line.**

Kubernetes captures stdout automatically. Grafana Alloy ships it to Loki. Nothing else is needed. Do not log to files. Do not configure a log shipper.

## Required Fields
Every log line must include:
- ` + "`time`" + ` — RFC3339 timestamp (e.g. ` + "`2026-02-19T14:23:01Z`" + `)
- ` + "`level`" + ` — severity: ` + "`debug`" + `, ` + "`info`" + `, ` + "`warn`" + `, ` + "`error`" + `
- ` + "`msg`" + ` — human-readable message

## Recommended Fields
Add these where relevant: ` + "`app`" + `, ` + "`req_id`" + `, ` + "`method`" + `, ` + "`path`" + `, ` + "`status`" + `, ` + "`duration_ms`" + `, ` + "`error`" + `

## Trace Correlation
When OTel tracing is enabled, add ` + "`trace_id`" + ` and ` + "`span_id`" + ` to every log line. See ` + "`tracing-guide`" + ` and ` + "`iaf://org/tracing-standards`" + `.

## Security — What Never to Log
- API tokens, passwords, or any value from a Kubernetes Secret
- Full request/response bodies (unless debug mode with explicit opt-in)
- PII: names, emails, phone numbers, IP addresses

## Full Standards Reference
Read ` + "`iaf://org/logging-standards`" + ` for the machine-readable standard.

## Language-Specific Setup
Run ` + "`logging-guide`" + ` with a ` + "`language`" + ` argument for copy-paste-ready code:
- ` + "`logging-guide language=go`" + `      — log/slog (stdlib)
- ` + "`logging-guide language=nodejs`" + `  — pino
- ` + "`logging-guide language=python`" + `  — python-json-logger
- ` + "`logging-guide language=java`" + `    — logback + logstash-logback-encoder
- ` + "`logging-guide language=ruby`" + `    — semantic_logger
`
}

func renderLoggingGuide(lang string, g loggingGuide) string {
	title := strings.ToUpper(lang[:1]) + lang[1:]
	return fmt.Sprintf(`# %s Logging Guide for IAF

## Library
%s

## Installation
%s

## Initialization
` + "```" + `
%s
` + "```" + `

## Log an HTTP Request (with recommended fields)
` + "```" + `
%s
` + "```" + `

## Log an Error
` + "```" + `
%s
` + "```" + `

## Log Level via Environment Variable
Read ` + "`LOG_LEVEL`" + ` at startup (` + "`debug`" + `, ` + "`info`" + `, ` + "`warn`" + `, ` + "`error`" + `). Default to ` + "`info`" + `. This follows the same pattern as ` + "`PORT`" + ` — set it as an env var in your Application spec when needed.

## Trace Correlation
When OTel tracing is enabled, extract ` + "`trace_id`" + ` and ` + "`span_id`" + ` from the active span context and add them as log fields. See the ` + "`tracing-guide`" + ` prompt for language-specific extraction code.

## Rules
- **Log to stdout only.** Do not configure file appenders or log shippers.
- **Do not log secrets, tokens, or env var values that may contain credentials.**
- Kubernetes captures stdout automatically; Grafana Alloy ships it to Loki.

## Verify
After deploying, run ` + "`app_logs <your-app>`" + ` to confirm structured JSON log lines are appearing.

## Full Standard
Read ` + "`iaf://org/logging-standards`" + ` for the complete machine-readable standard.
`, title, g.library, g.install, g.init, g.request, g.errLog)
}
