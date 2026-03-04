package coach

import (
	"context"
	"fmt"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterScaffoldGuide registers the scaffold-guide prompt that explains how
// to fetch a UI scaffold and deploy it on IAF.
func RegisterScaffoldGuide(server *gomcp.Server, deps *Dependencies) {
	server.AddPrompt(&gomcp.Prompt{
		Name:        "scaffold-guide",
		Description: "How to fetch a UI scaffold (nextjs or html) and deploy it on IAF. Covers fetching the file map, customising it, and deploying via push_code.",
		Arguments: []*gomcp.PromptArgument{
			{
				Name:        "framework",
				Description: "Optional scaffold framework (nextjs or html). Omit to see guidance for both.",
				Required:    false,
			},
		},
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		framework := strings.ToLower(strings.TrimSpace(req.Params.Arguments["framework"]))

		var sb strings.Builder
		sb.WriteString("# UI Scaffold Guide for IAF\n\n" +
			"## When to Use a Scaffold\n" +
			"Start from a scaffold when you need a complete, deployment-ready UI with:\n" +
			"- A navigation shell (header + footer)\n" +
			"- A health endpoint at `/health` (required by IAF for readiness probes)\n" +
			"- Tailwind CSS styling with org design tokens\n" +
			"- A `package.json` with a `start` script the buildpack can detect\n\n" +
			"## Available Frameworks\n\n")

		switch framework {
		case "nextjs":
			sb.WriteString(nextjsGuide(deps.BaseDomain))
		case "html":
			sb.WriteString(htmlGuide(deps.BaseDomain))
		default:
			// Show both when no framework is specified or unknown value provided.
			sb.WriteString(nextjsGuide(deps.BaseDomain))
			sb.WriteString("\n---\n\n")
			sb.WriteString(htmlGuide(deps.BaseDomain))
		}

		sb.WriteString("\n## Deploying on IAF\n\n" +
			"After customising the scaffold:\n\n" +
			"```\n" +
			"1. push_code  session_id=<your-session> name=<app-name> files=<scaffold-file-map>\n" +
			"2. deploy_app session_id=<your-session> name=<app-name>\n" +
			"3. app_status session_id=<your-session> name=<app-name>  # wait for Running\n" +
			"```\n\n")
		sb.WriteString(fmt.Sprintf("Your app will be available at `http://<app-name>.%s` once Running.\n", deps.BaseDomain))

		return &gomcp.GetPromptResult{
			Description: "UI scaffold deployment guide for IAF.",
			Messages: []*gomcp.PromptMessage{
				{
					Role:    "user",
					Content: &gomcp.TextContent{Text: sb.String()},
				},
			},
		}, nil
	})
}

func nextjsGuide(baseDomain string) string {
	return `### Next.js + Tailwind CSS (` + "`nextjs`" + `)

**When to choose**: Full-featured React app, server-side rendering, multiple pages, PostCSS build step.

**Fetch the scaffold**:
` + "```" + `
ReadResource: iaf://scaffold/nextjs
` + "```" + `

Returns a file map with:
- ` + "`package.json`" + ` — Next.js 14, React 18, Tailwind CSS
- ` + "`pages/index.js`" + ` — Home page using the Layout component
- ` + "`pages/_app.js`" + ` — App wrapper (imports global CSS)
- ` + "`pages/api/health.js`" + ` — Health endpoint (` + "`GET /api/health`" + ` → HTTP 200)
- ` + "`components/Layout.js`" + ` — Navigation shell
- ` + "`styles/globals.css`" + ` — Tailwind directives + org design tokens
- ` + "`tailwind.config.js`" + `, ` + "`postcss.config.js`" + `
- ` + "`README.md`" + `

**Adding a page**:
` + "```js" + `
// pages/about.js
import Layout from '../components/Layout';
export default function About() {
  return <Layout title="About"><h1>About</h1></Layout>;
}
` + "```" + `

**Build behaviour**: The Node.js buildpack runs ` + "`npm run build`" + ` then ` + "`npm start`" + `.
The app listens on ` + "`process.env.PORT`" + ` (default 8080).
`
}

func htmlGuide(baseDomain string) string {
	return `### Plain HTML + Tailwind CDN (` + "`html`" + `)

**When to choose**: Simple static site, no build step, minimal complexity.

**Fetch the scaffold**:
` + "```" + `
ReadResource: iaf://scaffold/html
` + "```" + `

Returns a file map with:
- ` + "`package.json`" + ` — express dependency, ` + "`start`" + ` script
- ` + "`server.js`" + ` — Express: serves ` + "`public/`" + ` + explicit ` + "`GET /health`" + ` → 200
- ` + "`public/index.html`" + ` — HTML5 with Tailwind CDN, navigation shell, org tokens
- ` + "`public/styles.css`" + ` — org design tokens
- ` + "`README.md`" + `

**Adding a page**:
Create ` + "`public/about.html`" + ` and link to it from the navigation in ` + "`index.html`" + `.

**Build behaviour**: The Node.js buildpack runs ` + "`npm install`" + ` then ` + "`npm start`" + `.
No build step — Tailwind CSS is loaded from CDN.
The app listens on ` + "`process.env.PORT`" + ` (default 8080).
`
}
