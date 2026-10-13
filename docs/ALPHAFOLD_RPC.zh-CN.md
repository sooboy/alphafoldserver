# AlphaFold Server 平台 RPC 协议

> 历史协议观察，日期与证据范围保留原义。旧 Node 工具已移入本机 `archive/node-prototype/`，本文旧脚本/证据路径仅用于历史追溯，不是现行入口，也不随 Git 发布。当前平台使用根目录 README 和 GO_PLATFORM_API.zh-CN.md。

调查日期：2026-10-12。目标站点：`https://alphafoldserver.com/`。
公开首页 build：`boq_deepmind-alphafoldserver-web_20261006.00_p0`。

## 当前实测结果：单任务全流程通过

2026-10-12，用户手动完成 Google 登录、输入普通蛋白 `TEACHINGS`（1 copy）后，授权自动测试。自动化只提交了 **1 个**任务 `af-protocol-draft-001`，没有批量运行。

| 阶段 | 实际观测 |
|---|---|
| 账号会话 | `GetUser/EU26qc` 成功；业务请求带 `at` 字段，Cookie/token 值未写入报告 |
| 配额与预检 | `GetFoldQuota/tjI49c`、`CalculateFoldCost/Gbje0` HTTP 200 且 RPC 成功 |
| 保存草稿 | `CreateFoldJob/kMnDgb` 一次，返回状态 **5** |
| 提交草稿 | `UpdateFoldJob/bteK7` 一次，同一任务进入状态 **1** |
| 轮询及详情 | `ListFoldJobs/IbBsAc` 观察到状态 **3**；`GetFoldJob/EXvXWc` 确认同一成功任务和 **5 个模型** |
| 额度变化 | **30 → 29** |
| 结果下载 | 模型 0 的 CIF、两种 JSON，以及整个 ZIP，均 HTTP 200、无重定向 |
| 包完整性 | ZIP 为 29,625 字节、19 个条目；大小、CRC32、SHA256、JSON/CIF 格式及独立文件一致性检查通过 |

pending 响应与首次观察成功相隔 **76.992 秒**，不是精确的服务器计算耗时。结果仅用于协议测试，不表示这条 9 残基序列的预测具有研究可靠性。

证据：`../evidence/e2e-verification.json`、`../evidence/test-downloads.json`、`../evidence/test-zip-manifest.json`。运行 `node scripts/verify-e2e.mjs` 可在本机离线重复核验，**不会重新提交**。实际任务 ID、浏览器会话、固定抓包快照和下载文件留在被 Git 忽略的 `private/capture/`。

### 已接受的关键请求

以下是 `f.req` 内序列化的业务 payload，不是最外层 batchexecute 封装。`JOB_NAME`、`SEQUENCE`、`JOB_ID` 替换了实际字符串，数字和数组层级来自成功请求。

保存草稿（`kMnDgb`）：

```json
[["JOB_NAME",[[[null,"SEQUENCE",null,null,null,null,1,1]]]],5]
```

提交同一草稿（`bteK7`）：

```json
[[["JOB_ID"],1,null,null,null,null,["JOB_NAME",[[[null,"SEQUENCE",null,null,null,null,1,1]]]],null,null,null,null,null,null,[2029956658]],[["fold_job_status","fold_input","model_seeds"]]]
```

详情查询（`EXvXWc`）：`[["JOB_ID"]]`；列表和额度请求：`[]`。本次 UI 未指定 seed，提交时前端生成 `2029956658`；不要把该样例 seed 硬编码为协议要求。蛋白模板开关在请求中为数字 `1`，响应中为 `true`。直接创建 pending 的分支及其它实体组合仍未实测。

### 实际导出格式与文件

本次 ZIP 中的 job_request 使用 **`dialect: "alphafoldserver", version: 3`**，包含字符串 seed、`TEACHINGS`、count 1 和 `useStructureTemplate: true`；完整原文保存在 `../evidence/test-exported-job.json`。这与先前官方 GitHub 文档样例中的 version 1 不同：**已验证导出 version 3，不等于已验证所有导入版本的兼容性**。

ZIP 内包含 5 个 `*_model_N.cif`、5 个 `*_summary_confidences_N.json`、5 个 `*_full_data_N.json`、一个 job_request JSON、两个 MSA 文件和 `terms_of_use.md`。模型 0 的独立下载与 ZIP 对应条目字节一致；两个矩阵均为 9×9。这些数量/尺寸只描述本次输入。

### 会话边界

这是**复用手动登录后的浏览器会话**完成的自动化，不是 Google 密码自动登录，也不是已完成的独立 Go HTTP 客户端。Google 重新认证/MFA 仍由用户处理。

本机只记录到 AlphaFold 域适用的 Cookie 元数据：`NID`、`__Secure-OSID`、`OSID`，均 Secure/HttpOnly，Path 为 `/`；只有 `__Secure-OSID` 明确返回 SameSite=None，其余未返回该属性。详见 `../evidence/auth-cookie-metadata.json`。**存在不代表必需**，没有通过删 Cookie 测试最小凭据集合。完整登录重定向、会话过期、token 刷新、批量下载、自定义上传和错误/限流分支尚未验证。

## 登录后的增量验证（较早阶段记录）

> 最新的系统接入实现见本文末尾“账号池与 Google 登录接入（第一版）”；以下历史捕获结果保持原义。

2026-10-12，用户在独立 Edge 窗口手动完成 Google 认证及站点操作后，浏览器实际出现：

- `GetFoldQuota/tjI49c`：HTTP 200，业务成功；请求位置数组 `[]`，表单含 `at`（不记录值）；响应 `[[30,true]]`，与前端读取逻辑对应剩余 30、队列开放。
- `ListFoldJobs/IbBsAc`：HTTP 200，业务成功；请求 `[]`，表单含 `at`；本次响应 `[]`，历史列表为空。
- 登录前 `GetUser/EU26qc` 返回内部错误 16；认证过程中还捕获过内部错误 5，之后业务查询成功。该阶段尚未捕获成功 GetUser；后续在 03:22:04.654 UTC 已捕获成功。错误 5 的原因仍不能写成已确认事实。
- 中间有一个未映射 RPC 返回成功；采集器把未知 ID 脱敏为 UNKNOWN，因此不凭响应形状猜它的名称。

证据为 `../evidence/login-verification.json`，保留当时额度 30、历史为空的结果，不用较晚状态覆盖。后续保存、提交、状态和私有下载已按上节验证。`protocol.json` 保留原始静态和匿名观测，并追加 `authenticated_observation` 与 `live_verification`。

## 结论和证据等级

网页主要通过 `POST /_/AlphaFoldUi/data/batchexecute` 调用内部 RPC，结果文件另走 HTTP GET。官方公开的任务导入 JSON 与 RPC 的位置数组是两种不同格式，不能直接把导入文件 POST 到 RPC 地址。

- **实测**：真实 HTTP 请求及浏览器操作得到结果；公开阶段在 `evidence/public-probe/`，账号阶段在 `evidence/e2e-verification.json` 等记录。
- **静态确认**：从本次官网下载的 JavaScript 明确找到标识或调用代码，尚不代表登录后的服务器契约已验证。
- **推导**：依据字段号和构造代码还原的请求结构，需要 HAR 对照。
- **待验证**：尚未观察到对应操作，不能由相邻步骤成功推定。

方法名、RPC ID、证据文件、字符偏移和哈希另见 `protocol.json` 及 `../evidence/source-index.json`。本文件没有声称存在官方支持的、稳定的开放 API。

## 全流程顺序

```text
打开网页 → /signin → Google 交互登录 → 返回 AlphaFold Server
  → GetUser → 必要时手动补全资料和接受条款
  → GetFoldQuota → 组装 FoldInput → CalculateFoldCost
  → 可选上传自定义 MSA 或模板
  → CreateFoldJob（直接运行或保存草稿）
    或 UpdateFoldJob（把已有草稿提交运行）
  → ListFoldJobs / GetFoldJob 轮询
  → pending → succeeded / failed
  → model.cif / full_data.json / summary_confidences.json / fold.zip
```

其中“手动 Google 登录 → 草稿 → 提交 → 成功 → 私有文件下载”的单任务路径已完成。可选上传、直接创建运行、失败和批量分支不在本次成功范围内。

## 登录和会话

| 操作 | 已知内容 | 证据 |
|---|---|---|
| 登录入口 | `GET /signin`，本次跟随跳转得到 Google Accounts 登录页面 | 实测 HTML 标题为 `Sign in - Google Accounts` |
| 返回站点 | 登录页出现 `continue=https://alphafoldserver.com/signin`，`followup` 同指此地址 | 静态 HTML；完整重定向链待抓包 |
| 用户检查 | `UserService.GetUser`，RPC ID `EU26qc`，空请求 | 匿名错误 16；登录后成功已实测 |
| 首次使用 | 前端包含 `/login`、`/terms-of-service` 路由，未完成用户状态会进入资料流程 | 静态；不能把所有路由当成登录回调 |
| 资料更新 | `UserService.UpdateUser`，`Lg5BMe`，用户消息加更新掩码 | 静态；不自动代用户接受条款 |
| 退出 | `/accounts/ClearOSID?continue=.../welcome` | 静态；本次未执行 |

前端 bootstrap 的 `oPEP7c` 被用于登录状态判断。通用 RPC 代码读取 `SNlM0e` 作为 XSRF token，以表单字段 `at` 发送；`FdrFJe` 对应 `f.sid`，`cfb2h` 对应 `bl`。匿名首页没有可用于账号调用的 `SNlM0e`；不要把匿名值或示例值硬编码为认证凭据。

**已观察**：登录后业务请求含 `at`；浏览器内重放列表和下载时复用同源会话。Cookie 名称和属性见上节。**待验证**：最小必需 Cookie 集合、失效时机、token 更新及最小请求头。尚无证据表明这是 OAuth Bearer API；不能凭空改成 `Authorization: Bearer TOKEN`。

实现建议：先让用户在专用浏览器配置中手动完成 Google 登录/MFA，然后仅在本机复用其会话。不要记录密码，遇到验证码或重新认证就交还用户。Cookie 和 `at` 的值不进入日志或版本库。

## RPC 传输层

### 请求封装

本次匿名探测实际使用：

```http
POST /_/AlphaFoldUi/data/batchexecute?rpcids=RPC_ID&source-path=%2F&bl=BUILD&f.sid=PAGE_SESSION&hl=en&_reqid=REQUEST_ID&rt=c
Content-Type: application/x-www-form-urlencoded;charset=UTF-8
X-Same-Domain: 1
Origin: https://alphafoldserver.com
Referer: https://alphafoldserver.com/
```

表单的 `f.req` 在 URL 编码前为：

```json
[[["RPC_ID","SERIALIZED_PAYLOAD",null,"1"]]]
```

`SERIALIZED_PAYLOAD` 是再次 JSON 序列化后的字符串，而非直接嵌入的对象；`"1"` 是本次请求关联标识示例，不是鉴权信息。已登录请求可能还需 `at=TOKEN` 和同源 Cookie。`authuser`、其他参数及各参数必需性尚待实测，不应认为这里列出的都是强制项。

公开示例调用的完整未编码 `f.req`：

```json
[[["vyuJrc","[[\"examplefold_pdb_1aay\"]]",null,"1"]]]
```

代码中的 `/FoldService.CreateFoldJob` 等字符串是 RPC 方法元数据，**不是已确认可以直接 POST 的 REST 路径**。

### 响应解码

本次响应以 `)]}'` 开头，随后是长度标记和 JSON 数据帧。业务数据在 `wrb.fr` 中又是一层 JSON 字符串。必须按响应关联标识/RPC ID 分发，不能假设第一个数组就是任务。

匿名 `GetUser` 实测的业务记录：

```json
["wrb.fr","EU26qc",null,null,null,[16],"1"]
```

HTTP 为 **200**，但内部状态码 **16**。前端遇到此码会跳转 `/signin`。所以 HTTP 200 不等于登录成功或业务成功。

公开示例 `GetExampleFoldJob` 实测返回有效位置数组，任务标识为 `examplefold_pdb_1aay`，状态为 `6`，有 5 个 fold 引用。关联标识、追踪记录和帧长度见原始证据，不作为稳定业务字段。

`audit-protocol.mjs` 是针对现有文本帧的离线核验器，不是完整的流式 batchexecute 客户端。正式客户端还需覆盖分块、多 RPC、错误帧和 token 刷新。

## 核心 RPC 清单

以下 ID 均从当前公开前端提取；只有明确标为实测的调用已发出。

| 阶段 | 方法 | RPC ID | 说明 |
|---|---|---|---|
| 用户 | `UserService.GetUser` | `EU26qc` | 匿名错误 16；登录后成功 |
| 用户 | `UserService.UpdateUser` | `Lg5BMe` | 资料及条款相关更新，未调用 |
| 配额 | `FoldService.GetFoldQuota` | `tjI49c` | 实测 30→29，队列开放 |
| 预检 | `FoldService.CalculateFoldCost` | `Gbje0` | 本次预检实测成功；错误输入未测 |
| 创建 | `FoldService.CreateFoldJob` | `kMnDgb` | 保存状态 5 草稿实测成功；直接运行仅静态 |
| 更新 | `FoldService.UpdateFoldJob` | `bteK7` | 带 FieldMask 将草稿提交为状态 1 实测成功 |
| 批量更新 | `FoldService.UpdateFoldJobs` | `GNrSbe` | 前端批量归档等，不等于批量运行 |
| 列表 | `FoldService.ListFoldJobs` | `IbBsAc` | 实测空列表及本次任务状态变化 |
| 单任务 | `FoldService.GetFoldJob` | `EXvXWc` | 实测成功任务状态 3、输入及 5 个模型引用 |
| 多任务 | `FoldService.GetFoldJobs` | `uCiTI` | 存在声明；使用方式待验证 |
| 公共示例 | `FoldService.GetExampleFoldJob` | `vyuJrc` | 本次匿名实测成功 |
| 单 fold | `FoldService.GetFold` | `Tkddhb` | 存在声明，不等于主页面一定使用它 |
| 多 fold | `FoldService.GetFolds` | `yYrXS` | 存在声明 |
| 批量下载 | `FoldService.CreateDownload` | `YeS2Vb` | 为选中的任务建立打包下载 |
| 下载查询 | `FoldService.GetDownload` | `bWZJNb` | 存在声明；是否需要轮询待验证 |

扩展接口：`CreateMsa/rPXxMb`、`CreateTemplate/KNbI3`、`GetMsas/oYroVe`、`GetMsasWith/qqzLwf`、`GetTemplates/HIFwNc`、`GetCcdInfo/jX4pge`、`CountCustomMsasCreated/oDm7cc`、`CountCustomTemplatesCreated/pwXru`。上传路径还包括 `/_/upload/msa` 和 `/_/upload/template`。前端存在 Google upload 协议代码，不应假定是普通 multipart。上传初始化、数据传输、metadata、完成响应需要独立抓包。

生产站点使用同源路径。代码中的内部测试域名不是本项目目标，不应访问或写入生产配置。

## 请求和响应字段

以下数组位置使用 **0 基索引**；字段号等于索引加一。省略与 `null` 的准确序列化，以及扩展字段，仍以登录抓包为准。

| 消息 | 已确认或推导的字段 |
|---|---|
| `FoldJobId` | 索引 0：ID 字符串；公开示例实测为 `["examplefold_pdb_1aay"]` |
| `GetFoldJob` 请求 | 索引 0：`FoldJobId`，实测 `[["JOB_ID"]]` |
| `GetUser` 请求 | `[]`，匿名与登录后均实测 |
| `ListFoldJobs` 请求 | 实测 `[]`；服务端分页能力未知 |
| `GetFoldQuota` 请求 | 实测 `[]` |
| `CalculateFoldCost` 请求 | 索引 0：`FoldInput` |
| `CreateFoldJob` 请求 | 索引 0：`FoldInput`；1：目标状态；2：seeds 数组 |
| `UpdateFoldJob` 请求 | 索引 0：`FoldJob`；1：FieldMask 消息 |
| `FoldInput` | 索引 0：任务名；1：分子消息数组 |
| 蛋白实体 | 实体 oneof 索引 0；蛋白消息内 1：sequence；2：glycans；5：modifications；6：count；7：useStructureTemplate；8：日期消息；9：MSA IDs；10：template IDs |
| 其他实体 | 实体 oneof 索引 2：ligand；3：ion；4：DNA；5：RNA。RNA/DNA 子消息内 1：sequence；2：count |
| `FoldJob` 响应 | 索引 0：任务 ID 消息；1：状态；4：错误；5：单 fold 引用；6：FoldInput；7：fold 引用数组；11：archived；13：model_seeds。未列出的字段保留原值 |
| `CreateDownload` 请求 | 索引 0：下载消息；下载消息索引 0：`FoldJobId` 消息数组，即推导为 `[[[["JOB_A"],["JOB_B"]]]]` |

公开示例及本次任务响应验证了 ID、状态、输入、fold 引用和 seed 位置；错误和归档分支未实测。仅上节实际请求及证据中的数组已被服务器接受，其余示意组合不应当作完整验证过的编码器。

### 草稿与真正提交不是一回事

前端导入 JSON 调用 `CreateFoldJob` 时使用状态 **5**，只是保存草稿。直接运行使用状态 **1**。已有草稿运行走 `UpdateFoldJob`，更新掩码含 `fold_job_status`、`fold_input`，必要时含 `model_seeds`。

前端还会在部分未指定 seed 的运行分支生成随机 seed。因此“输入 JSON 中 `modelSeeds: []`”不意味着最终 RPC 必须携带空 seeds。

导入、保存草稿、点击运行三者在抓包时要分开记。自动化应先跑通一个最小普通蛋白任务，再扩展复杂输入。

## 官方任务导入格式

已保存的官方 GitHub 文档样例使用顶层数组，包含 `name`、`modelSeeds`、`sequences`、`dialect: "alphafoldserver"` 和 `version: 1`。对应 `examples/minimal-job.json` 仍作为历史格式样例保留，没有通过 Upload JSON 导入。**本次真实服务器导出为 version 3**，见 `evidence/test-exported-job.json`；勿将版本 1 硬编码为唯一有效版本，也不宣称已验证 version 3 的重新导入。

分子键包括 `proteinChain`、`dnaSequence`、`rnaSequence`、`ligand`、`ion`。种子建议用字符串数组或空数组；当前前端导入器限制最多 1 个 seed。蛋白修饰示例实际使用 `ptmPosition`，不要误写成普通 `position`。MSA 和自定义模板在导入时可能先上传，再把返回 ID 放入 RPC，不能原样平移所有字符串字段。

来源：[官方 Server JSON 文档](https://github.com/google-deepmind/alphafold/blob/main/server/README.md)。这不是本地 `alphafold3` 方言，不要混用本地推理程序的字段要求。

## 状态和轮询

| 值 | 前端名称 | 客户端处理建议 |
|---|---|---|
| 1 | `pending` | 继续查询；暂未发现分开的 queued/running 映射 |
| 3 | `succeeded` | 转入结果下载 |
| 4 | `failed` | 记录错误，停止正常轮询 |
| 5 | `draft` | 尚未运行，不应一直等待结果 |
| 6 | `example` | 公共示例，不是本次成功提交的任务 |
| 其他 | 当前映射未定义 | 保留原始数据，报告协议变化，不猜枚举含义 |

当前前端刷新配置为 **10 秒**；详情查询在状态不再为 1 时结束正常轮询。页面历史表的分页不证明服务端具有同名分页参数。

官方 FAQ 当前写明 30 jobs/day、单任务 5,000 tokens、单次 JSON 导入最多 100 个草稿、最多 500 个保存草稿；这些是当前说明，不是永久常量。执行时先看 `GetFoldQuota` 和 `CalculateFoldCost` 返回值。[官方 FAQ](https://alphafoldserver.com/faq)

实现建议：默认单并发、至少采用页面级轮询间隔，失败退避并遵守服务端限流提示。创建请求在超时后先查历史和任务状态，禁止盲重试以免重复提交或重复占用额度；`_reqid` 不能当成已确认的幂等键。

## 结果获取

### 单任务与单模型

本次登录会话已验证的正常用户路径（单模型 GET 实测索引 0，ZIP 内验证索引 0–4）：

```text
GET /_/v2/folds/{JOB_ID}/fold.zip
GET /_/v2/folds/{JOB_ID}/{FOLD_INDEX}/model.cif
GET /_/v2/folds/{JOB_ID}/{FOLD_INDEX}/full_data.json
GET /_/v2/folds/{JOB_ID}/{FOLD_INDEX}/summary_confidences.json
```

`JOB_ID` 来自任务响应，`FOLD_INDEX` 来自选择的模型；不要把形如 `examplefold_pdb_1aay_0` 的 fold 引用直接当成任务 ID。前端示例模型的文件主机另为 `https://www.gstatic.com/alphafoldserver`，普通任务仍用同源路径。

本次匿名获取公开示例 `examplefold_pdb_1aay/0/summary_confidences.json` 成功：`ptm=0.9`、`iptm=0.93`、`ranking_score=0.93`。它只验证公开示例路径，不证明私有任务能匿名下载。[公开示例 summary](https://www.gstatic.com/alphafoldserver/examplefold_pdb_1aay/0/summary_confidences.json)

同一公开示例的 `model.cif` 和 `full_data.json` 也实测 HTTP 200，并通过本地格式检查。CIF 为 109,841 字节；full_data 为 146,443 字节，包含 `atom_chain_ids`、`atom_plddts`、`contact_probs`、`pae`、`token_chain_ids`、`token_res_ids`。该样例有 1,212 个原子条目和 115 个 token，PAE/contact_probs 均为 115×115；这些是样例尺寸，不能硬编码为协议限制。哈希和下载地址见 source-index。

早期匿名请求 `/_/v2/folds/examplefold_pdb_1aay/fold.zip` **60 秒超时**，该历史失败仍保留；不能改写成 HTTP 拒绝。本次自有成功任务的同源 ZIP 下载则 HTTP 200，已验证 19 个条目，见上节。

`summary_confidences.json` 用于排名和摘要；`full_data.json` 用于细粒度置信度、PAE 等；CIF 是坐标结构。返回数据可能包含 `null`；前端还有处理非标准 `NaN` 的逻辑。正式解析器应保留原始响应，并针对合法 token 处理，不要全局替换字符串中的 `NaN`。

### 批量下载

前端将选中的成功任务或示例任务 ID 传给 `CreateDownload/YeS2Vb`，从响应取下载 ID，再导航到：

```text
GET /_/v2/folds/{DOWNLOAD_ID}/folds.zip
```

单包 `fold.zip` 与多任务包 `folds.zip` 不同；批量下载 ID 不等于任一任务 ID。当前 UI 禁止一次选择超过 50 个任务下载；这是前端限制，服务端限制需实测。

### 本地保存与解析

应记录任务 ID、原始输入哈希、模型索引、响应 MIME、下载时间和文件 SHA256。先保存 `.part`，确认响应不是登录 HTML、检查 ZIP 文件头和目录后再原子改名。解压限制路径穿越和总解压大小，保留输出使用条款。不要仅以 HTTP 200、文件扩展名或 `ranking_score` 有值作为完整任务验收。

官方说明结果 ZIP 包含 job_request JSON、模型和置信度文件，还可能有 MSA 和模板。包内确切命名/数量应对一次本人的真实下载做目录核验，不照搬本地 AlphaFold 3 的输出目录。[官方 FAQ](https://alphafoldserver.com/faq)

## 自动化实现边界和验收

建议分层实现 `Session`、`RPC transport`、`FoldInput encoder`、`Job client`、`Downloader`。先用浏览器会话和一条已验证调用链证明可行，再决定是否用 Go 直接请求；当前不提供猜测 Cookie 的伪客户端。

平台及输出存在非商业用途和其他使用限制，应核对适用的 [站点条款](https://alphafoldserver.com/terms) 与 [输出条款](https://alphafoldserver.com/output-terms)，不要把技术上存在 RPC 理解为官方授权无限自动化。

验收状态：

1. **通过**：手动登录后 `GetUser` 成功；**待验证**：会话失效/刷新，完整 Google 重定向链。
2. **通过**：本次输入的 quota/cost、草稿创建和草稿运行请求响应。
3. **通过**：同一任务 5→1→3；**待验证**：自然出现的 failed 样本，不故意耗额度造错。
4. **通过**：同一 JOB_ID 的模型 0 文件及 ZIP，校验内容、哈希、CRC32 和目录。
5. **待验证**：按需批量下载、下载 ID、上限与失败恢复。
6. **待验证**：按需自定义 MSA/模板上传和引用链，以及其它输入类型。

抓包步骤见 `CAPTURE.zh-CN.md`。当前状态是“手动登录后的单任务提交到下载闭环已跑通”，并非“所有 API 分支和独立客户端均已验证”。普通测试流程不再需要用户点击；只有重新认证或涉及新增测试范围时再交互。


## 与本地系统的关系

本文件只描述平台网页协议。本地账号池的 `/v1/accounts`、`/v1/jobs` 和 `/ui/*` 不属于平台接口，接入本地系统请参见 [本地 API 文档](LOCAL_API.zh-CN.md)。平台任务 ID 对应本地任务记录的 `remote_id`；`request_id` 是本地防重标识，不会作为平台任务 ID 发送。
