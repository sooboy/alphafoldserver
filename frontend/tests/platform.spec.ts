import { test, expect, type Page } from '@playwright/test'
import { spawn, type ChildProcess } from 'node:child_process'
import { mkdtempSync, readFileSync, existsSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { createServer } from 'node:net'

test.describe.configure({ mode: 'serial' })
let child: ChildProcess, origin: string, data: string, page: Page
const output = resolve('../private/platform-v2')
test.beforeAll(async ({ browser }) => {
  mkdirSync(output, { recursive: true })
  data = mkdtempSync(join(tmpdir(), 'af-ui-v2-'))
  const s = createServer()
  await new Promise<void>((r) => s.listen(0, '127.0.0.1', r))
  const port = (s.address() as { port: number }).port
  await new Promise<void>((r) => s.close(() => r()))
  origin = `http://127.0.0.1:${port}`
  child = spawn(
    resolve('../private/platform-v2/modified' + (process.platform === 'win32' ? '.exe' : '')),
    ['serve', '-data', data, '-listen', `127.0.0.1:${port}`, '-public-url', origin],
    { windowsHide: true, stdio: 'ignore' },
  )
  for (let i = 0; i < 100; i++) {
    try {
      await fetch(origin)
      break
    } catch {
      await new Promise((r) => setTimeout(r, 100))
    }
  }
  page = await browser.newPage()
  await page.addInitScript(() => {
    ;(window as unknown as { violations: string[] }).violations = []
    document.addEventListener('securitypolicyviolation', (e) =>
      (window as unknown as { violations: string[] }).violations.push(e.violatedDirective),
    )
  })
  page.on('dialog', (d) => d.accept())
})
test.afterAll(async () => {
  await page?.close()
  if (child && child.exitCode === null) {
    child.kill()
    await new Promise((r) => child.once('exit', r))
  }
})
async function jobs() {
  return await page.evaluate(async () => await (await fetch('/api/v1/jobs')).json())
}
async function post(path: string, body: unknown) {
  return await page.evaluate(
    async ({ path, body }) => {
      const s = await (await fetch('/api/v1/session')).json()
      const r = await fetch('/api/v1/' + path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': s.csrf },
        body: JSON.stringify(body),
      })
      return { status: r.status, value: await r.json() }
    },
    { path, body },
  )
}

test('first run initializes administrator from the page without init', async () => {
  await page.goto(origin)
  await expect(page.getByRole('heading', { name: '创建你的工作空间' })).toBeVisible()
  await page.screenshot({ path: join(output, 'setup-desktop.png'), fullPage: true })
  await page
    .getByLabel('一次性初始化凭据')
    .fill(readFileSync(join(data, 'setup-token.txt'), 'utf8').trim())
  await page.getByLabel('管理员用户名').fill('research-admin')
  await page.getByLabel('密码', { exact: true }).fill('ui-admin-password-1234')
  await page.getByLabel('确认密码').fill('ui-admin-password-1234')
  await page.getByRole('button', { name: '创建管理员并进入平台' }).click()
  await expect(page.getByRole('heading', { name: '预测工作台', exact: true })).toBeVisible()
  expect(existsSync(join(data, 'setup-token.txt'))).toBe(false)
  expect(existsSync(join(data, 'initial-admin-password.txt'))).toBe(false)
  const setup = await fetch(origin + '/api/v1/setup').then((r) => r.json())
  expect(setup.required).toBe(false)
})
test('manual batch preserves sequence groups, preview, retry and locked reload', async () => {
  await page.locator('#seq-0-0').fill('TEACHINGS')
  await page.getByRole('button', { name: '添加序列到此任务' }).click()
  await page.locator('#seq-0-1').fill('ACDEFG')
  await page.locator('#copies-0-1').fill('2')
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await page.locator('#seq-1-0').fill('ACDEFGHIK')
  await page.getByRole('button', { name: '校验预览' }).click()
  await expect(page.getByRole('status')).toContainText('2 个任务校验通过')
  expect((await jobs()).length).toBe(0)
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.screenshot({ path: join(output, 'batch-desktop.png'), fullPage: true })
  await page.screenshot({ path: join(output, 'batch-viewport.png') })
  await page.getByRole('button', { name: '确认批量提交' }).click()
  await expect(page.locator('#jobs tr')).toHaveCount(2)
  const before = await jobs()
  expect(
    before.find((j: { input: { sequences: unknown[] } }) => j.input.sequences.length === 2),
  ).toBeTruthy()
  await page.getByRole('button', { name: '确认原批次结果' }).click()
  await expect(page.getByText('2 个任务已确认入队。', { exact: false })).toBeVisible()
  expect((await jobs()).length).toBe(2)
  await page.reload()
  await expect(page.locator('#draft')).toHaveAttribute('disabled', '')
  await expect(page.locator('#seq-0-0')).toBeDisabled()
  expect((await jobs()).length).toBe(2)
})
test('multi-file import is preview-only, rejects unsupported fields, no partial append', async () => {
  await page.getByRole('button', { name: '新建批次' }).click()
  const local = { name: 'Imported monomer', sequences: [{ sequence: 'TEACHINGS', copies: 1 }] }
  const server = [
    {
      name: 'Imported complex',
      modelSeeds: [],
      sequences: [
        { proteinChain: { sequence: 'TEACHINGS', count: 1 } },
        { proteinChain: { sequence: 'ACDEFG', count: 2 } },
      ],
      dialect: 'alphafoldserver',
      version: 1,
    },
  ]
  await page.getByLabel('选择多个 JSON 文件').setInputFiles([
    {
      name: 'local.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(local)),
    },
    {
      name: 'server.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(server)),
    },
  ])
  await expect(page.getByTestId('task-card')).toHaveCount(2)
  await expect(page.locator('#name-1')).toHaveValue('Imported complex')
  await expect(page.locator('#copies-1-1')).toHaveValue('2')
  expect((await jobs()).length).toBe(2)
  const bad = { ...server[0], modelSeeds: ['123'] }
  await page.getByLabel('选择多个 JSON 文件').setInputFiles([
    {
      name: 'valid.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(local)),
    },
    {
      name: 'unsupported.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(bad)),
    },
  ])
  await expect(page.getByText(/unsupported.json · 第 1 项/)).toBeVisible()
  await expect(page.getByTestId('task-card')).toHaveCount(2)
  await page.getByRole('button', { name: '确认批量提交' }).click()
  await expect(page.locator('#jobs tr')).toHaveCount(4)
})
test('admin screens, mobile layout, member isolation, and CSP', async () => {
  await page.getByRole('button', { name: '共享账号池' }).click()
  await expect(page.getByText('连接第一个共享账号')).toBeVisible()
  await page.getByRole('button', { name: '团队成员' }).click()
  await page.getByLabel('新成员用户名').fill('member-one')
  await page.getByLabel('新成员密码').fill('member-password-1234')
  await page.getByRole('button', { name: '创建成员' }).click()
  await expect(page.getByRole('cell', { name: 'member-one', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '预测工作台' }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: join(output, 'batch-mobile.png'), fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.getByRole('button', { name: '退出工作空间' }).click()
  await page.getByLabel('用户名', { exact: true }).fill('member-one')
  await page.getByLabel('密码', { exact: true }).fill('member-password-1234')
  await page.getByRole('button', { name: '登录工作空间' }).click()
  await expect(page.getByRole('heading', { name: '预测工作台', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '共享账号池' })).toHaveCount(0)
  expect((await jobs()).length).toBe(0)
  const blocked = await page.evaluate(async () => (await fetch('/api/v1/admin/accounts')).status)
  expect(blocked).toBe(403)
  const batch = await post('jobs/batch', {
    items: [
      {
        request_id: 'member-batch',
        input: { name: 'private', sequences: [{ sequence: 'TEACHINGS', copies: 1 }] },
      },
    ],
    execute: true,
  })
  expect(batch.status).toBe(202)
  expect((await jobs()).length).toBe(1)
  expect(
    await page.evaluate(() => (window as unknown as { violations: string[] }).violations),
  ).toEqual([])
})
