import { spawn, spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, existsSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { tmpdir } from 'node:os'
import { createServer } from 'node:net'
const exe = resolve(process.argv[2])
const data = mkdtempSync(join(tmpdir(), 'af-v2-probe-'))
const listener = createServer()
await new Promise((r) => listener.listen(0, '127.0.0.1', r))
const port = listener.address().port
await new Promise((r) => listener.close(r))
const origin = `http://127.0.0.1:${port}`
let child,
  stderr = '',
  stdout = ''
const start = async () => {
  child = spawn(
    exe,
    ['serve', '-data', data, '-listen', `127.0.0.1:${port}`, '-public-url', origin],
    { windowsHide: true },
  )
  child.stderr.on('data', (b) => (stderr += b))
  child.stdout.on('data', (b) => (stdout += b))
  for (let i = 0; i < 150; i++) {
    if (child.exitCode !== null) return false
    try {
      await fetch(origin)
      return true
    } catch {}
    await new Promise((r) => setTimeout(r, 100))
  }
  throw Error('server start timeout')
}
let cookie = '',
  csrf = ''
const api = async (path, body) => {
  const r = await fetch(origin + '/api/v1/' + path, {
    method: body ? 'POST' : 'GET',
    headers: {
      Origin: origin,
      'Content-Type': 'application/json',
      Cookie: cookie,
      'X-CSRF-Token': csrf,
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  const value = await r.json()
  if (r.headers.get('set-cookie')) cookie = r.headers.get('set-cookie').split(';')[0]
  if (value.csrf) csrf = value.csrf
  return { status: r.status, value }
}
try {
  const first = await start()
  console.log(
    'fresh_serve=' +
      JSON.stringify({ running: first, exit: first ? null : child.exitCode, stderr }),
  )
  if (first) {
    const state = await api('setup')
    console.log('setup_status=' + JSON.stringify(state))
    const token = readFileSync(join(data, 'setup-token.txt'), 'utf8').trim()
    const r = await api('setup', { name: 'probe-admin', password: 'probe-password-12345', token })
    console.log('web_setup_http=' + r.status)
    if (r.status !== 201) throw Error('setup failed')
  } else {
    const r = spawnSync(exe, ['init', '-data', data], { encoding: 'utf8', windowsHide: true })
    console.log('legacy_init_exit=' + r.status)
    if (r.status !== 0) throw Error(r.stderr)
    await start()
  }
  const password = existsSync(join(data, 'initial-admin-password.txt'))
    ? readFileSync(join(data, 'initial-admin-password.txt'), 'utf8').trim()
    : 'probe-password-12345'
  const login = await api('session', { name: first ? 'probe-admin' : 'admin', password })
  if (login.status !== 200) throw Error('login failed')
  const items = [
    {
      request_id: 'probe-v2-1',
      input: {
        name: 'complex',
        sequences: [
          { sequence: 'TEACHINGS', copies: 1 },
          { sequence: 'ACDEFG', copies: 2 },
        ],
      },
    },
    {
      request_id: 'probe-v2-2',
      input: { name: 'protein', sequences: [{ sequence: 'ACDEFGHIK', copies: 1 }] },
    },
  ]
  const preview = await api('jobs/batch', { items, execute: false })
  console.log(
    'batch_preview=' +
      JSON.stringify({
        status: preview.status,
        no_submission: preview.value.no_submission ?? null,
        count: preview.value.items?.length ?? 0,
      }),
  )
  const submit = await api('jobs/batch', { items, execute: true })
  console.log(
    'batch_submit=' +
      JSON.stringify({ status: submit.status, count: submit.value.jobs?.length ?? 0 }),
  )
  const retry = await api('jobs/batch', { items, execute: true })
  const list = await api('jobs')
  console.log(
    'batch_retry=' +
      JSON.stringify({
        status: retry.status,
        total: list.value.length,
        group_sequences:
          list.value.find?.((x) => x.request_id === 'probe-v2-1')?.input.sequences.length ?? 0,
      }),
  )
  console.log('real_google_submission=false')
} finally {
  if (child && child.exitCode === null) {
    child.kill()
    await new Promise((r) => child.once('exit', r))
  }
}
