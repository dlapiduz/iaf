package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type tracingGuide struct {
	autoAvailable bool
	autoPath      string
	library       string
	install       string
	init          string
	customSpan    string
	traceLog      string
}

var tracingGuides = map[string]tracingGuide{
	"go": {
		autoAvailable: false,
		library:       "go.opentelemetry.io/otel",
		install: `go get go.opentelemetry.io/otel \
    go.opentelemetry.io/otel/sdk/trace \
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`,
		init: `import (
    "context"
    "os"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/sdk/trace"
)

func initTracer(ctx context.Context) (func(), error) {
    // Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
    exp, err := otlptracehttp.New(ctx)
    if err != nil {
        return nil, err
    }
    tp := trace.NewTracerProvider(trace.WithBatcher(exp))
    otel.SetTracerProvider(tp)
    return func() { tp.Shutdown(ctx) }, nil
}

// In main():
shutdown, err := initTracer(ctx)
if err != nil { log.Fatal(err) }
defer shutdown()`,
		customSpan: `tracer := otel.Tracer("myapp")
ctx, span := tracer.Start(ctx, "operation-name")
defer span.End()

span.SetAttributes(
    attribute.String("http.method", r.Method),
    attribute.String("http.route",  "/users/:id"),
    attribute.Int("http.status_code", statusCode),
)`,
		traceLog: `import (
    "go.opentelemetry.io/otel/trace"
    "log/slog"
)

// Extract trace and span IDs from the current span context:
spanCtx := trace.SpanFromContext(ctx).SpanContext()
slog.InfoContext(ctx, "request completed",
    "trace_id", spanCtx.TraceID().String(),
    "span_id",  spanCtx.SpanID().String(),
    "method",   r.Method,
    "path",     r.URL.Path,
)`,
	},
	"nodejs": {
		autoAvailable: true,
		autoPath: `# Install:
npm install @opentelemetry/auto-instrumentations-node @opentelemetry/exporter-trace-otlp-http

# Start with auto-instrumentation (zero code changes):
NODE_OPTIONS="--require @opentelemetry/auto-instrumentations-node/register" node app.js

# Or in package.json scripts:
# "start": "node --require @opentelemetry/auto-instrumentations-node/register app.js"

# Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
# When using Cloud Native Buildpacks, set the start command via Procfile:
# web: node --require @opentelemetry/auto-instrumentations-node/register app.js`,
		library: "@opentelemetry/sdk-node",
		install: "npm install @opentelemetry/sdk-node @opentelemetry/exporter-trace-otlp-http",
		init: `// tracing.js — require this before your app:
const { NodeSDK } = require('@opentelemetry/sdk-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-http');

const sdk = new NodeSDK({
  traceExporter: new OTLPTraceExporter(),
  // Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
});
sdk.start();
process.on('SIGTERM', () => sdk.shutdown());`,
		customSpan: `const { trace } = require('@opentelemetry/api');
const tracer = trace.getTracer('myapp');

tracer.startActiveSpan('operation-name', (span) => {
  span.setAttribute('http.method', req.method);
  span.setAttribute('http.route', '/users/:id');
  span.setAttribute('http.status_code', res.statusCode);
  // ... do work ...
  span.end();
});`,
		traceLog: `const { trace } = require('@opentelemetry/api');
const logger = require('pino')(); // from logging-guide

// Extract trace context for log correlation:
const span = trace.getActiveSpan();
const ctx = span ? span.spanContext() : null;
logger.info({
  trace_id: ctx?.traceId,
  span_id:  ctx?.spanId,
  method: req.method,
  path:   req.path,
}, 'request completed');`,
	},
	"python": {
		autoAvailable: true,
		autoPath: `# Install:
pip install opentelemetry-distro opentelemetry-exporter-otlp
opentelemetry-bootstrap -a install

# Start with auto-instrumentation (zero code changes):
opentelemetry-instrument python app.py

# Or in Procfile (Cloud Native Buildpacks):
# web: opentelemetry-instrument python app.py

# Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
# opentelemetry-instrumentation-logging auto-injects trace_id into stdlib logging.`,
		library: "opentelemetry-sdk",
		install: "pip install opentelemetry-sdk opentelemetry-exporter-otlp-proto-http",
		init: `from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

# Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
provider = TracerProvider()
provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(provider)
tracer = trace.get_tracer(__name__)`,
		customSpan: `with tracer.start_as_current_span("operation-name") as span:
    span.set_attribute("http.method", request.method)
    span.set_attribute("http.route",  "/users/<id>")
    span.set_attribute("http.status_code", response.status_code)
    # ... do work ...`,
		traceLog: `from opentelemetry import trace as otel_trace
import logging

# Get current span context for log correlation:
span = otel_trace.get_current_span()
ctx  = span.get_span_context()
logger.info('request completed', extra={
    'trace_id': format(ctx.trace_id, '032x') if ctx.is_valid else None,
    'span_id':  format(ctx.span_id, '016x') if ctx.is_valid else None,
    'method': request.method,
    'path':   request.path,
})`,
	},
	"java": {
		autoAvailable: true,
		autoPath: `# Download the OpenTelemetry Java agent:
curl -L -o opentelemetry-javaagent.jar \
  https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/latest/download/opentelemetry-javaagent.jar

# Add to JVM startup flags (e.g. in Dockerfile or Procfile):
java -javaagent:/app/opentelemetry-javaagent.jar -jar myapp.jar

# Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
# Auto-injects trace_id and span_id into Logback MDC for log correlation.`,
		library: "io.opentelemetry:opentelemetry-sdk",
		install: `<!-- pom.xml -->
<dependency>
  <groupId>io.opentelemetry</groupId>
  <artifactId>opentelemetry-sdk</artifactId>
  <version>1.32.0</version>
</dependency>
<dependency>
  <groupId>io.opentelemetry</groupId>
  <artifactId>opentelemetry-exporter-otlp</artifactId>
  <version>1.32.0</version>
</dependency>`,
		init: `import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.sdk.OpenTelemetrySdk;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.export.BatchSpanProcessor;
import io.opentelemetry.exporter.otlp.trace.OtlpGrpcSpanExporter;

// Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically:
SdkTracerProvider tracerProvider = SdkTracerProvider.builder()
    .addSpanProcessor(BatchSpanProcessor.builder(OtlpGrpcSpanExporter.getDefault()).build())
    .build();
OpenTelemetrySdk.builder().setTracerProvider(tracerProvider).buildAndRegisterGlobal();`,
		customSpan: `import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.api.trace.Span;

Tracer tracer = GlobalOpenTelemetry.getTracer("myapp");
Span span = tracer.spanBuilder("operation-name").startSpan();
try (var scope = span.makeCurrent()) {
    span.setAttribute("http.method", method);
    span.setAttribute("http.route",  "/users/:id");
    span.setAttribute("http.status_code", statusCode);
    // ... do work ...
} finally {
    span.end();
}`,
		traceLog: `import io.opentelemetry.api.trace.Span;
// When using javaagent, trace_id and span_id are auto-injected into Logback MDC.
// For manual extraction:
Span span = Span.current();
var ctx  = span.getSpanContext();
logger.info("request completed",
    StructuredArguments.kv("trace_id", ctx.getTraceId()),
    StructuredArguments.kv("span_id",  ctx.getSpanId()));`,
	},
	"ruby": {
		autoAvailable: true,
		autoPath: `# Gemfile:
gem 'opentelemetry-sdk'
gem 'opentelemetry-instrumentation-all'
gem 'opentelemetry-exporter-otlp'

# config/initializers/opentelemetry.rb (Rails) or at startup:
require 'opentelemetry/sdk'
require 'opentelemetry/instrumentation/all'
require 'opentelemetry-exporter-otlp'

OpenTelemetry::SDK.configure do |c|
  c.use_all  # auto-instrument HTTP, Redis, ActiveRecord, etc.
end
# Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.`,
		library: "opentelemetry-sdk",
		install: "gem 'opentelemetry-sdk', 'opentelemetry-exporter-otlp'  # Gemfile\nbundle install",
		init: `require 'opentelemetry/sdk'
require 'opentelemetry-exporter-otlp'

OpenTelemetry::SDK.configure do |c|
  c.add_span_processor(
    OpenTelemetry::SDK::Trace::Export::BatchSpanProcessor.new(
      OpenTelemetry::Exporter::OTLP::Exporter.new
      # Reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME automatically.
    )
  )
end
tracer = OpenTelemetry.tracer_provider.tracer('myapp')`,
		customSpan: `tracer.in_span('operation-name') do |span|
  span.set_attribute('http.method', request.request_method)
  span.set_attribute('http.route',  '/users/:id')
  span.set_attribute('http.status_code', response.status)
  # ... do work ...
end`,
		traceLog: `span = OpenTelemetry::Trace.current_span
ctx  = span.context
trace_id = ctx.trace_id.unpack1('H*')
span_id  = ctx.span_id.unpack1('H*')

logger.info 'request completed',
  trace_id: trace_id,
  span_id:  span_id,
  method: request.request_method,
  path:   request.path`,
	},
}

// RegisterTracingGuide registers the tracing-guide prompt that provides
// per-language OpenTelemetry setup for IAF applications.
func RegisterTracingGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "tracing-guide",
		Description: "Per-language OpenTelemetry tracing setup: auto-instrumentation, manual SDK init, custom spans, and trace-log correlation.",
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
			text = tracingGuideOverview()
		} else {
			canonical, ok := languageAliases[lang]
			if !ok {
				return nil, fmt.Errorf("unsupported language %q; supported languages: go, nodejs, python, java, ruby", lang)
			}
			guide := tracingGuides[canonical]
			text = renderTracingGuide(canonical, guide)
		}

		return &gomcp.GetPromptResult{
			Description: "OpenTelemetry tracing guide for IAF applications.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}

func tracingGuideOverview() string {
	return `# IAF Tracing Guide

## The Standard: OpenTelemetry + OTLP
Use the OpenTelemetry SDK. Point it to the platform Collector via env vars. Log trace IDs.

The platform automatically injects:
- ` + "`OTEL_EXPORTER_OTLP_ENDPOINT`" + ` — OTel Collector endpoint (never hardcode this)
- ` + "`OTEL_SERVICE_NAME`" + ` — set to your IAF application name

## Auto-Instrumentation (preferred)
Most languages have zero-code OTel instrumentation — no code changes required:
- **Node.js**: ` + "`--require @opentelemetry/auto-instrumentations-node/register`" + `
- **Python**: ` + "`opentelemetry-instrument python app.py`" + `
- **Java**: ` + "`-javaagent:opentelemetry-javaagent.jar`" + `
- **Ruby**: ` + "`opentelemetry-instrumentation-all`" + ` gem + ` + "`c.use_all`" + `
- **Go**: Manual SDK required (no auto-instrumentation available)

## Trace-Log Correlation
Add ` + "`trace_id`" + ` and ` + "`span_id`" + ` to every log line to enable Grafana trace-to-log jumps. See ` + "`logging-guide`" + ` for the structured logging standard.

## Security
**Never add secrets, tokens, or PII as span attributes.** Traces are stored in Tempo and queryable by platform operators.

## Full Standards Reference
Read ` + "`iaf://org/tracing-standards`" + ` for the machine-readable standard.

## Language-Specific Setup
Run ` + "`tracing-guide`" + ` with a ` + "`language`" + ` argument:
- ` + "`tracing-guide language=go`" + `      — manual OTel SDK
- ` + "`tracing-guide language=nodejs`" + `  — auto-instrumentation + pino correlation
- ` + "`tracing-guide language=python`" + `  — opentelemetry-instrument CLI
- ` + "`tracing-guide language=java`" + `    — javaagent + Logback MDC
- ` + "`tracing-guide language=ruby`" + `    — opentelemetry-instrumentation-all
`
}

func renderTracingGuide(lang string, g tracingGuide) string {
	title := strings.ToUpper(lang[:1]) + lang[1:]

	var autoSection string
	if g.autoAvailable {
		autoSection = fmt.Sprintf(`## Auto-Instrumentation (Recommended — Zero Code Changes)
`+"```"+`
%s
`+"```"+`

`, g.autoPath)
	} else {
		autoSection = "## Auto-Instrumentation\nNot available for Go — use the manual SDK path below.\n\n"
	}

	return fmt.Sprintf(`# %s Tracing Guide for IAF

%s## Manual SDK Setup

### Library
%s

### Installation
`+"```"+`
%s
`+"```"+`

### SDK Initialization
`+"```"+`
%s
`+"```"+`

### Create a Custom Span
`+"```"+`
%s
`+"```"+`

## Trace-Log Correlation
Add ` + "`trace_id`" + ` and ` + "`span_id`" + ` to every structured log line (enables Grafana trace-to-log jumps):
`+"```"+`
%s
`+"```"+`

## Rules
- **Do not hardcode ` + "`OTEL_EXPORTER_OTLP_ENDPOINT`" + `.** The platform injects it automatically.
- **Never add secrets, tokens, or PII as span attributes.** Traces are stored in Tempo and queryable by platform operators.
- Review what your OTel SDK auto-instruments — HTTP headers and query parameters can contain sensitive data. Disable unwanted capture with ` + "`OTEL_INSTRUMENTATION_HTTP_CAPTURE_HEADERS_SERVER_REQUEST=false`" + ` if needed.

## Verify
After deploying, call ` + "`app_status`" + ` and check the ` + "`traceExploreUrl`" + ` field (present when the platform's Tempo integration is configured) to verify traces are flowing.

## Full Standard
Read ` + "`iaf://org/tracing-standards`" + ` for the complete machine-readable standard.
`, title, autoSection, g.library, g.install, g.init, g.customSpan, g.traceLog)
}
