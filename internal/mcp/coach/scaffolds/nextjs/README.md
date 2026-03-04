# Next.js + Tailwind CSS Scaffold

A minimal Next.js 14 application with Tailwind CSS, ready to deploy on IAF.

## Structure

```
pages/
  index.js          — Home page
  _app.js           — App wrapper (imports global CSS)
  api/
    health.js       — Health endpoint (returns HTTP 200)
components/
  Layout.js         — Navigation shell (header + footer)
styles/
  globals.css       — Tailwind directives + design tokens
tailwind.config.js  — Colour palette, font, content paths
postcss.config.js   — Tailwind PostCSS plugin
package.json        — Dependencies and start script
```

## Adding Pages

Create a new file under `pages/`:

```js
// pages/about.js
import Layout from '../components/Layout';

export default function About() {
  return (
    <Layout title="About">
      <h1 className="text-2xl font-bold">About</h1>
    </Layout>
  );
}
```

## Extending Design Tokens

Edit `tailwind.config.js` to add colours, fonts, or spacing, and update the
CSS custom properties in `styles/globals.css` to match.

## Running Locally

```bash
npm install
npm run dev   # http://localhost:3000
```

## Deploying on IAF

```
push_code  — upload this file map via the push_code tool
deploy_app — create the Application CR (buildpack auto-detects Node.js)
```

The buildpack runs `npm run build` then `npm start`.
The app listens on `process.env.PORT` (default 8080).
