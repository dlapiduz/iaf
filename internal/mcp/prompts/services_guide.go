package prompts

import (
	"context"

	"github.com/dlapiduz/iaf/internal/mcp/tools"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterServicesGuide registers the services-guide prompt that explains
// how to provision and bind managed backing services.
func RegisterServicesGuide(server *gomcp.Server, deps *tools.Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "services-guide",
		Description: "Complete guide for provisioning and using managed backing services (e.g. PostgreSQL) on IAF. Covers the full lifecycle: provision → poll → bind → use → unbind → deprovision.",
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		text := `# IAF Managed Services Guide

## Overview

IAF provides managed backing services — pre-provisioned, platform-managed resources (such as PostgreSQL databases) that run alongside your applications in your session namespace. This guide explains the complete lifecycle.

**IMPORTANT**: Do NOT deploy a database as an Application (e.g. using a postgres Docker image). Use ` + "`provision_service`" + ` instead — it provisions a properly managed, isolated, and backed-up database via CloudNativePG.

## Supported Services

| Type | Plans | Description |
|------|-------|-------------|
| ` + "`postgres`" + ` | ` + "`micro`" + `, ` + "`small`" + `, ` + "`ha`" + ` | PostgreSQL via CloudNativePG |

### Plans

| Plan | Instances | Memory | Storage | Use case |
|------|-----------|--------|---------|----------|
| ` + "`micro`" + ` | 1 | 256Mi | 1Gi | Development / ephemeral |
| ` + "`small`" + ` | 1 | 512Mi | 5Gi | Light production workloads |
| ` + "`ha`" + ` | 3 | 1Gi | 10Gi | High-availability production |

## Complete Workflow

### Step 1: Provision the service

` + "```" + `
provision_service(
  session_id="<your-session-id>",
  name="mydb",
  type="postgres",
  plan="micro"
)
` + "```" + `

Returns immediately. Provisioning is asynchronous.

### Step 2: Poll until Ready (every 10 seconds)

` + "```" + `
service_status(session_id="<your-session-id>", name="mydb")
` + "```" + `

When ` + "`phase`" + ` is ` + "`Ready`" + `, the response also includes ` + "`connectionEnvVars`" + `:
` + "```json" + `
{
  "phase": "Ready",
  "connectionEnvVars": ["DATABASE_URL", "PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD"]
}
` + "```" + `

**IMPORTANT**: Credential values are never returned by any tool. They are stored as Kubernetes Secrets and injected directly into application containers. This is by design — agents cannot exfiltrate credentials.

### Step 3: Deploy or identify your application

Ensure your application exists (use ` + "`deploy-guide`" + ` if needed).

### Step 4: Bind the service to your application

` + "```" + `
bind_service(
  session_id="<your-session-id>",
  service_name="mydb",
  app_name="myapp"
)
` + "```" + `

This injects 6 environment variables as Kubernetes Secret references into your application:
- ` + "`DATABASE_URL`" + ` — Full connection string (recommended for most ORMs/frameworks)
- ` + "`PGHOST`" + ` — PostgreSQL host
- ` + "`PGPORT`" + ` — PostgreSQL port
- ` + "`PGDATABASE`" + ` — Database name
- ` + "`PGUSER`" + ` — Database user
- ` + "`PGPASSWORD`" + ` — Database password

The application is automatically redeployed with the new credentials.

### Step 5: Use the connection in your code

Your application code reads these as standard environment variables:

**Python (psycopg2 / SQLAlchemy)**
` + "```python" + `
import os
DATABASE_URL = os.environ["DATABASE_URL"]
` + "```" + `

**Go**
` + "```go" + `
dsn := os.Getenv("DATABASE_URL")
` + "```" + `

**Node.js**
` + "```javascript" + `
const connectionString = process.env.DATABASE_URL;
` + "```" + `

### Step 6 (cleanup): Unbind the service

` + "```" + `
unbind_service(
  session_id="<your-session-id>",
  service_name="mydb",
  app_name="myapp"
)
` + "```" + `

Removes the 6 injected environment variables from the application. Does NOT delete credentials or data.

### Step 7 (cleanup): Deprovision the service

` + "```" + `
deprovision_service(session_id="<your-session-id>", name="mydb")
` + "```" + `

**WARNING**: Permanently deletes the database and all its data. You must unbind all applications first.

## Listing Services

` + "```" + `
list_services(session_id="<your-session-id>")
` + "```" + `

Returns all managed services in your namespace with their current phase and bound applications.

## Security Notes

- Credentials are stored as Kubernetes Secrets in your isolated namespace.
- Tools NEVER return credential values — only environment variable names.
- A service with bound applications cannot be deleted (controller-enforced).
- Network access to the database is restricted to pods within your namespace.

## Polling Guidance

The ` + "`provision_service`" + ` tool returns immediately. Use ` + "`service_status`" + ` every 10 seconds to check progress. Provisioning typically takes 1–3 minutes for the ` + "`micro`" + ` plan.

## Cross-references

- See ` + "`deploy-guide`" + ` for deploying applications and understanding the application lifecycle.
- See ` + "`language-guide`" + ` for buildpack requirements for your target language.
`

		return &gomcp.GetPromptResult{
			Description: "Complete guide for provisioning and using managed backing services on IAF.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: text},
				},
			},
		}, nil
	})
}
