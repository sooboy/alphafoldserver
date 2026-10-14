import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { mkdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
const root = fileURLToPath(new URL('../', import.meta.url))
const records = []
function run(command, args, env = process.env) {
  const r = spawnSync(command, args, {
    cwd: root,
    env,
    encoding: 'utf8',
    maxBuffer: 16 * 1024 * 1024,
    windowsHide: true,
  })
  records.push({
    command: [command, ...args],
    exit: r.status,
    stdout: r.stdout || '',
    stderr: r.stderr || '',
    error: r.error?.message,
  })
  process.stdout.write(r.stdout || '')
  process.stderr.write(r.stderr || '')
  mkdirSync(resolve(root, 'private/platform-v2'), { recursive: true })
  writeFileSync(
    resolve(root, 'private/platform-v2/checks.json'),
    JSON.stringify(records, null, 2) + '\n',
  )
  if (r.error || r.status !== 0)
    throw Error(`${command} failed (${r.status}): ${r.error?.message || ''}`)
}
run('go', ['mod', 'verify'])
run('go', ['vet', './cmd/...', './internal/...', './tools/...'])
run('go', ['test', './cmd/...', './internal/...', './tools/...', '-count=1'])
run('go', ['test', '-race', './cmd/...', './internal/...', './tools/...', '-count=1'])
run('go', ['run', './tools/dev', 'frontend'])
const binary = 'private/platform-v2/modified' + (process.platform === 'win32' ? '.exe' : '')
run('go', ['build', '-trimpath', '-buildvcs=false', '-o', binary, './cmd/afplatform'])
run(process.execPath, [resolve(root, 'scripts/verify-platform-v2-probe.mjs'), binary])
run('go', ['run', './tools/dev', 'npm', 'test'])
run('go', ['run', './tools/dev', 'release'])
console.log('All local checks passed. No real Google authentication or prediction performed.')
