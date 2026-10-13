import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
const out = new URL('../../internal/platform/assets/', import.meta.url)
// Copy only the UI outputs; NEVER erase browser-request.js used by the Go transport.
mkdirSync(out, { recursive: true })
for (const name of ['index.html', 'assets/app.js', 'assets/style.css']) {
  copyFileSync(
    new URL('../dist/' + name, import.meta.url),
    new URL(name.replace('assets/', ''), out),
  )
}
const docs = new URL('../../docs/', import.meta.url)
copyFileSync(
  new URL('../dist/license.txt', import.meta.url),
  new URL('FRONTEND_LICENSES.txt', docs),
)
const notices = ['GO_THIRD_PARTY_NOTICES.txt', 'SHADCN_LICENSE.txt', 'FRONTEND_LICENSES.txt']
  .map((n) => `\n===== ${n} =====\n${readFileSync(new URL(n, docs), 'utf8')}`)
  .join('\n')
writeFileSync(
  new URL('THIRD_PARTY_NOTICES.txt', docs),
  'Third-party licenses for the Go binary and embedded frontend. External browser is not bundled.\n' +
    notices,
)
