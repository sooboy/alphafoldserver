import { spawnSync } from 'node:child_process'
import { mkdirSync, copyFileSync, readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
const root = fileURLToPath(new URL('../', import.meta.url))
function run(command, args, env = process.env) {
  const r = spawnSync(command, args, { cwd: root, env, stdio: 'inherit', windowsHide: true })
  if (r.error) throw r.error
  if (r.status !== 0) throw Error(`${command} failed (${r.status})`)
}
if (process.argv.includes('--frontend')) {
  run(process.execPath, [
    fileURLToPath(new URL('./run-npm.mjs', import.meta.url)),
    '--prefix',
    'frontend',
    'run',
    'build',
  ])
}
mkdirSync(resolve(root, 'dist'), { recursive: true })
const artifacts = []
for (const [os, arch, name] of [
  ['windows', 'amd64', 'afplatform-windows-amd64-v2.exe'],
  ['linux', 'amd64', 'afplatform-linux-amd64'],
  ['linux', 'arm64', 'afplatform-linux-arm64'],
]) {
  run('go', ['build', '-trimpath', '-o', `dist/${name}`, './cmd/afplatform'], {
    ...process.env,
    CGO_ENABLED: '0',
    GOOS: os,
    GOARCH: arch,
  })
  const b = readFileSync(resolve(root, 'dist', name))
  artifacts.push({
    file: name,
    os,
    arch,
    bytes: b.length,
    sha256: createHash('sha256').update(b).digest('hex'),
  })
}
copyFileSync(
  resolve(root, 'docs/THIRD_PARTY_NOTICES.txt'),
  resolve(root, 'dist/THIRD_PARTY_NOTICES.txt'),
)
writeFileSync(
  resolve(root, 'dist/build-manifest-v2.json'),
  JSON.stringify({ artifacts, linux_runtime_tested: false }, null, 2) + '\n',
)
console.log(JSON.stringify(artifacts, null, 2))
