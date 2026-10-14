export type User = { id: string; name: string; role: 'admin' | 'member'; disabled: boolean }
export type Sequence = { sequence: string; copies: number }
export type Input = { name: string; sequences: Sequence[] }
export type Item = { request_id: string; input: Input }
export type Draft = { items: Item[]; locked: boolean }
export type Job = {
  id: string
  owner: string
  request_id: string
  input: Input
  stage: string
  error?: string
  created_at: number
  remote_id?: string
  seed: number
}
export type Account = {
  id: string
  label: string
  status: string
  enabled: boolean
  remaining: number | null
  last_error?: string
}
export type Meta = { browser_pool: { active: number; limit: number } }
let csrf = ''
export const setCSRF = (value: string) => {
  csrf = value
}

// getRandomValues also works on private LAN HTTP, unlike crypto.randomUUID.
// Keep 122 random bits and UUID v4 formatting; never fall back to Math.random.
export function newRequestID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
export class APIError extends Error {
  constructor(
    public data: {
      error: string
      index?: number
      issues?: { source: string; index: number; code: string }[]
    },
    public status: number,
  ) {
    super(data.error)
  }
}
export async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const r = await fetch('/api/v1/' + path, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await r.json()
  if (!r.ok) throw new APIError(data, r.status)
  return data as T
}
const hints: Record<string, string> = {
  FORBIDDEN: '认证失败或会话已失效，请检查凭据并重新登录。',
  ALREADY_INITIALIZED: '平台已初始化，请刷新后登录。',
  PASSWORD_LENGTH_12_TO_256: '密码长度应为 12–256 字节。',
  INVALID_USER: '用户名只能包含英文、数字、下划线和连字符，长度不超过 64。',
  INVALID_SEQUENCE: '请填写标准大写氨基酸序列（20 种），不包含 FASTA 标题。',
  INVALID_TOTAL_SIZE: '单任务总残基数（计入 copies）不能超过 5000。',
  INVALID_COPIES_OR_SIZE: 'copies 应为 1–5000，单条序列不超过 5000 残基。',
  QUEUE_FULL: '队列已满：每人最多 50 个未结束任务，全平台最多 1000 个。',
  IDEMPOTENCY_CONFLICT: '该请求已提交过不同内容，请核对历史任务；不要盲目创建新请求重试。',
  MANUAL_SEED_NOT_SUPPORTED: '当前只支持自动 seed，请将 modelSeeds 设为空数组。',
  UNSUPPORTED_OR_INVALID_JSON:
    'JSON 包含不支持的字段或类型。当前只接受蛋白序列与 copies，不会忽略其他参数。',
  UNSUPPORTED_DIALECT_VERSION: '目前导入支持 AlphaFold Server dialect version 1。',
  ONLY_PROTEIN_SUPPORTED: '目前只支持 proteinChain，不支持 DNA、RNA 或配体。',
  LOGIN_WINDOW_OPEN: '请关闭该账号全部认证窗口，再点击“完成登录并检查”。',
  LOGIN_REQUIRES_SERVER_DESKTOP: '服务器需要图形桌面，请通过受保护的远程桌面完成 Google 认证。',
  BROWSER_NOT_FOUND: '服务端未找到 Chrome/Chromium/Edge，请安装或通过 --browser 指定。',
  BROWSER_BUSY: '活跃浏览器已满，请先关闭其他认证窗口或稍后重试。',
  BATCH_SIZE_1_TO_50: '每批允许 1–50 个任务。',
  INVALID_JSON: 'JSON 格式错误，或请求超过 1 MiB。',
}
export function message(error: unknown): string {
  if (error instanceof APIError) {
    const d = error.data
    if (d.issues)
      return d.issues
        .map((i) => `${i.source} · 第 ${i.index + 1} 项：${hints[i.code] || i.code}`)
        .join('\n')
    return (d.index === undefined ? '' : `任务 ${d.index + 1}：`) + (hints[d.error] || d.error)
  }
  return error instanceof Error ? error.message : '操作失败，请重试。'
}
export const stages: Record<string, string> = {
  queued: '已排队',
  waiting_for_account: '等待可用账号',
  preflight: '提交前检查',
  creating: '创建中',
  draft: '上游草稿',
  submitting: '提交中',
  pending: '预测中',
  succeeded: '已完成',
  failed: '失败',
  preflight_failed: '检查失败',
  submission_unknown: '提交结果待确认',
  draft_attention: '草稿待核对',
  cancelled: '已取消',
  ready: '已验证',
  needs_login: '需要认证',
  connection_error: '连接异常',
  quota_exhausted: '额度用尽',
  queue_closed: '上游队列关闭',
  cooldown: '限流暂停',
  identity_mismatch: '身份不一致',
}
export const newItem = (): Item => {
  const d = new Date(),
    p = (n: number) => String(n).padStart(2, '0')
  return {
    request_id: newRequestID(),
    input: {
      name: `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}_${p(d.getHours())}:${p(d.getMinutes())}`,
      sequences: [{ sequence: '', copies: 1 }],
    },
  }
}
export const newDraft = (): Draft => ({ items: [newItem()], locked: false })
export const draftKey = (id: string) => 'af-batch-draft:v2:' + id
export function readDraft(id: string): Draft {
  try {
    const d = JSON.parse(sessionStorage.getItem(draftKey(id)) || 'null')
    if (
      d &&
      typeof d.locked === 'boolean' &&
      Array.isArray(d.items) &&
      d.items.length > 0 &&
      d.items.length <= 50 &&
      d.items.every(
        (x: Item) =>
          typeof x.request_id === 'string' &&
          typeof x.input?.name === 'string' &&
          Array.isArray(x.input?.sequences) &&
          x.input.sequences.length > 0 &&
          x.input.sequences.every(
            (s) => typeof s.sequence === 'string' && Number.isInteger(s.copies),
          ),
      )
    )
      return d
  } catch {}
  return newDraft()
}
