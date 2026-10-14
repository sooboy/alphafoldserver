// Isolated opt-in LAN HTTP probe. No Google accounts, sessions or jobs are used.
import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, existsSync, writeFileSync } from 'node:fs'
import { request } from 'node:http'
import { createServer } from 'node:net'
import { join, resolve } from 'node:path'
import { tmpdir } from 'node:os'

const exe = resolve(process.argv[2])
const data = mkdtempSync(join(tmpdir(), 'af-lan-http-'))
const listener = createServer()
await new Promise(r => listener.listen(0, '127.0.0.1', r))
const port = listener.address().port
await new Promise(r => listener.close(r))
// The Host fixture exercises a LAN public origin without relying on this PC's IP.
const host = `192.168.1.100:${port}`, origin = 'http://' + host
const args = ['serve', '-data', data, '-listen', `0.0.0.0:${port}`, '-public-url', origin]
const events = []
let child
const call = (path = '/', options = {}) => new Promise((resolve, reject) => {
  const req = request({ hostname: '127.0.0.1', port, path, method: options.body ? 'POST' : 'GET',
    headers: { Host: host, ...options.headers }, timeout: 2000 }, res => {
    let body = ''
    res.setEncoding('utf8').on('data', b => (body += b)).on('end', () =>
      resolve({ status: res.statusCode, headers: res.headers, body }))
  })
  req.on('error', reject).on('timeout', () => req.destroy(Error('timeout')))
  req.end(options.body ? JSON.stringify(options.body) : undefined)
})
const defaultRun = spawnSync(exe, args, { encoding: 'utf8', windowsHide: true, timeout: 10000 })
events.push({ command: [exe, ...args], exit: defaultRun.status, stdout: defaultRun.stdout, stderr: defaultRun.stderr })
assert.equal(defaultRun.status, 1)
assert.match(defaultRun.stderr, /HTTPS_REQUIRED_FOR_TEAM_ACCESS/)
assert.equal(existsSync(join(data, 'setup-token.txt')), false)
console.log('default_lan_http=HTTPS_REQUIRED_FOR_TEAM_ACCESS;exit=1')
const event = { command: [exe, ...args, '-allow-insecure-http'], stdout: '', stderr: '', exit: null, signal: null }
events.push(event)
try {
  child = spawn(exe, [...args, '-allow-insecure-http'], { windowsHide: true })
  child.stdout.on('data', b => (event.stdout += b))
  child.stderr.on('data', b => (event.stderr += b))
  child.on('error', e => { event.error = e.message })
  child.on('exit', (exit, signal) => { event.exit = exit; event.signal = signal })
  let running = false
  for (let i = 0; i < 100; i++) {
    if (event.error) throw Error(event.error)
    if (child.exitCode !== null) break
    try { assert.equal((await call()).status, 200); running = true; break } catch {}
    await new Promise(r => setTimeout(r, 100))
  }
  if (!running) {
    assert.equal(child.exitCode, 1)
    assert.match(event.stderr, /flag provided but not defined: -allow-insecure-http/)
    console.log('opt_in=UNSUPPORTED_FLAG;exit=1')
  } else {
    console.log('opt_in=HTTP_RUNNING;root=200')
    const headers = { Origin: origin, 'Content-Type': 'application/json' }
    const token = readFileSync(join(data, 'setup-token.txt'), 'utf8').trim()
    const credentials = { name: 'lan-fixture-admin', password: 'lan-http-fixture-password-1234' }
    assert.equal((await call('/api/v1/setup', { headers: { ...headers, Origin: 'http://evil.example' }, body: { ...credentials, token } })).status, 403)
    assert.equal((await call('/api/v1/setup', { headers, body: { ...credentials, token } })).status, 201)
    assert.equal(existsSync(join(data, 'setup-token.txt')), false)
    const login = await call('/api/v1/session', { headers, body: credentials })
    assert.equal(login.status, 200)
    const rawCookie = login.headers['set-cookie'][0], cookie = rawCookie.split(';')[0]
    assert.match(rawCookie, /HttpOnly/)
    assert.match(rawCookie, /SameSite=Strict/)
    assert.doesNotMatch(rawCookie, /; Secure(?:;|$)/)
    const csrf = JSON.parse(login.body).csrf
    const member = { name: 'lan-fixture-member', password: 'member-fixture-password-1234', role: 'member' }
    assert.equal((await call('/api/v1/admin/users', { headers: { ...headers, Cookie: cookie }, body: member })).status, 403)
    assert.equal((await call('/api/v1/admin/users', { headers: { ...headers, Cookie: cookie, 'X-CSRF-Token': csrf }, body: member })).status, 201)
    assert.equal((await call('/api/v1/setup', { headers: { Host: 'evil.example' } })).status, 403)
    console.log('web_setup=201;login=200;member_create=201;init_invoked=false')
    console.log('cross_origin=403;wrong_host=403;missing_csrf=403;cookie=HttpOnly,SameSiteStrict')
    console.log('google_requests=0;cross_machine_test=false')
  }
} finally {
  if (child && child.exitCode === null && child.signalCode === null) {
    const ended = new Promise(r => child.once('exit', r))
    child.kill()
    await ended
  }
  if (process.env.AF_LAN_EVENTS) writeFileSync(process.env.AF_LAN_EVENTS, JSON.stringify(events, null, 2) + '\n')
}
