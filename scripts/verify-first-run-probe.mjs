// Isolated startup probe: no init, Google login, account import or remote jobs.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtempSync, readFileSync, existsSync, writeFileSync } from 'node:fs'
import { request } from 'node:http'
import { createServer } from 'node:net'
import { join, resolve } from 'node:path'
import { tmpdir } from 'node:os'

const exe = resolve(process.argv[2])
const data = mkdtempSync(join(tmpdir(), 'af-first-run-'))
const listener = createServer()
await new Promise((r) => listener.listen(0, '127.0.0.1', r))
const port = listener.address().port
await new Promise((r) => listener.close(r))
const origin = `http://127.0.0.1:${port}`
const events = []
let child
const call = (path = '/', { host = `127.0.0.1:${port}`, headers = {}, body } = {}) =>
  new Promise((resolve, reject) => {
    const req = request(origin + path, {
      method: body ? 'POST' : 'GET',
      headers: { Host: host, ...headers },
      timeout: 2000,
    }, (res) => {
      let text = ''
      res.setEncoding('utf8').on('data', (b) => (text += b)).on('end', () =>
        resolve({ status: res.statusCode, headers: res.headers, body: text }))
    })
    req.on('error', reject).on('timeout', () => req.destroy(Error('request timeout')))
    req.end(body ? JSON.stringify(body) : undefined)
  })
async function start() {
  const args = ['serve', '-data', data, '-listen', `127.0.0.1:${port}`, '-public-url', origin]
  const event = { command: [exe, ...args], stdout: '', stderr: '', exit: null, signal: null }
  events.push(event)
  child = spawn(exe, args, { windowsHide: true })
  child.stdout.on('data', (b) => (event.stdout += b))
  child.stderr.on('data', (b) => (event.stderr += b))
  child.on('error', (e) => { event.error = e.message })
  child.on('exit', (code, signal) => { event.exit = code; event.signal = signal })
  for (let i = 0; i < 100; i++) {
    if (event.error || child.exitCode !== null) throw Error(JSON.stringify(event))
    try { await call(); return } catch {}
    await new Promise((r) => setTimeout(r, 100))
  }
  throw Error('startup timeout')
}
async function stop() {
  if (child && child.exitCode === null && child.signalCode === null) {
    const ended = new Promise((r) => child.once('exit', r))
    child.kill()
    await ended
  }
}
const jsonHeaders = { Origin: origin, 'Content-Type': 'application/json' }
try {
  await start()
  assert.equal((await call()).status, 200)
  console.log('canonical_root=200')
  const alias = await call('/', { host: `localhost:${port}`, headers: { Accept: 'text/html' } })
  console.log('localhost_root=' + JSON.stringify({ status: alias.status,
    canonical_redirect: alias.headers.location === origin + '/',
    forbidden: alias.body.includes('FORBIDDEN') }))
  const wrong = await call('/', { host: `127.0.0.1:${port + 1}`, headers: { Accept: 'text/html' } })
  assert.equal(wrong.status, 403)
  console.log('mismatched_address=' + JSON.stringify({ status: wrong.status,
    public_url_hint: wrong.body.includes('-public-url') }))
  const forbidden = await call('/api/v1/setup', { host: `localhost:${port}` })
  assert.equal(forbidden.status, 403)
  console.log('alias_api=403')
  const initial = await call('/api/v1/setup')
  assert.deepEqual(JSON.parse(initial.body), { required: true, token_required: true })
  console.log('fresh_setup=' + initial.body.trim())
  const token = readFileSync(join(data, 'setup-token.txt'), 'utf8').trim()
  const credentials = { name: 'first-run-admin', password: 'first-run-fixture-password-1234' }
  const setup = await call('/api/v1/setup', { headers: jsonHeaders, body: { ...credentials, token } })
  assert.equal(setup.status, 201)
  assert.equal(existsSync(join(data, 'initial-admin-password.txt')), false)
  assert.equal(existsSync(join(data, 'setup-token.txt')), false)
  assert.equal((await call('/api/v1/session', { headers: jsonHeaders, body: credentials })).status, 200)
  console.log('web_setup=201;login=200;init_invoked=false')
  await stop()
  await start()
  assert.deepEqual(JSON.parse((await call('/api/v1/setup')).body), { required: false, token_required: false })
  assert.equal((await call('/api/v1/session', { headers: jsonHeaders, body: credentials })).status, 200)
  console.log('restart_setup_required=false;login=200;google_requests=0')
} finally {
  await stop()
  if (process.env.AF_FIRST_RUN_EVENTS) {
    writeFileSync(process.env.AF_FIRST_RUN_EVENTS, JSON.stringify(events, null, 2) + '\n')
  }
}
