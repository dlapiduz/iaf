package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type metricsGuide struct {
	library string
	install string
	expose  string
	instrument string
}

var metricsGuides = map[string]metricsGuide{
	"go": {
		library: "github.com/prometheus/client_golang",
		install: "go get github.com/prometheus/client_golang/prometheus github.com/prometheus/client_golang/prometheus/promhttp",
		expose: `import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// Register the /metrics handler alongside your app routes:
http.Handle("/metrics", promhttp.Handler())`,
		instrument: `import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "http_requests_total",
        Help: "Total HTTP requests by method, path, and status code.",
    }, []string{"method", "path", "status_code"})

    httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "http_request_duration_seconds",
        Help:    "HTTP request duration in seconds.",
        Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5},
    }, []string{"method", "path"})
)

// In your HTTP handler middleware:
func metricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        timer := prometheus.NewTimer(httpDuration.WithLabelValues(r.Method, r.URL.Path))
        defer timer.ObserveDuration()
        rw := &responseWriter{w, http.StatusOK}
        next.ServeHTTP(rw, r)
        httpRequests.WithLabelValues(r.Method, r.URL.Path,
            fmt.Sprintf("%d", rw.status)).Inc()
    })
}`,
	},
	"nodejs": {
		library: "prom-client",
		install: "npm install prom-client",
		expose: `const client = require('prom-client');
const express = require('express');

// Collect default Node.js metrics (memory, CPU, event loop lag):
client.collectDefaultMetrics();

// Expose /metrics endpoint:
app.get('/metrics', async (req, res) => {
  res.set('Content-Type', client.register.contentType);
  res.end(await client.register.metrics());
});`,
		instrument: `const httpRequestsTotal = new client.Counter({
  name: 'http_requests_total',
  help: 'Total HTTP requests by method, path, and status code.',
  labelNames: ['method', 'path', 'status_code'],
});

const httpDuration = new client.Histogram({
  name: 'http_request_duration_seconds',
  help: 'HTTP request duration in seconds.',
  labelNames: ['method', 'path'],
  buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5],
});

// Express middleware:
app.use((req, res, next) => {
  const end = httpDuration.startTimer({ method: req.method, path: req.route?.path || req.path });
  res.on('finish', () => {
    end();
    httpRequestsTotal.inc({ method: req.method, path: req.route?.path || req.path, status_code: res.statusCode });
  });
  next();
});`,
	},
	"python": {
		library: "prometheus_client",
		install: "pip install prometheus-client",
		expose: `from prometheus_client import make_wsgi_app, REGISTRY
from werkzeug.middleware.dispatcher import DispatcherMiddleware

# Mount /metrics alongside your Flask/FastAPI app:
app.wsgi_app = DispatcherMiddleware(app.wsgi_app, {
    '/metrics': make_wsgi_app()
})`,
		instrument: `from prometheus_client import Counter, Histogram
import time

http_requests_total = Counter(
    'http_requests_total',
    'Total HTTP requests by method, path, and status code.',
    ['method', 'path', 'status_code'],
)
http_request_duration = Histogram(
    'http_request_duration_seconds',
    'HTTP request duration in seconds.',
    ['method', 'path'],
    buckets=[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5],
)

# Flask before/after request hooks:
@app.before_request
def start_timer():
    g.start_time = time.monotonic()

@app.after_request
def record_metrics(response):
    duration = time.monotonic() - g.start_time
    http_request_duration.labels(request.method, request.path).observe(duration)
    http_requests_total.labels(request.method, request.path, response.status_code).inc()
    return response`,
	},
	"java": {
		library: "io.micrometer:micrometer-registry-prometheus (Spring Boot Actuator)",
		install: `<!-- pom.xml -->
<dependency>
  <groupId>org.springframework.boot</groupId>
  <artifactId>spring-boot-starter-actuator</artifactId>
</dependency>
<dependency>
  <groupId>io.micrometer</groupId>
  <artifactId>micrometer-registry-prometheus</artifactId>
</dependency>`,
		expose: `# application.properties
management.endpoints.web.exposure.include=health,prometheus
management.endpoint.prometheus.enabled=true
# /actuator/prometheus serves Prometheus text format automatically`,
		instrument: `import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import org.springframework.web.filter.OncePerRequestFilter;

// Spring Boot auto-instruments HTTP requests via actuator.
// For custom metrics:
@Autowired MeterRegistry registry;

Counter.builder("http_requests_total")
    .tag("method", method).tag("path", path).tag("status_code", statusCode)
    .register(registry).increment();

Timer.builder("http_request_duration_seconds")
    .tag("method", method).tag("path", path)
    .publishPercentileHistogram()
    .register(registry).record(duration, TimeUnit.MILLISECONDS);`,
	},
	"ruby": {
		library: "prometheus-client",
		install: "gem 'prometheus-client'  # Gemfile\nbundle install",
		expose: `require 'prometheus/middleware/collector'
require 'prometheus/middleware/exporter'

# Rack middleware (config.ru or Rails config):
use Prometheus::Middleware::Collector
use Prometheus::Middleware::Exporter  # serves /metrics`,
		instrument: `require 'prometheus/client'

prometheus = Prometheus::Client.registry

http_requests = prometheus.counter(
  :http_requests_total,
  docstring: 'Total HTTP requests by method, path, and status code.',
  labels: [:method, :path, :status_code],
)
http_duration = prometheus.histogram(
  :http_request_duration_seconds,
  docstring: 'HTTP request duration in seconds.',
  labels: [:method, :path],
  buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5],
)

# In your request handler:
start = Process.clock_gettime(Process::CLOCK_MONOTONIC)
# ... handle request ...
duration = Process.clock_gettime(Process::CLOCK_MONOTONIC) - start
http_duration.observe(duration, labels: { method: method, path: path })
http_requests.increment(labels: { method: method, path: path, status_code: status_code })`,
	},
}

// RegisterMetricsGuide registers the metrics-guide prompt that provides
// per-language Prometheus instrumentation setup for IAF applications.
func RegisterMetricsGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "metrics-guide",
		Description: "Per-language Prometheus metrics setup: RED method (http_requests_total, http_request_duration_seconds), /metrics endpoint, and verification.",
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
			text = metricsGuideOverview()
		} else {
			canonical, ok := languageAliases[lang]
			if !ok {
				return nil, fmt.Errorf("unsupported language %q; supported languages: go, nodejs, python, java, ruby", lang)
			}
			guide := metricsGuides[canonical]
			text = renderMetricsGuide(canonical, guide)
		}

		return &gomcp.GetPromptResult{
			Description: "Prometheus metrics guide for IAF applications.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}

func metricsGuideOverview() string {
	return `# IAF Metrics Guide

## The Standard: RED Method
Every HTTP service must expose these two metrics at ` + "`/metrics`" + ` in Prometheus text format:

| Metric | Type | Labels |
|--------|------|--------|
| ` + "`http_requests_total`" + ` | counter | method, path, status_code |
| ` + "`http_request_duration_seconds`" + ` | histogram | method, path |

**Histogram buckets:** ` + "`[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5]`" + `

## Standard Labels
The IAF controller sets ` + "`app.kubernetes.io/name`" + ` and ` + "`app.kubernetes.io/version`" + ` on every Deployment. Prometheus automatically adds these as ` + "`app`" + ` and ` + "`version`" + ` labels to all scraped metrics — you don't need to set them in your code.

## Relationship to Logging
Request logs complement RED metrics — log every request AND emit ` + "`http_request_duration_seconds`" + `. Metrics answer "how many and how slow?"; logs answer "which specific request failed?". Use both. See ` + "`logging-guide`" + `.

## Full Standards Reference
Read ` + "`iaf://org/metrics-standards`" + ` for the machine-readable standard.

## Language-Specific Setup
Run ` + "`metrics-guide`" + ` with a ` + "`language`" + ` argument for copy-paste-ready code:
- ` + "`metrics-guide language=go`" + `      — prometheus/client_golang
- ` + "`metrics-guide language=nodejs`" + `  — prom-client
- ` + "`metrics-guide language=python`" + `  — prometheus_client
- ` + "`metrics-guide language=java`" + `    — micrometer-registry-prometheus
- ` + "`metrics-guide language=ruby`" + `    — prometheus-client
`
}

func renderMetricsGuide(lang string, g metricsGuide) string {
	title := strings.ToUpper(lang[:1]) + lang[1:]
	return fmt.Sprintf(`# %s Metrics Guide for IAF

## Library
%s

## Installation
` + "```" + `
%s
` + "```" + `

## Expose /metrics Endpoint
` + "```" + `
%s
` + "```" + `

## Instrument HTTP Requests (RED method)
` + "```" + `
%s
` + "```" + `

## Standard Labels
The IAF controller sets ` + "`app.kubernetes.io/name`" + ` and ` + "`app.kubernetes.io/version`" + ` on your Deployment automatically. You do not need to set these in your metrics code.

## Relationship to Logging
Request logs complement RED metrics — log every request AND emit ` + "`http_request_duration_seconds`" + `. See ` + "`logging-guide`" + ` for the structured logging standard.

## Verify
After deploying, curl your app's ` + "`/metrics`" + ` endpoint and confirm Prometheus text format output:
` + "```" + `
curl http://<your-app>/metrics | grep http_requests_total
` + "```" + `

## Full Standard
Read ` + "`iaf://org/metrics-standards`" + ` for the complete machine-readable standard.
`, title, g.library, g.install, g.expose, g.instrument)
}
