# 团队平台 API 调用指南

本文介绍如何从脚本或业务系统调用 FoldSpace：获取平台 Token、校验输入、单任务或批量入队、查询进度并下载结果。示例使用**本平台 `/api/v1` 接口**，不是 AlphaFold Server 的官方公共 API。

## 1 调用前准备

1. 启动服务并完成首次网页管理员初始化，无需运行 `init`。已有用户数据直接登录。
2. 管理员在「共享账号池」接入合法使用的账号，在**服务器普通浏览器**完成 Google 认证，关闭认证窗口后点「完成登录并检查」。实际预测需要有效会话、可用额度和浏览器容量。
3. 管理员为调用方创建独立平台用户。用户登录本平台，在「接口与凭据」生成 API Token。Token 绑定该用户，成员只能读取自己的任务，管理员可读全部。
4. 设置服务器地址与 Token。默认本地地址为 `http://127.0.0.1:8788`；团队远程调用使用 HTTPS，域名须与服务 `-public-url` 一致。

请求入队不要求实时存在可用账号；没有可用账号时会等待。HTTP 202 **只表示本平台接收任务，不代表上游已接受，更不代表预测完成**。

### 凭据与公共请求头

```http
Authorization: Bearer TOKEN
Content-Type: application/json
```

- Token 只在生成时返回明文；轮换立即撤销旧 Token，停用用户也会撤销访问权限。
- 用环境变量或密钥管理注入 Token，不放在 URL、代码仓库、截图、调试日志中。
- Bearer 方式不需要 Cookie 和 CSRF。不要同时发送浏览器 Cookie，以免混用身份。
- 服务端脚本通常不发送 Origin；如果发送，必须与 `-public-url` 完全一致。跨站网页请求不被允许，不能把这个服务当作任意来源可调用的浏览器公共 API。
- Google 会话只保存在服务端 profile 中，团队客户端不需要 Google 密码、Cookie 或 Token。会话失效后由管理员重新认证，不支持绕过 MFA。

以下命令从仓库根目录执行。`AF_PLATFORM_URL` 不带末尾斜杠；`AF_PLATFORM_TOKEN` 已通过安全方式设置。Linux 示例需要 curl，涉及 JSON 修改的示例需要 jq；Windows 使用 PowerShell 自带的 HTTP 命令。

## 2 单任务提交与多序列分组

接口：`POST /api/v1/jobs`。可直接复制 [`examples/api/single-request.json`](../examples/api/single-request.json)：

```json
{
  "request_id": "example-single-001",
  "input": {
    "name": "API complex example",
    "sequences": [
      {"sequence": "TEACHINGS", "copies": 1},
      {"sequence": "ACDEFGHIK", "copies": 2}
    ]
  },
  "execute": false
}
```

这里两条序列组成**一个预测任务**；第二条有两个 copies，不会被拆为三个任务。

| 字段 | 含义与限制 |
|---|---|
| `request_id` | 调用方生成的稳定幂等标识，1–64 字符，首字符为英文或数字，其余允许英文、数字、`_`、`-`；推荐 UUID |
| `input.name` | 可编辑任务名，最长 80 个 Unicode 字符；为空时服务补当前服务器时间。客户端最好显式填写固定名称 |
| `input.sequences` | 至少一条蛋白序列，按顺序保留 |
| `sequence` | 20 种标准**大写**氨基酸；移除空白后校验，不接受 FASTA 标题、小写、X、B、Z 等非支持字符 |
| `copies` | 建议显式填写正整数 1–5000；兼容省略/0 时补 1；不是批量任务数量 |
| `execute` | false 或省略只校验；true 才入队 |

每条序列最长 5000 残基，单任务总残基数 `sum(length(sequence) × copies)` 不超过 5000。当前只支持蛋白、自动 seed；不支持手动 seed、DNA/RNA、配体等。未知字段会报错，不会静默忽略。

### 先校验

```bash
export AF_PLATFORM_URL='http://127.0.0.1:8788'
# AF_PLATFORM_TOKEN 应已注入环境；不要在共享终端记录明文。
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer ${AF_PLATFORM_TOKEN:?请先设置平台Token}" \
  -H 'Content-Type: application/json' \
  --data-binary @examples/api/single-request.json \
  "$AF_PLATFORM_URL/api/v1/jobs"
```

成功为 HTTP 200，返回 `stage:"validated"`、`no_submission:true` 与规范化 `input`。不会创建任务、唤醒浏览器或预留额度；校验通过不等于稍后一定能入队。

### 再明确提交

创建一次业务请求后，把**完整请求体与 request_id 保存到本地或业务数据库**；提交和重试都复用它。样例中的固定标识只是示例：不同业务任务必须换新标识，同一个业务任务重试不得换标识。

```bash
umask 077
mkdir -p private/api-requests
# 先复制样例并编辑为此次业务唯一的 request_id、name 和序列。
# 不要覆盖已有未确认请求；此文件由你为本次业务事先保存。
cp -n examples/api/single-request.json private/api-requests/single.json
jq '.execute = true' private/api-requests/single.json > private/api-requests/single-submit.json

curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AF_PLATFORM_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @private/api-requests/single-submit.json \
  "$AF_PLATFORM_URL/api/v1/jobs"
```

HTTP 202 返回一个 Job 对象。以下为字段节选，不是某次真实预测结果：

```json
{
  "id": "JOB_ID",
  "owner": "USER_ID",
  "request_id": "example-single-001",
  "stage": "queued",
  "input": {
    "name": "API complex example",
    "sequences": [
      {"sequence": "TEACHINGS", "copies": 1},
      {"sequence": "ACDEFGHIK", "copies": 2}
    ]
  }
}
```

保存返回的 `id`，用于查询和下载。返回还可能包含 `input_hash`、`account_id`、`remote_id`、`seed`、`models`、`created_at`、`updated_at`、`error` 等；时间字段是 Unix 毫秒。seed 在任务实际处理时生成，不要把刚入队时的默认值当作最终种子。

### Windows PowerShell 示例

```powershell
$base = 'http://127.0.0.1:8788'
if (-not $env:AF_PLATFORM_TOKEN) { throw '请先设置 AF_PLATFORM_TOKEN' }
$headers = @{ Authorization = "Bearer $env:AF_PLATFORM_TOKEN" }

# 这一段只执行一次：生成本次业务请求并保存，后续重试不要重新生成 UUID。
$request = Get-Content examples/api/single-request.json -Raw | ConvertFrom-Json
$request.request_id = [guid]::NewGuid().ToString()
$request.input.name = Get-Date -Format 'yyyy-MM-dd_HH:mm'
$request.execute = $false
New-Item -ItemType Directory -Force private/api-requests | Out-Null
$requestFile = "private/api-requests/$($request.request_id).json"
$request | ConvertTo-Json -Depth 12 | Set-Content -Encoding utf8 $requestFile

# 校验（不提交）
$body = Get-Content $requestFile -Raw
Invoke-RestMethod "$base/api/v1/jobs" -Method Post -Headers $headers `
  -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($body))

# 确认后将同一文件改为 execute:true，再保存后发送。
$request.execute = $true
$request | ConvertTo-Json -Depth 12 | Set-Content -Encoding utf8 $requestFile
$body = Get-Content $requestFile -Raw
$job = Invoke-RestMethod "$base/api/v1/jobs" -Method Post -Headers $headers `
  -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($body))
$job.id
```

若最后一步超时，直接重新读取 `$requestFile` 并发送，不重新执行 UUID 生成代码。Windows 文件包含敏感研究输入时，请为 `private/` 设置仅本人和管理员可访问的 ACL。

## 3 批量提交

接口：`POST /api/v1/jobs/batch`。样例：[`examples/api/batch-request.json`](../examples/api/batch-request.json)。

```json
{
  "items": [
    {"request_id": "example-batch-001", "input": {"name": "Complex A", "sequences": [{"sequence": "TEACHINGS", "copies": 1}, {"sequence": "ACDEFG", "copies": 2}]}},
    {"request_id": "example-batch-002", "input": {"name": "Protein B", "sequences": [{"sequence": "ACDEFGHIK", "copies": 1}]}}
  ],
  "execute": false
}
```

每个 item 是一个独立任务，每个任务内部的 sequences 是一组。每批 1–50 项，**每项 request_id 必填**，同一批次不允许重复。

```bash
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AF_PLATFORM_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @examples/api/batch-request.json \
  "$AF_PLATFORM_URL/api/v1/jobs/batch"
```

`execute:false` 返回 200 `{items:[...],no_submission:true}`。确认每项标识和内容后，将保存的请求文件中 execute 改为 true，发送至相同接口，成功返回 202 `{jobs:[...]}`，结果顺序对应输入顺序。

本地持久化入队是**全有或全无**：任一项无效、幂等冲突或超出队列上限，则本次调用没有新增任务。已经存在的旧任务不受影响。远端预测仍逐个异步执行，可能部分成功、部分失败，不是跨多个远端任务的事务。

错误可能返回 `{"error":"INVALID_SEQUENCE","index":1}`；index 从 0 开始。全批请求体错误没有 index。队列上限为每个用户 50 个未结束任务、全平台 1000 个。

## 4 多 JSON 文件导入

接口：`POST /api/v1/import`。样例：[`examples/api/import-request.json`](../examples/api/import-request.json)。此接口接收 JSON，不接收 multipart 文件上传。

```json
{"files":[{"name":"input.json","content":{"name":"Imported protein","sequences":[{"sequence":"TEACHINGS","copies":1}]}}]}
```

`content` 是解析后的 JSON 对象或数组，不是 JSON 字符串、文件路径或 Base64。可一次传多个 files，各文件可包含多个任务。

支持：

- 本平台 `{name,sequences:[{sequence,copies}]}` 对象或其数组。
- AlphaFold Server version 1 的蛋白 `proteinChain:{sequence,count}` 数组；`modelSeeds` 必须为空数组。实际平台导出的其他版本或含模板/修饰/非空 seeds 的文件不保证可直接导入，会明确拒绝。

成功返回：

```json
{"items":[{"source":"input.json","index":0,"input":{"name":"Imported protein","sequences":[{"sequence":"TEACHINGS","copies":1}]}}],"no_submission":true}
```

**导入永远不创建任务。** 取每项 input，为每项生成并保存 request_id，构造成上一节的批量请求后再校验/确认提交。任何文件失败返回 400 `IMPORT_REJECTED` 和 `issues:[{source,index,code}]`，不返回部分成功项。

文件数及全部文件合计任务数均最多 50，整个 HTTP JSON 请求体最大 1 MiB；网页端还限制原文件合计 900 KB。filename 只是来源标记，不会被当作服务器文件路径。

## 5 查询与下载

```bash
# 本人任务列表（管理员看全体）；响应是 Job 数组，目前无分页。
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AF_PLATFORM_TOKEN" "$AF_PLATFORM_URL/api/v1/jobs"

# 单任务详情：将 JOB_ID 替换为提交返回的 id。
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $AF_PLATFORM_TOKEN" "$AF_PLATFORM_URL/api/v1/jobs/JOB_ID"

# 仅 succeeded 后下载。先写临时文件，HTTP 失败不把错误 JSON 当成 ZIP。
curl --fail --silent --show-error \
  -H "Authorization: Bearer $AF_PLATFORM_TOKEN" \
  --output result.zip.part "$AF_PLATFORM_URL/api/v1/jobs/JOB_ID/download" \
  && mv result.zip.part result.zip
```

PowerShell 对应查询和下载：

```powershell
$detail = Invoke-RestMethod "$base/api/v1/jobs/$($job.id)" -Headers $headers
if ($detail.stage -eq 'succeeded') {
  Invoke-WebRequest "$base/api/v1/jobs/$($job.id)/download" -Headers $headers -OutFile 'result.zip.part'
  Move-Item -LiteralPath 'result.zip.part' -Destination 'result.zip'
}
```

建议每 10–30 秒查询一次，设置有限的总等待时间。超时停止客户端等待，不等于取消后台任务；本版没有公开取消或删除任务接口。下载会检查用户权限，成功结果按原任务账号获取或读取缓存；不将上游临时地址直接暴露给调用方。

| 状态 | 调用方应如何处理 |
|---|---|
| queued / waiting_for_account | 已入队或等待账号，继续查询，不重新创建 |
| preflight / creating / draft / submitting | 正在检查或提交，不另建重复任务 |
| pending | 上游预测中，继续查询 |
| succeeded | 可以下载 ZIP |
| failed / preflight_failed / cancelled | 查看 error，结束本次等待；新业务尝试需显式决定，不自动换 ID 重试 |
| submission_unknown / draft_attention | 结果不明或草稿待核对；保留原任务，不自动重新提交 |

对于已知 remote_id 且状态允许的任务，可 `POST /api/v1/jobs/JOB_ID/check`（请求体 `{}`）只读核对远端。未知 remote_id 时返回 `CANNOT_RECONCILE_WITHOUT_REMOTE_ID`，应由管理员在原账号网页核对。

## 6 幂等 重试与常见错误

幂等范围为 `(平台用户, request_id)`：相同内容重复调用返回同一个任务；相同标识配不同内容返回 409。换 Token 但还是同一平台用户不会绕过幂等；换用户则是另一个命名空间。

单任务接口允许省略 request_id 并由服务端生成，但网络超时可能拿不到它。因此系统集成应始终自己生成并持久化标识，批量接口必须提供。重试时保持名称、序列顺序、copies 和标识不变。

| HTTP / error | 处理方式 |
|---|---|
| 400 INVALID_JSON / JSON_REQUIRED | 检查 Content-Type、JSON、未知字段及 1 MiB 上限 |
| 400 INVALID_SEQUENCE / INVALID_TOTAL_SIZE | 修正输入后重新校验，不盲目重试 |
| 400 IMPORT_REJECTED | 查看 issues 的文件名、项索引和 code，导入未产生任务 |
| 403 FORBIDDEN | 检查 Token、用户是否停用、Origin/Host 是否匹配；Cookie 模式还需检查 CSRF |
| 404 NOT_FOUND | 路径/ID 错误，或当前用户无权读取该任务；不会泄露其他成员的任务 |
| 409 IDEMPOTENCY_CONFLICT | 同一标识已经对应其他内容；查原任务或修正调用逻辑 |
| 429 QUEUE_FULL | 等已有任务结束后重试原批次；不换账号/标识逃避限制 |
| 429 RATE_LIMITED | 登录/初始化或服务操作限流，退避后再试 |
| 网络超时、断连、响应无法解析 | 结果可能已入队；查询或使用原标识/原内容确认，不能断定提交失败 |

不要给提交请求配置“失败即换 request_id”的重试机制。后端对结果不明的上游写入不会自动重放或换号重复提交。

## 7 平台登录 API 与管理员接口

常规集成推荐从网页生成 Token；也可使用平台用户名/密码登录（**不是 Google 密码**）：

```http
POST /api/v1/session
Origin: http://127.0.0.1:8788
Content-Type: application/json

{"name":"USER","password":"PASSWORD"}
```

响应设置 HttpOnly 会话 Cookie，返回 `{user,csrf}`。带 Cookie 调用 `POST /api/v1/token` 时，必须同时发送准确的 Origin 和 `X-CSRF-Token: CSRF`，成功 201 返回 `{token,note}`。不要将 Cookie 或 token 响应保存在共享日志中；每次调用 token 接口都会撤销旧 Token。

`GET /api/v1/session` 查询会话；`DELETE /api/v1/session` 登出 Cookie 会话，不撤销已经生成的 API Token。服务重启会清空网页会话，但持久化的有效 API Token 不因重启自动失效。

用户密码为 12–256 字节。登录与初始化共用每 IP 每分钟 10 次限制；反向代理后可能按代理地址共享额度。

管理员可调用 `/admin/accounts`、`/admin/users` 和账号登录/检查动作；成员不能调用。字段和完整路由表见 [API 协议参考](GO_PLATFORM_API.zh-CN.md)。管理员 API 不接收 Google 登录密码，也不导出浏览器 Cookie。

## 8 验证示例与运维入口

`make test-api`（Windows 可用 `mingw32-make test-api`）实际执行本目录样例对应的 HTTP 请求，验证 Token、分组、批量幂等、导入、权限和下载。测试使用隔离本地 fixture，不登录 Google、不提交真实预测。

[Makefile 与运维指南](MAKEFILE_OPERATIONS.zh-CN.md) 说明编译、前端更新、服务管理和安全清理；[部署说明](GO_PLATFORM_DEPLOY.zh-CN.md) 说明 HTTPS、服务端 Google 认证和数据备份边界。
