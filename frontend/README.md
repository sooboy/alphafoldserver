# Frontend

React + TypeScript + Vite + Tailwind v4. `src/components/ui/` is the actual shadcn/ui radix-nova registry output (CLI 4.21.4), not a CSS imitation. `components.json` records aliases and registry style.

```sh
npm ci
npm run build
npm test
```

Build copies only index.html/app.js/style.css into the Go embed directory, leaving `browser-request.js` intact. No CDN, remote fonts or Node runtime is required by the deployed service. Committed generated assets allow Go-only builds; rebuild after UI changes.

Registry CSS is vendored in `src/styles/shadcn.css` (MIT, `docs/SHADCN_LICENSE.txt`). The scaffolding CLI and unused font package were removed from runtime dependencies after generation. `npm audit` should also be run when updating the lockfile. `npm run build` generates bundled-package license notices with Vite and combines them with Go and shadcn notices.

Playwright tests use an isolated service/data directory and installed Edge by default. Set `AF_TEST_BROWSER_PATH` to another installed Chromium executable. Run `make ui-test` (Windows: `mingw32-make ui-test`) from the repository root to build the native test binary first. Tests never create Google accounts or remote predictions.
