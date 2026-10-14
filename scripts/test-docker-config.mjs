import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync, chmodSync, existsSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { createHash } from 'node:crypto'

const root = fileURLToPath(new URL('../', import.meta.url))
const bash = process.env.BASH_EXE || (process.platform === 'win32' ? 'C:/Program Files/Git/bin/bash.exe' : 'bash')
const work = mkdtempSync(join(tmpdir(), 'af-docker-config-'))
const env = { ...process.env }
for (const key of Object.keys(env)) if (key.startsWith('AF_')) delete env[key]
const config = resolve(root, 'deploy/docker/config.sh').replaceAll('\\', '/')
const records = []
let count = 0
function run(command, args, extra = {}, expected = 0) {
  const r = spawnSync(command, args, { cwd: root, env: { ...env, ...extra }, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024, windowsHide: true })
  records.push({ command: [command, ...args], exit: r.status, stdout: r.stdout || '', stderr: r.stderr || '', error: r.error?.message })
  if (process.env.DOCKER_TEST_RECORDS) writeFileSync(process.env.DOCKER_TEST_RECORDS, JSON.stringify(records, null, 2) + '\n')
  assert.ifError(r.error)
  assert.equal(r.status, expected, r.stderr)
  return r.stdout
}
function test(name, fn) { fn(); console.log('PASS ' + name); count++ }
function shell(script, extra = {}, expected = 0, args = []) {
  return run(bash, ['-c', 'source "$AF_CONFIG_SH"; ' + script, 'test', ...args], { AF_CONFIG_SH: config, ...extra }, expected)
}
test('Bash syntax: entrypoint, config, healthcheck', () => {
  for (const f of ['entrypoint.sh', 'config.sh', 'healthcheck.sh']) run(bash, ['-n', resolve(root, 'deploy/docker', f)])
})
test('environment defaults map to exact native Go flags', () => {
  const args = shell('build_platform_args; printf "%s\\0" "${PLATFORM_ARGS[@]}"').split('\0').slice(0, -1)
  assert.deepEqual(args, ['serve', '-data', '/data', '-listen', '127.0.0.1:8788', '-public-url', 'http://127.0.0.1:8788', '-browser', '/usr/bin/chromium', '-max-active-browsers', '3', '-browser-idle', '2m'])
})
test('explicit flags follow environment; spaces/metacharacters stay one literal argument', () => {
  const value = '/data/a b; echo NOT_EXECUTED'
  const args = shell('build_platform_args -max-active-browsers 4; printf "%s\\0" "${PLATFORM_ARGS[@]}"', { AF_DATA: value, AF_MAX_BROWSERS: '2', AF_TLS_CERT: '/certs/cert file.pem', AF_TLS_KEY: '/certs/key.pem' }).split('\0').slice(0, -1)
  assert.equal(args[2], value)
  assert.deepEqual(args.slice(-6), ['-tls-cert', '/certs/cert file.pem', '-tls-key', '/certs/key.pem', '-max-active-browsers', '4'])
})
test('desktop ports reject privileged, overflow and non-numeric values', () => {
  for (const p of ['1024', '6080', '65535']) shell('validate_desktop_port "$1"', {}, 0, [p])
  for (const p of ['0', '1000', '1023', '65536', '999999999999', 'abc', '01', '6080;echo BAD']) shell('validate_desktop_port "$1"', {}, 1, [p])
})
test('desktop password created privately once, never printed or replaced', () => {
  const p = join(work, 'desktop-password').replaceAll('\\', '/')
  assert.equal(shell('init_desktop_password "$1"', {}, 0, [p]), '')
  const before = readFileSync(p, 'utf8')
  assert.match(before, /^[a-f0-9]{8}\r?\n$/) // Git-for-Windows OpenSSL emits CRLF.
  assert.equal(shell('init_desktop_password "$1"', {}, 0, [p]), '')
  assert.equal(readFileSync(p, 'utf8'), before)
  if (process.platform !== 'win32') shell('test "$(stat -c %a "$1")" = 600', {}, 0, [p])
})
test('supplied missing/invalid passwords fail, no default fallback', () => {
  const missing = join(work, 'missing-secret').replaceAll('\\', '/')
  shell('init_desktop_password "$1"', { AF_VNC_PASSWORD_FILE: missing }, 2, [missing])
  assert.equal(existsSync(missing), false)
  const p = join(work, 'invalid-password').replaceAll('\\', '/')
  writeFileSync(p, 'longer-than-eight\n')
  shell('init_desktop_password "$1"', {}, 2, [p])
  assert.equal(readFileSync(p, 'utf8'), 'longer-than-eight\n')
})
test('healthcheck preserves public Host and bypasses ambient proxy', () => {
  const bin = join(work, 'bin'); mkdirSync(bin)
  const curl = join(bin, 'curl')
  writeFileSync(curl, '#!/bin/bash\nprintf "%s\\0" "$@"\n'); chmodSync(curl, 0o755)
  const health = resolve(root, 'deploy/docker/healthcheck.sh').replaceAll('\\', '/')
  const out = shell('mock_path=$(cd "$MOCK_BIN" && pwd); export PATH="$mock_path:$PATH"; bash "$HEALTH_SCRIPT"', { MOCK_BIN: bin.replaceAll('\\', '/'), HEALTH_SCRIPT: health, AF_LISTEN: '127.0.0.1:9898', AF_PUBLIC_URL: 'https://fold.example.org' })
  const args = out.split('\0').slice(0, -1)
  assert.ok(args.includes('Host: fold.example.org'))
  assert.ok(args.includes('--noproxy'))
  assert.equal(args.at(-1), 'http://127.0.0.1:9898/api/v1/setup')
  assert.ok(!args.includes('-k') && !args.includes('--insecure'))
})
test('Compose parses: non-root, loopback host network, volume and sandbox policy', () => {
  const emptyEnv = join(work, 'empty.env'); writeFileSync(emptyEnv, '')
  const v = JSON.parse(run('docker', ['compose', '--env-file', emptyEnv, '-f', 'compose.yaml', 'config', '--format', 'json']))
  const app = v.services.afplatform
  assert.equal(app.network_mode, 'host')
  assert.equal(app.user, '10001:10001')
  assert.equal(app.environment.AF_LISTEN, '127.0.0.1:8788')
  assert.equal(app.environment.AF_PUBLIC_URL, 'http://127.0.0.1:8788')
  assert.equal(app.ports, undefined)
  assert.equal(app.privileged, undefined)
  assert.deepEqual(app.cap_drop, ['ALL'])
  assert.ok(app.security_opt.includes('no-new-privileges:true'))
  assert.ok(app.security_opt.some(x => x.startsWith('seccomp=') && x.endsWith('seccomp-chromium.json')))
  assert.ok(app.volumes.some(x => x.type === 'volume' && x.target === '/data'))
  assert.equal(app.stop_grace_period, '1m0s')
})
test('optional Compose env overrides are forwarded, not silently ignored', () => {
  const p = join(work, 'override.env')
  writeFileSync(p, 'AF_PORT=9898\nAF_PUBLIC_URL=https://fold.example.org\nAF_MAX_BROWSERS=2\nAF_DESKTOP=0\n')
  const v = JSON.parse(run('docker', ['compose', '--env-file', p, '-f', 'compose.yaml', 'config', '--format', 'json']))
  const e = v.services.afplatform.environment
  assert.equal(e.AF_LISTEN, '127.0.0.1:9898')
  assert.equal(e.AF_PUBLIC_URL, 'https://fold.example.org')
  assert.equal(e.AF_MAX_BROWSERS, '2')
  assert.equal(e.AF_DESKTOP, '0')
})
test('vendored seccomp has pinned bytes, default deny, namespace rules and license', () => {
  const data = readFileSync(resolve(root, 'deploy/docker/seccomp-chromium.json'))
  assert.equal(createHash('sha256').update(data).digest('hex'), 'cc3e61cabda6bbc1e53e54d27ba4d55a9d3be829b6dd1a596f4a7b31b1cc7849')
  const policy = JSON.parse(data)
  assert.equal(policy.defaultAction, 'SCMP_ACT_ERRNO')
  for (const syscall of ['clone', 'setns', 'unshare']) assert.ok(policy.syscalls.some(r => r.action === 'SCMP_ACT_ALLOW' && r.names.includes(syscall) && (r.args || []).length === 0))
  assert.match(readFileSync(resolve(root, 'deploy/docker/PLAYWRIGHT-LICENSE'), 'utf8'), /Apache License/)
})
test('Docker context is allowlisted; image does not COPY the entire workspace', () => {
  const ignore = readFileSync(resolve(root, '.dockerignore'), 'utf8').split(/\r?\n/).filter(x => x && !x.startsWith('#'))
  assert.equal(ignore[0], '**')
  for (const prefix of ['private', 'evidence', 'transactions', '.git', 'dist', 'frontend']) assert.ok(!ignore.some(x => x.startsWith('!' + prefix)))
  const file = readFileSync(resolve(root, 'Dockerfile'), 'utf8')
  assert.ok(!/^COPY\s+\.\s/m.test(file))
  assert.match(file, /^USER 10001:10001$/m)
  assert.ok(!file.includes('--no-sandbox'))
})
console.log(`${count} Docker configuration checks passed. No image build, Linux runtime or Google login claimed.`)
