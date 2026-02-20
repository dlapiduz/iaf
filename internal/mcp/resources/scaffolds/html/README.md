# HTML + Tailwind CDN Scaffold

A minimal static HTML application served by Express, with Tailwind CSS loaded
from CDN. No build step required — ready to deploy on IAF.

## Structure

```
server.js           — Express server (static files + /health endpoint)
public/
  index.html        — HTML5 page with Tailwind CDN and navigation shell
  styles.css        — Org design tokens and utility classes
package.json        — express dependency and start script
```

## Adding Pages

Create additional `.html` files under `public/`. Link to them from the
navigation in `index.html`.

## Customising Design Tokens

Edit the `:root` block in `public/styles.css` to change colours and other
design tokens. Update the `tailwind.config` block in `index.html` to keep
Tailwind utility classes in sync.

## Migrating to a Build Step

When you need PurgeCSS, custom fonts, or a JS framework, switch to the
`nextjs` scaffold which includes a full Tailwind build pipeline.

## Running Locally

```bash
npm install
npm start   # http://localhost:8080
```

## Deploying on IAF

```
push_code  — upload this file map via the push_code tool
deploy_app — create the Application CR (buildpack auto-detects Node.js)
```

The buildpack installs dependencies and runs `npm start`.
