# Go 团队平台 API（不是 AlphaFold 公共 API）

命名空间 `/api/v1`；旧 Node `/v1` 已归档，不是当前发布接口，也不直接兼容迁移。Google/AlphaFold 上游调用仍见 [上游 RPC](ALPHAFOLD_RPC.zh-CN.md)，不对团队客户端暴露 Cookie、Token、CDP 地址或 `batchexecute` 代理。

## 登录与授权

`GET /api/v1/setup` 无需登录，返回 `{required,token_required}`，不返回凭据。首次启动自动生成数据目录下的 `setup-token.txt`；`POST /api/v1/setup` 提交 `{name,password,token}`，必须同源 Origin、JSON，并受登录限速保护。成功 201 创建管理员，随后使用 session 登录。已初始化或并发竞争失败返回 409 `ALREADY_INITIALIZED`；服务端事务只允许一个首次管理员。无参数运行等价于 `serve`，不再要求 CLI `init`。

`POST /api/v1/session`，同源 Origin + JSON `{"name":"USER","password":"PASSWORD"}`。返回 `user`、`csrf` 和 HttpOnly Cookie。后续 Cookie 写请求必须带 `Origin` 与 `X-CSRF-Token`；`GET /api/v1/session` 返回当前用户及 CSRF；`DELETE` 登出。用户名使用字母、数字、下划线、横杠，最多 64 字符；密码 12–256 字节。

`POST /api/v1/token` 生成并轮换本人的 API Token，旧 Token 即刻失效；只返回本次明文。系统集成使用 `Authorization: Bearer TOKEN`，不要将 Token 放在 URL。Bearer 请求不使用 Cookie CSRF，但仍拒绝跨源 Origin。管理员和成员权限都在服务器执行。

## 任务

```json
{
  "request_id": "CLIENT_GENERATED_UUID",
  "input": {
    "name": "2026-10-12_15:30",
    "sequences": [
      {"sequence": "TEACHINGS", "copies": 1},
      {"sequence": "ACDEFG", "copies": 2}
    ]
  },
  "execute": false
}
```

`POST /api/v1/jobs`：`execute:false` 只做本地校验，不唤醒浏览器，不检查实时额度，不创建任务；`true` 持久入队（HTTP 202）。每条序列是标准大写蛋白序列，整组计入 copies 后最多 5000 残基。实体顺序保留，不拆任务；不支持 DNA/RNA/配体。

网页自动生成并保存 UUID，名称默认当前本地时间，可编辑。API 调用建议自己固定 `request_id` 和名称后重试；服务会补缺失请求标识，但网络中断时客户端可能拿不到它，无法保证后续无标识请求不重复。幂等键为 `(平台用户, request_id)`，相同输入返回原任务，不同输入返回 409。JSON 未知字段会拒绝；不能传入手动 seed。

| 接口 | 语义 |
|---|---|
| GET `/jobs` | 成员仅本人；管理员全部 |
| GET `/jobs/{id}` | 同上，其他成员的 ID 返回 404 |
| POST `/jobs/{id}/check` | 仅已知 remote ID 且 pending/unknown/draft_attention 的只读核对，绝不重新提交 |
| GET `/jobs/{id}/download` | 成功任务 ZIP，先执行 owner/admin 校验，再读取缓存或原账号下载 |
| GET `/meta` | 平台边界、任务可见策略及浏览器活跃数/上限，不返回配置身份 |

状态：`queued → waiting_for_account/preflight → creating → draft → submitting → pending → succeeded/failed`。`preflight_failed` 在写入前失败；`submission_unknown` 表示远端写入可能已生效，不自动重试。重启恢复继续待处理队列和 pending 查询，但不重放不明写入。

## 批量与文件导入

`POST /api/v1/jobs/batch`：

```json
{"items":[{"request_id":"unique-stable-id-1","input":{"name":"complex-1","sequences":[{"sequence":"TEACHINGS","copies":1},{"sequence":"ACDEFG","copies":2}]}},{"request_id":"unique-stable-id-2","input":{"name":"protein-2","sequences":[{"sequence":"ACDEFGHIK","copies":1}]}}],"execute":false}
```

- 每批 1–50 项，每项必须有稳定 request_id；同批重复标识拒绝。顺序保留，一项一个任务；其 sequences 全部作为同组。
- `execute:false` 返回 200 `{items,no_submission:true}`，只做输入校验，不预留额度/队列位置，不代表最终可入队。
- `execute:true` 成功返回 202 `{jobs:[...]}`；单个 Store 事务检查用户、所有输入、幂等冲突和队列上限，然后整体提交。失败无部分创建；错误 `{error,index}` 的 index 从 0 开始。
- 幂等范围是 owner + request_id；相同标识和内容返回原 Job，内容改变返回 409。不同用户可用相同标识但不能读取对方任务。
- 原子性仅限本平台持久队列，不是远端预测事务。成功入队后每个任务独立执行、返回结果。

`POST /api/v1/import`：`{"files":[{"name":"batch.json","content":[...]}]}`。content 是 JSON 值，不是 Base64/字符串。支持本地 Input 对象/数组、AlphaFold Server version 1 的 proteinChain/count；后者 modelSeeds 必须为空。成功 200 `{items:[{source,index,input}],no_submission:true}`。文件只转成校验后输入，不分配账号或提交。任意项失败则 400 `{error:"IMPORT_REJECTED",issues:[{source,index,code}]}`，不返回部分成功输入。未知字段、修饰、DNA/RNA、配体、种子等不支持内容明确报错。

整个请求体最大 1 MiB；文件数与合计任务数最多 50；UI 预先限制原文件总大小 900 KB。名称、序列、copies 沿用单任务规则。filename 仅用于错误显示，不写文件系统。导入、预览、执行均需团队认证与 CSRF/Token。

## 管理员

| 接口 | 输入 / 用途 |
|---|---|
| GET/POST `/admin/users` | 列表；创建 `{name,password,role:"member"或"admin"}` |
| POST `/admin/users/{id}` | `{disabled:true/false}`，禁止停用自己；撤销目标会话及 Token |
| GET/POST `/admin/accounts` | 配置列表/浏览器池；新增 `{id,label}` |
| POST `/admin/accounts/{id}/login` | 打开服务器专属 profile 的普通人工认证窗口（无 CDP/WebDriver） |
| POST `/admin/accounts/{id}/finish-login` | 管理员认证并关闭全部认证窗口后，用同一 profile 单独检查会话/额度；窗口尚未关闭返回 `LOGIN_WINDOW_OPEN`；检查实例结束后关闭 |
| POST `/admin/accounts/{id}/refresh` | 按需无头打开并只读检查额度（不能替代打开着的登录窗口） |
| POST `/admin/accounts/{id}/sleep` | 关闭空闲浏览器，不删会话或配置 |
| POST `/admin/accounts/{id}/enable` 或 `/disable` | 启停新任务调度；不取消已提交远端任务 |
| GET `/admin/audit` | 最多最近 10000 条管理与提交事件，无密码/Token |

账号最后已知额度不是保证仍有效的额度，提交前重新验证；暂停、休眠、连接失败和明确需要登录分别处理。不提供绕过 Google 验证或上游限流的接口。日志及测试不会打印凭据。
