import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Box,
  Check,
  ChevronRight,
  Code2,
  Database,
  Download,
  FileJson,
  FlaskConical,
  Layers3,
  Loader2,
  LockKeyhole,
  LogOut,
  Plus,
  RefreshCw,
  Send,
  ShieldCheck,
  Sparkles,
  Trash2,
  Upload,
  Users,
  X,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Input as TextInput } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Separator } from '@/components/ui/separator'
import {
  api,
  APIError,
  setCSRF,
  message,
  newDraft,
  newItem,
  readDraft,
  draftKey,
  stages,
  type User,
  type Job,
  type Draft,
  type Account,
  type Meta,
  type Input,
} from '@/lib/api'

type View = 'tasks' | 'accounts' | 'members' | 'api'
const nav = [
  { id: 'tasks', label: '预测工作台', icon: Layers3 },
  { id: 'accounts', label: '共享账号池', icon: Database },
  { id: 'members', label: '团队成员', icon: Users },
  { id: 'api', label: '接口与凭据', icon: Code2 },
] as const
const titles = {
  tasks: ['预测工作台', '将研究想法，变成下一次探索。'],
  accounts: ['共享账号池', '配置保留在服务端，浏览器按需唤醒。'],
  members: ['团队成员', '共享计算资源，保留各自的研究空间。'],
  api: ['接口与凭据', '一个团队入口，两层清晰的接口边界。'],
}
const completed = (stage: string) =>
  ['succeeded', 'failed', 'preflight_failed', 'cancelled'].includes(stage)
function Status({ value }: { value: string }) {
  return (
    <Badge
      variant="outline"
      className={
        value === 'succeeded' || value === 'ready'
          ? 'status-good'
          : ['failed', 'preflight_failed', 'submission_unknown', 'draft_attention'].includes(value)
            ? 'status-warning'
            : 'status-neutral'
      }
    >
      <span className="status-dot" />
      {stages[value] || value}
    </Badge>
  )
}
function Brand() {
  return (
    <div className="brand">
      <div className="brand-icon">
        <Box size={22} />
      </div>
      <div>
        <strong>
          FoldSpace<span>TEAM PLATFORM</span>
        </strong>
      </div>
    </div>
  )
}

export default function App() {
  const [boot, setBoot] = useState(true),
    [setup, setSetup] = useState(false),
    [me, setMe] = useState<User | null>(null)
  const [view, setView] = useState<View>('tasks'),
    [notice, setNotice] = useState(''),
    [danger, setDanger] = useState(false),
    [busy, setBusy] = useState(false)
  const [jobs, setJobs] = useState<Job[]>([]),
    [accounts, setAccounts] = useState<Account[]>([]),
    [users, setUsers] = useState<User[]>([]),
    [meta, setMeta] = useState<Meta | null>(null)
  const [instances, setInstances] = useState<
    { account_id: string; login_window: boolean; busy: boolean }[]
  >([])
  const [draft, setDraft] = useState<Draft>(newDraft),
    [preview, setPreview] = useState(false),
    [search, setSearch] = useState(''),
    [token, setToken] = useState('')
  const importRef = useRef<HTMLInputElement>(null),
    refreshing = useRef(false),
    operation = useRef(false),
    sessionVersion = useRef(0)
  const notify = (text: string, bad = false) => {
    setNotice(text)
    setDanger(bad)
  }
  const enter = useCallback((s: { user: User; csrf: string }) => {
    sessionVersion.current++
    setJobs([])
    setAccounts([])
    setUsers([])
    setMeta(null)
    setCSRF(s.csrf)
    setMe(s.user)
    setDraft(readDraft(s.user.id))
    setSetup(false)
    setToken('')
    setView('tasks')
  }, [])
  const refresh = useCallback(async () => {
    if (!me || refreshing.current) return
    refreshing.current = true
    const version = sessionVersion.current
    try {
      const [j, m] = await Promise.all([api<Job[]>('jobs'), api<Meta>('meta')])
      if (version !== sessionVersion.current) return
      setJobs(j)
      setMeta(m)
      if (me.role === 'admin') {
        const [a, u] = await Promise.all([
          api<{ accounts: Account[]; browsers: { instances: typeof instances } }>('admin/accounts'),
          api<User[]>('admin/users'),
        ])
        if (version !== sessionVersion.current) return
        setAccounts(a.accounts)
        setInstances(a.browsers.instances)
        setUsers(u)
      }
    } catch (e) {
      notify(message(e), true)
    } finally {
      refreshing.current = false
    }
  }, [me])
  useEffect(() => {
    ;(async () => {
      try {
        const s = await api<{ required: boolean }>('setup')
        setSetup(s.required)
        if (!s.required) {
          try {
            enter(await api('session'))
          } catch (e) {
            if (!(e instanceof APIError && e.status === 403)) throw e
          }
        }
      } catch (e) {
        notify(message(e), true)
      } finally {
        setBoot(false)
      }
    })()
  }, [enter])
  useEffect(() => {
    if (me) {
      void refresh()
      const timer = setInterval(() => void refresh(), 10000)
      return () => clearInterval(timer)
    }
  }, [me, refresh])
  const persist = (next: Draft) => {
    if (!me) throw Error('请先登录')
    sessionStorage.setItem(draftKey(me.id), JSON.stringify(next))
    setDraft(next)
  }
  const change = (fn: (next: Draft) => void) => {
    const next = structuredClone(draft)
    fn(next)
    try {
      persist(next)
      setPreview(false)
    } catch {
      notify('浏览器无法保存草稿，请允许会话存储；为避免重复提交已停止操作。', true)
    }
  }
  const perform = async (fn: () => Promise<void>) => {
    if (operation.current) return
    operation.current = true
    setBusy(true)
    notify('')
    try {
      await fn()
    } catch (e) {
      notify(message(e), true)
    } finally {
      operation.current = false
      setBusy(false)
    }
  }
  const authenticate = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    void perform(async () => {
      const body = { name: String(f.get('name')), password: String(f.get('password')) }
      if (setup) {
        if (body.password !== f.get('confirm')) throw Error('两次密码不一致。')
        await api('setup', 'POST', { ...body, token: String(f.get('token')).trim() })
        setSetup(false)
        notify('平台初始化成功。')
      }
      enter(await api('session', 'POST', body))
    })
  }
  const previewBatch = () =>
    perform(async () => {
      await api('jobs/batch', 'POST', { items: draft.items, execute: false })
      setPreview(true)
      notify(`全部 ${draft.items.length} 个任务校验通过，尚未提交。`)
    })
  const submit = () =>
    perform(async () => {
      if (!draft.locked) {
        await api('jobs/batch', 'POST', { items: draft.items, execute: false })
        if (
          !confirm(
            `确认将 ${draft.items.length} 个任务加入共享队列？实际提交会使用 AlphaFold 额度。`,
          )
        )
          return
        persist({ ...draft, locked: true })
      }
      // Persist identities before the request. A network failure leaves the draft locked.
      const result = await api<{ jobs: Job[] }>('jobs/batch', 'POST', {
        items: draft.items,
        execute: true,
      })
      notify(
        `${result.jobs.length} 个任务已确认入队。输入已锁定；重复确认会复用原请求，不重复创建。`,
      )
      await refresh()
    })
  const importFiles = (files: File[]) =>
    perform(async () => {
      if (files.length === 0) return
      if (files.length > 50 || files.reduce((n, f) => n + f.size, 0) > 900000)
        throw Error('一次最多导入 50 个文件，总大小请小于 900 KB。')
      const payload = await Promise.all(
        files.map(async (f) => {
          try {
            return { name: f.name, content: JSON.parse(await f.text()) }
          } catch {
            throw Error(`${f.name}：不是有效的 JSON 文件。`)
          }
        }),
      )
      const result = await api<{ items: { source: string; index: number; input: Input }[] }>(
        'import',
        'POST',
        { files: payload },
      )
      const empty =
        draft.items.length === 1 &&
        draft.items[0].input.sequences.length === 1 &&
        !draft.items[0].input.sequences[0].sequence.trim()
      const items = [
        ...(empty ? [] : draft.items),
        ...result.items.map((r) => ({ request_id: crypto.randomUUID(), input: r.input })),
      ]
      if (items.length > 50) throw Error('导入后超过每批 50 个任务的上限。请分批提交。')
      persist({ items, locked: false })
      setPreview(false)
      notify(`已导入 ${files.length} 个文件、${result.items.length} 个任务。请核对后确认提交。`)
    })
  const accountAction = (id: string, action: string) =>
    perform(async () => {
      await api(`admin/accounts/${id}/${action}`, 'POST', {})
      await refresh()
      notify(
        action === 'login'
          ? '请在服务器普通浏览器中完成 Google 登录，进入 AlphaFold Server 后关闭该账号的所有认证窗口，再点击“完成登录并检查”。'
          : '操作完成。',
      )
    })
  const signout = () =>
    perform(async () => {
      await api('session', 'DELETE')
      sessionVersion.current++
      setMe(null)
      setCSRF('')
      setToken('')
      setJobs([])
      setAccounts([])
      setUsers([])
      notify('已退出登录。')
    })
  const alert = notice && (
    <Alert variant={danger ? 'destructive' : 'default'} className="notice" aria-live="polite">
      <AlertDescription className="whitespace-pre-wrap">{notice}</AlertDescription>
      <button type="button" aria-label="关闭提示" onClick={() => setNotice('')}>
        <X size={16} />
      </button>
    </Alert>
  )
  if (boot)
    return (
      <div className="loading">
        <Loader2 className="animate-spin" />
        <span>正在连接工作空间…</span>
      </div>
    )
  if (!me)
    return (
      <main className="auth-shell">
        <section className="auth-story">
          <Brand />
          <div>
            <Badge className="story-badge" variant="outline">
              <Sparkles size={12} /> YOUR NEXT DISCOVERY
            </Badge>
            <h1>
              让协作，
              <br />
              成为研究的
              <br />
              <em>一部分。</em>
            </h1>
            <p>
              从一条序列到一组复合物。
              <br />
              让团队专注探索，让平台管理提交。
            </p>
          </div>
          <div className="auth-footer">
            <ShieldCheck size={16} /> 服务端会话 · 独立任务空间
          </div>
        </section>
        <section className="auth-panel">
          <div className="auth-form-wrap">
            <div className="eyebrow">WELCOME TO FOLDSPACE</div>
            <h2>{setup ? '创建你的工作空间' : '欢迎回来'}</h2>
            <p className="text-muted-foreground">
              {setup
                ? '首次运行只需在这里创建管理员，无需执行 init。'
                : '使用团队平台账号登录，不是 Google 账号。'}
            </p>
            {alert}
            <form id="login-form" onSubmit={authenticate} className="space-y-5 mt-8">
              {setup && (
                <div className="space-y-2">
                  <Label htmlFor="setup-token">一次性初始化凭据</Label>
                  <TextInput
                    id="setup-token"
                    name="token"
                    type="password"
                    required
                    autoComplete="off"
                  />
                  <p className="helper">
                    从服务器数据目录的 <code>setup-token.txt</code> 复制，完成后自动失效。默认位置：
                    <code>private/go-platform/</code>
                  </p>
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="username">{setup ? '管理员用户名' : '用户名'}</Label>
                <TextInput
                  id="username"
                  name="name"
                  autoComplete="username"
                  required
                  maxLength={64}
                  placeholder="your-name"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">密码</Label>
                <TextInput
                  id="password"
                  name="password"
                  type="password"
                  autoComplete={setup ? 'new-password' : 'current-password'}
                  minLength={setup ? 12 : undefined}
                  required
                  placeholder={setup ? '至少 12 个字符' : '输入你的密码'}
                />
              </div>
              {setup && (
                <div className="space-y-2">
                  <Label htmlFor="confirm-password">确认密码</Label>
                  <TextInput
                    id="confirm-password"
                    name="confirm"
                    type="password"
                    autoComplete="new-password"
                    minLength={12}
                    required
                  />
                </div>
              )}
              <Button type="submit" className="w-full h-11" disabled={busy}>
                {busy ? <Loader2 className="animate-spin" /> : null}
                {setup ? '创建管理员并进入平台' : '登录工作空间'}
                <ArrowRight size={16} />
              </Button>
            </form>
            <p className="auth-note">
              {setup
                ? '初始化完成后，在“团队成员”中邀请同事。'
                : '平台账户由管理员创建。忘记密码请联系管理员。'}
            </p>
          </div>
        </section>
      </main>
    )
  const admin = me.role === 'admin',
    totalResidues = draft.items.reduce(
      (n, i) =>
        n +
        i.input.sequences.reduce((k, s) => k + s.sequence.replace(/\s/g, '').length * s.copies, 0),
      0,
    )
  const shownJobs = jobs.filter((j) =>
    (j.input.name + ' ' + j.id + ' ' + j.stage).toLowerCase().includes(search.toLowerCase()),
  )
  return (
    <div className="app-shell" id="workspace">
      <aside className="sidebar">
        <Brand />
        <div className="workspace-chip">
          <div className="workspace-avatar">F</div>
          <div>
            <strong>团队工作空间</strong>
            <span>Shared research workspace</span>
          </div>
          <ChevronRight size={15} />
        </div>
        <div className="nav-caption">WORKSPACE</div>
        <nav>
          {nav
            .filter((n) => admin || !['accounts', 'members'].includes(n.id))
            .map((n) => (
              <button
                key={n.id}
                className={'nav-item ' + (view === n.id ? 'selected' : '')}
                aria-current={view === n.id ? 'page' : undefined}
                onClick={() => {
                  setView(n.id)
                  setNotice('')
                  setToken('')
                }}
              >
                <n.icon size={18} />
                {n.label}
                {n.id === 'tasks' && <span className="nav-count">{jobs.length}</span>}
              </button>
            ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="resource-note">
            <div className="flex items-center gap-2">
              <span className="live-dot" />
              <strong>按需唤醒 · 有限并发</strong>
            </div>
            <p>账号配置长期保存，无需保持所有浏览器开启。</p>
          </div>
          <Separator className="opacity-20 my-5" />
          <div className="identity">
            <div className="user-avatar">{me.name.slice(0, 1).toUpperCase()}</div>
            <div>
              <strong>{me.name}</strong>
              <span>{admin ? '工作空间管理员' : '团队成员'}</span>
            </div>
            <Button
              variant="ghost"
              size="icon"
              id="logout"
              title="退出登录"
              aria-label="退出登录"
              disabled={busy}
              onClick={signout}
            >
              <LogOut size={16} />
            </Button>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumb">
            工作空间 <ChevronRight size={14} />
            <span>{titles[view][0]}</span>
          </div>
          <div className="flex items-center gap-3">
            <Badge variant="outline" className="privacy-badge">
              <LockKeyhole size={12} />
              {admin ? '管理员视图' : '仅本人任务'}
            </Badge>
            <Button
              variant="ghost"
              size="icon"
              aria-label="刷新数据"
              disabled={busy}
              onClick={() => void perform(refresh)}
            >
              <RefreshCw size={16} />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              aria-label="退出工作空间"
              disabled={busy}
              onClick={signout}
            >
              <LogOut size={16} />
            </Button>
          </div>
        </header>
        <main className="content">
          <div className="page-heading">
            <div>
              <div className="eyebrow">RESEARCH, TOGETHER.</div>
              <h1>{titles[view][0]}</h1>
              <p>{titles[view][1]}</p>
            </div>
            <Badge variant="outline" className="version-badge">
              PLATFORM / 02
            </Badge>
          </div>
          {alert}
          {view === 'tasks' && (
            <>
              <div className="metrics">
                <Metric
                  title={admin ? '团队任务' : '我的任务'}
                  value={jobs.length}
                  icon={<Layers3 />}
                  foot="任务输入与结果按成员隔离"
                />
                <Metric
                  title="进行中的任务"
                  value={jobs.filter((j) => !completed(j.stage)).length}
                  icon={<Activity />}
                  foot="包括排队、预测与待核对"
                />
                <Metric
                  title="已完成预测"
                  value={jobs.filter((j) => j.stage === 'succeeded').length}
                  icon={<Check />}
                  foot="在任务记录中下载结果"
                />
                <Metric
                  title="活跃浏览器"
                  value={`${meta?.browser_pool.active ?? 0} / ${meta?.browser_pool.limit ?? '—'}`}
                  icon={<Database />}
                  foot="共享账号 · 按需启动"
                />
              </div>
              <div className="composer-layout">
                <Card className="composer">
                  <CardHeader className="section-header">
                    <div>
                      <CardTitle className="flex items-center gap-2">
                        <FlaskConical size={19} /> 批量创建预测
                      </CardTitle>
                      <CardDescription>
                        一个任务可含多条序列；每个任务独立提交、独立返回结果。
                      </CardDescription>
                    </div>
                    <Badge variant="secondary">{draft.items.length} / 50 任务</Badge>
                  </CardHeader>
                  <CardContent>
                    <div className="composer-toolbar">
                      <div className="flex flex-wrap gap-2">
                        <Button
                          variant="outline"
                          type="button"
                          disabled={busy || draft.locked || draft.items.length >= 50}
                          onClick={() => change((d) => d.items.push(newItem()))}
                        >
                          <Plus size={15} /> 添加任务
                        </Button>
                        <Button
                          variant="outline"
                          disabled={busy || draft.locked}
                          onClick={() => importRef.current?.click()}
                        >
                          <Upload size={15} /> 导入 JSON 文件
                        </Button>
                        <input
                          ref={importRef}
                          type="file"
                          multiple
                          accept=".json,application/json"
                          className="sr-only"
                          aria-label="选择多个 JSON 文件"
                          onChange={(e) => {
                            const files = Array.from(e.target.files || [])
                            e.target.value = ''
                            void importFiles(files)
                          }}
                        />
                      </div>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy}
                        onClick={() => {
                          if (confirm('开始新批次？当前草稿将被替换，已提交的任务不会取消。')) {
                            try {
                              persist(newDraft())
                              setPreview(false)
                            } catch (e) {
                              notify(message(e), true)
                            }
                          }
                        }}
                      >
                        新建批次 <ArrowUpRight size={14} />
                      </Button>
                    </div>
                    <fieldset id="draft" disabled={busy || draft.locked} className="space-y-4">
                      <legend className="sr-only">本批任务</legend>
                      {draft.items.map((item, i) => (
                        <section
                          className="task-card"
                          key={item.request_id}
                          data-testid="task-card"
                        >
                          <div className="task-card-header">
                            <div className="flex items-center gap-2">
                              <span className="task-number">{String(i + 1).padStart(2, '0')}</span>
                              <strong>预测任务</strong>
                              <span className="task-chain-count">
                                {item.input.sequences.length} 条序列
                              </span>
                            </div>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              aria-label={`删除任务 ${i + 1}`}
                              disabled={draft.items.length === 1}
                              onClick={() =>
                                change((d) => {
                                  d.items.splice(i, 1)
                                })
                              }
                            >
                              <Trash2 size={14} />
                            </Button>
                          </div>
                          <div className="space-y-2">
                            <Label htmlFor={`name-${i}`}>任务名称</Label>
                            <TextInput
                              id={`name-${i}`}
                              data-testid="task-name"
                              maxLength={80}
                              value={item.input.name}
                              onChange={(e) =>
                                change((d) => {
                                  d.items[i].input.name = e.target.value
                                })
                              }
                            />
                          </div>
                          <div className="sequences">
                            {item.input.sequences.map((seq, k) => (
                              <div className="sequence-editor" key={k}>
                                <div className="sequence-top">
                                  <Label htmlFor={`seq-${i}-${k}`}>
                                    <span className="chain-label">
                                      {String.fromCharCode(65 + (k % 26))}
                                    </span>{' '}
                                    蛋白序列 {k + 1}
                                  </Label>
                                  {item.input.sequences.length > 1 && (
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      aria-label={`删除任务 ${i + 1} 的序列 ${k + 1}`}
                                      onClick={() =>
                                        change((d) => {
                                          d.items[i].input.sequences.splice(k, 1)
                                        })
                                      }
                                    >
                                      <X size={13} />
                                    </Button>
                                  )}
                                </div>
                                <div className="sequence-fields">
                                  <Textarea
                                    id={`seq-${i}-${k}`}
                                    value={seq.sequence}
                                    placeholder="粘贴标准大写氨基酸序列，如 ACDEFGHIKLMNPQRSTVWY"
                                    spellCheck={false}
                                    onChange={(e) =>
                                      change((d) => {
                                        d.items[i].input.sequences[k].sequence = e.target.value
                                      })
                                    }
                                  />
                                  <div className="copies-field">
                                    <Label htmlFor={`copies-${i}-${k}`}>Copies</Label>
                                    <TextInput
                                      id={`copies-${i}-${k}`}
                                      type="number"
                                      min={1}
                                      max={5000}
                                      value={seq.copies}
                                      onChange={(e) =>
                                        change((d) => {
                                          d.items[i].input.sequences[k].copies =
                                            Number(e.target.value) || 0
                                        })
                                      }
                                    />
                                  </div>
                                </div>
                              </div>
                            ))}
                          </div>
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            className="add-chain"
                            onClick={() =>
                              change((d) =>
                                d.items[i].input.sequences.push({ sequence: '', copies: 1 }),
                              )
                            }
                          >
                            <Plus size={14} /> 添加序列到此任务
                          </Button>
                        </section>
                      ))}
                    </fieldset>
                    {preview && (
                      <div className="preview-ok" role="status">
                        <ShieldCheck size={17} />
                        {draft.items.length} 个任务校验通过 · 尚未提交
                      </div>
                    )}
                    {draft.locked && (
                      <div className="locked-note">
                        <LockKeyhole size={16} />{' '}
                        本批输入已锁定；未知结果请使用“确认原批次结果”，不要新建重复任务。
                      </div>
                    )}
                    <div className="submit-bar">
                      <div>
                        <strong>{draft.items.length} 个独立任务</strong>
                        <span>{totalResidues.toLocaleString()} 个残基 · 计入 copies</span>
                      </div>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          id="preview"
                          disabled={busy || draft.locked}
                          onClick={previewBatch}
                        >
                          校验预览
                        </Button>
                        <Button id="submit" disabled={busy} onClick={submit}>
                          {busy ? (
                            <Loader2 className="animate-spin" size={16} />
                          ) : (
                            <Send size={15} />
                          )}{' '}
                          {draft.locked ? '确认原批次结果' : '确认批量提交'}
                        </Button>
                      </div>
                    </div>
                  </CardContent>
                </Card>
                <aside className="guide">
                  <div className="guide-icon">
                    <FileJson size={23} />
                  </div>
                  <div className="eyebrow">A SMALL GUIDE</div>
                  <h3>
                    一批任务，
                    <br />
                    清晰有序。
                  </h3>
                  <ol>
                    <li>
                      <span>01</span>
                      <div>
                        <strong>创建或导入</strong>
                        <p>手动添加多个任务，也可一次选择多个 JSON 文件。</p>
                      </div>
                    </li>
                    <li>
                      <span>02</span>
                      <div>
                        <strong>核对分组</strong>
                        <p>同一任务的序列一起预测，不会拆成独立任务。</p>
                      </div>
                    </li>
                    <li>
                      <span>03</span>
                      <div>
                        <strong>统一提交</strong>
                        <p>校验后一次入队，再按可用账号与浏览器容量执行。</p>
                      </div>
                    </li>
                  </ol>
                  <Separator />
                  <p className="guide-foot">
                    <ShieldCheck size={16} /> 请求标识自动生成
                    <br />
                    支持同批次安全重试
                  </p>
                  <details className="format-help">
                    <summary>支持哪些 JSON？</summary>
                    <p>
                      本地 input 对象或数组；AlphaFold Server version 1 数组（proteinChain /
                      count）。modelSeeds 必须为空数组；不支持
                      DNA、RNA、配体及修饰字段，不会静默丢弃。
                    </p>
                    <pre>
                      {JSON.stringify(
                        [{ name: 'complex-1', sequences: [{ sequence: 'TEACHINGS', copies: 1 }] }],
                        null,
                        2,
                      )}
                    </pre>
                  </details>
                </aside>
              </div>
              <Card className="jobs-card">
                <CardHeader className="section-header">
                  <div>
                    <CardTitle>
                      任务记录{' '}
                      <span className="text-muted-foreground font-normal text-sm ml-2">
                        {jobs.length}
                      </span>
                    </CardTitle>
                    <CardDescription>
                      {admin ? '管理员可以查看所有成员的任务。' : '仅本人和管理员可查看这些任务。'}{' '}
                      每 10 秒刷新。
                    </CardDescription>
                  </div>
                  <TextInput
                    className="search-input"
                    aria-label="搜索任务"
                    placeholder="搜索名称、ID 或状态…"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>任务名称</TableHead>
                        <TableHead>序列 / 残基</TableHead>
                        <TableHead>状态</TableHead>
                        <TableHead>创建时间</TableHead>
                        <TableHead className="text-right">结果</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody id="jobs">
                      {shownJobs.map((j) => (
                        <TableRow key={j.id}>
                          <TableCell>
                            <strong className="job-title">{j.input.name}</strong>
                            <div className="mono-id" title={j.id}>
                              {j.id.slice(0, 12)}…
                              {admin
                                ? ' / ' +
                                  (users.find((u) => u.id === j.owner)?.name || j.owner.slice(0, 8))
                                : ''}
                            </div>
                          </TableCell>
                          <TableCell>
                            {j.input.sequences.length} 条 /{' '}
                            {j.input.sequences.reduce(
                              (n, s) => n + s.sequence.length * s.copies,
                              0,
                            )}
                          </TableCell>
                          <TableCell>
                            <Status value={j.stage} />
                            {j.error && <div className="job-error">{j.error}</div>}
                          </TableCell>
                          <TableCell className="date-cell">
                            {new Date(j.created_at).toLocaleString('zh-CN', {
                              month: '2-digit',
                              day: '2-digit',
                              hour: '2-digit',
                              minute: '2-digit',
                            })}
                          </TableCell>
                          <TableCell className="text-right">
                            {j.stage === 'succeeded' ? (
                              <Button variant="outline" size="sm" asChild>
                                <a href={`/api/v1/jobs/${j.id}/download`}>
                                  <Download size={14} />
                                  ZIP
                                </a>
                              </Button>
                            ) : j.remote_id &&
                              ['submission_unknown', 'draft_attention', 'pending'].includes(
                                j.stage,
                              ) ? (
                              <Button
                                size="sm"
                                variant="ghost"
                                disabled={busy}
                                onClick={() =>
                                  void perform(async () => {
                                    await api(`jobs/${j.id}/check`, 'POST', {})
                                    await refresh()
                                  })
                                }
                              >
                                只读核对
                              </Button>
                            ) : (
                              <span className="text-muted-foreground">—</span>
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  {shownJobs.length === 0 && (
                    <div className="empty-state">
                      <Layers3 size={25} />
                      <strong>{jobs.length ? '没有匹配的任务' : '准备好第一次探索了吗？'}</strong>
                      <p>
                        {jobs.length
                          ? '试试其他关键词。'
                          : '在上方创建任务，提交后即可在这里查看进度和结果。'}
                      </p>
                    </div>
                  )}
                </CardContent>
              </Card>
            </>
          )}
          {view === 'accounts' && admin && (
            <>
              <div className="metrics metrics-three">
                <Metric
                  title="已接入账号"
                  value={accounts.length}
                  icon={<Database />}
                  foot="配置与会话保存在服务端"
                />
                <Metric
                  title="已查询剩余额度"
                  value={accounts.reduce(
                    (n, a) => n + (a.enabled && a.status === 'ready' ? a.remaining || 0 : 0),
                    0,
                  )}
                  icon={<Activity />}
                  foot="以实际提交前的平台返回为准"
                />
                <Metric
                  title="活跃浏览器"
                  value={`${meta?.browser_pool.active ?? 0} / ${meta?.browser_pool.limit ?? '—'}`}
                  icon={<Layers3 />}
                  foot="休眠不会删除账号配置"
                />
              </div>
              <Alert className="mb-6">
                <ShieldCheck />
                <AlertDescription>
                  Google
                  认证在服务器的普通浏览器完成。完成后关闭该账号全部认证窗口，再点“完成登录并检查”。团队成员无需重新登录
                  Google。
                </AlertDescription>
              </Alert>
              <Card>
                <CardHeader>
                  <CardTitle>账号配置池</CardTitle>
                  <CardDescription>
                    仅管理员管理 Google 认证；遵守平台额度与使用政策。
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <form
                    className="inline-form"
                    onSubmit={(e) => {
                      e.preventDefault()
                      const form = e.currentTarget,
                        f = new FormData(form)
                      void perform(async () => {
                        await api('admin/accounts', 'POST', {
                          id: f.get('id'),
                          label: f.get('label'),
                        })
                        form.reset()
                        await refresh()
                        notify('账号已添加，请完成 Google 认证。')
                      })
                    }}
                  >
                    <TextInput
                      name="id"
                      aria-label="账号标识"
                      placeholder="账号标识，如 research-01"
                      required
                      pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,63}"
                    />
                    <TextInput
                      name="label"
                      aria-label="账号备注"
                      placeholder="备注，如研究组公用账号"
                      maxLength={160}
                    />
                    <Button disabled={busy}>
                      <Plus size={15} /> 添加账号
                    </Button>
                  </form>
                  <div className="account-list">
                    {accounts.map((a) => (
                      <div className="account-row" key={a.id}>
                        <div className="account-info">
                          <div className="account-avatar">
                            <Database size={19} />
                          </div>
                          <div>
                            <strong>{a.label || a.id}</strong>
                            <span>
                              {a.id} ·{' '}
                              {instances.find((x) => x.account_id === a.id)?.login_window
                                ? '认证窗口开启'
                                : instances.some((x) => x.account_id === a.id)
                                  ? '浏览器活跃'
                                  : '浏览器休眠'}
                              {!a.enabled ? ' · 已停用' : ''}
                            </span>
                          </div>
                        </div>
                        <div>
                          <Status value={a.status} />
                          <p className="helper mt-2">剩余额度 {a.remaining ?? '未查询'}</p>
                          {a.last_error && <p className="job-error">{a.last_error}</p>}
                        </div>
                        <div className="account-actions">
                          {[
                            ['login', 'Google 登录'],
                            ['finish-login', '完成登录并检查'],
                            ['refresh', '检查额度'],
                            ['sleep', '休眠'],
                            [a.enabled ? 'disable' : 'enable', a.enabled ? '停用' : '启用'],
                          ].map(([action, label]) => (
                            <Button
                              key={action}
                              size="sm"
                              variant={action === 'login' ? 'outline' : 'ghost'}
                              disabled={busy}
                              onClick={() => accountAction(a.id, action)}
                            >
                              {label}
                            </Button>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                  {accounts.length === 0 && (
                    <div className="empty-state">
                      <Database />
                      <strong>连接第一个共享账号</strong>
                      <p>添加配置后，由管理员手动完成一次 Google 认证。</p>
                    </div>
                  )}
                </CardContent>
              </Card>
            </>
          )}
          {view === 'members' && admin && (
            <Card>
              <CardHeader>
                <CardTitle>团队成员</CardTitle>
                <CardDescription>成员只看自己的任务；管理员可管理账号与所有任务。</CardDescription>
              </CardHeader>
              <CardContent>
                <form
                  className="inline-form"
                  onSubmit={(e) => {
                    e.preventDefault()
                    const form = e.currentTarget,
                      f = new FormData(form)
                    void perform(async () => {
                      await api('admin/users', 'POST', {
                        name: f.get('name'),
                        password: f.get('password'),
                        role: f.get('role'),
                      })
                      form.reset()
                      await refresh()
                      notify('成员已创建，请通过安全渠道告知初始密码。')
                    })
                  }}
                >
                  <TextInput name="name" placeholder="用户名" aria-label="新成员用户名" required />
                  <TextInput
                    name="password"
                    type="password"
                    placeholder="初始密码（至少 12 字符）"
                    aria-label="新成员密码"
                    minLength={12}
                    required
                    autoComplete="new-password"
                  />
                  <select name="role" aria-label="成员角色" className="native-select">
                    <option value="member">成员</option>
                    <option value="admin">管理员</option>
                  </select>
                  <Button disabled={busy}>
                    <Plus size={15} /> 创建成员
                  </Button>
                </form>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>用户</TableHead>
                      <TableHead>角色</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {users.map((u) => (
                      <TableRow key={u.id}>
                        <TableCell className="font-medium">
                          {u.name}
                          {u.id === me.id && (
                            <Badge variant="secondary" className="ml-2">
                              你
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell>{u.role === 'admin' ? '管理员' : '成员'}</TableCell>
                        <TableCell>{u.disabled ? '已停用' : '正常'}</TableCell>
                        <TableCell className="text-right">
                          {u.id !== me.id && (
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={busy}
                              onClick={() =>
                                void perform(async () => {
                                  await api('admin/users/' + u.id, 'POST', {
                                    disabled: !u.disabled,
                                  })
                                  await refresh()
                                })
                              }
                            >
                              {u.disabled ? '启用' : '停用'}
                            </Button>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
          {view === 'api' && (
            <div className="api-layout">
              <Card>
                <CardHeader>
                  <CardTitle>团队平台 API</CardTitle>
                  <CardDescription>
                    系统集成使用本平台 Token，不使用 Google Cookie。
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-5">
                  <div className="api-route">
                    <Badge variant="secondary">POST</Badge>
                    <code>/api/v1/jobs/batch</code>
                  </div>
                  <pre className="code-block">
                    {JSON.stringify(
                      {
                        items: [
                          {
                            request_id: 'stable-unique-id',
                            input: {
                              name: 'complex-1',
                              sequences: [{ sequence: 'TEACHINGS', copies: 1 }],
                            },
                          },
                        ],
                        execute: true,
                      },
                      null,
                      2,
                    )}
                  </pre>
                  <p className="helper">
                    Bearer Token 对应当前用户；提交、列表与下载均受成员权限约束。重试请保持相同
                    request_id 与内容。
                  </p>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => {
                      if (confirm('生成新 Token 会立即撤销旧 Token，确定继续？'))
                        void perform(async () => {
                          const r = await api<{ token: string }>('token', 'POST', {})
                          setToken(r.token)
                        })
                    }}
                  >
                    <LockKeyhole size={15} /> 生成 / 轮换 API Token
                  </Button>
                  {token && (
                    <div className="token-box">
                      <Label htmlFor="api-token">仅本次展示，请妥善保存</Label>
                      <TextInput
                        id="api-token"
                        value={token}
                        readOnly
                        onFocus={(e) => e.target.select()}
                      />
                      <p className="helper">离开此页面会清除展示，不会将明文保存到浏览器存储。</p>
                    </div>
                  )}
                </CardContent>
              </Card>
              <Card className="api-boundary">
                <CardHeader>
                  <CardTitle>AlphaFold Server 适配层</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <p>
                    上游平台负责身份、额度、预测与结果。本平台负责团队权限、批量队列、账号配置和有限浏览器调度。
                  </p>
                  <Separator />
                  <p>
                    内部协议不是本平台公开 API。Google
                    认证由管理员人工完成，不接收账号密码，也不会把会话下发给团队成员。
                  </p>
                  <p className="helper">提交结果不明时保留原任务并只读核对，不自动换号重复提交。</p>
                  <Button variant="outline" asChild>
                    <a href="https://alphafoldserver.com/" target="_blank" rel="noreferrer">
                      打开 AlphaFold Server <ArrowUpRight size={14} />
                    </a>
                  </Button>
                </CardContent>
              </Card>
            </div>
          )}
          <footer className="page-footer">
            <span>FoldSpace · 让每一次探索更有序</span>
            <span>独立团队平台，非 Google 官方产品</span>
          </footer>
        </main>
      </div>
    </div>
  )
}
function Metric({
  title,
  value,
  icon,
  foot,
}: {
  title: string
  value: string | number
  icon: React.ReactNode
  foot: string
}) {
  return (
    <Card className="metric">
      <CardContent>
        <div className="metric-label">
          {title}
          <span>{icon}</span>
        </div>
        <div className="metric-value">{value}</div>
        <p>{foot}</p>
      </CardContent>
    </Card>
  )
}
