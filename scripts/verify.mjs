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
run('go', ['vet', './cmd/afplatform', './internal/platform'])
run('go', ['test', './cmd/afplatform', './internal/platform', '-count=1'])
run('go', ['test', '-race', './cmd/afplatform', './internal/platform', '-count=1'])
run(process.execPath, [
  resolve(root, 'scripts/run-npm.mjs'),
  '--prefix',
  'frontend',
  'run',
  'build',
])
run('go', ['build', '-trimpath', '-o', 'private/platform-v2/modified.exe', './cmd/afplatform'])
run(process.execPath, [
  resolve(root, 'scripts/verify-platform-v2-probe.mjs'),
  'private/platform-v2/modified.exe',
])
run(process.execPath, [resolve(root, 'scripts/run-npm.mjs'), '--prefix', 'frontend', 'test'])
run(process.execPath, [resolve(root, 'scripts/build.mjs')])
console.log('All local checks passed. No real Google authentication or prediction performed.')
