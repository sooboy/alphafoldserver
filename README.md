# FoldSpace · AlphaFold 团队平台

Go 单二进制服务 + 内嵌 React / shadcn/ui 界面。团队共享服务端账号配置池，由有限数量的浏览器按需执行任务；成员只看自己的任务，管理员可看全部。

这是独立团队平台，非 Google 官方产品。使用者须遵守上游使用政策与额度限制；不绕过 Google 认证、验证码或限流。

## 直接运行，在网页初始化

```powershell
# Windows：不需要 init
.\dist\afplatform-windows-amd64-v2.exe
```

```sh
# Linux
chmod +x ./afplatform-linux-amd64
./afplatform-linux-amd64
```

1. 打开 `http://127.0.0.1:8788`。
2. 首次运行在页面填写管理员用户名、密码和一次性初始化凭据。凭据位于启动日志提示的数据目录 `setup-token.txt`（默认 `./private/go-platform/setup-token.txt`）。这是防止未初始化服务被抢占的保护，不是另一个 init 命令。
3. 初始化成功后凭据立即失效、文件尽力删除；已有用户数据直接显示登录页，不覆盖历史管理员或账号配置。
4. 管理员在“共享账号池”添加账号，在**服务器的普通浏览器**完成 Google 认证，关闭认证窗口后点“完成登录并检查”；在“团队成员”创建同事的平台账号。

无参数默认启动服务；`serve`、`-h` / `--help` 仍可用。旧 `init` 仅保留向后兼容，不是必需步骤。首次网页初始化不要求安装浏览器；实际认证和执行需要外部 Chrome/Chromium/Edge。

```sh
./afplatform-linux-amd64 serve -data /srv/afplatform/data \
  -listen 127.0.0.1:8788 -public-url https://fold.example.org \
  -browser /usr/bin/google-chrome -max-active-browsers 3
```

远程团队访问应使用 HTTPS。Linux 人工 Google 认证需要同一服务用户的受保护图形桌面。**一个可执行文件包含网页，不包含浏览器、私有数据或系统依赖。**

## 批量任务

- “添加任务”增加独立任务；任务内“添加序列”增加同组蛋白链，两者不会混淆。
- 一次选择多个 JSON 文件：支持本地 input 对象/数组、AlphaFold Server version 1 的蛋白 proteinChain/count；一个文件也可有多个任务。
- 导入只生成草稿，校验预览后明确确认才入队。每批 1–50 任务，请求体最大 1 MiB；单任务计入 copies 后最多 5000 残基。
- 当前仅蛋白序列、自动 seed。DNA/RNA、配体、修饰、模板、非空 modelSeeds 等明确报错，不静默丢弃字段。
- 每任务自动生成唯一请求标识，提交前保存并锁定草稿。网络中断请确认原批次结果，不新建重复任务。API 客户端须保存稳定 request_id。
- 本地入队是原子事务：任一项错误整个批次不入队。远端任务异步独立执行，不承诺远端任务同时成功。

样例：[本地批次](examples/batch-local.json)、[AlphaFold Server 批次](examples/batch-alphafoldserver.json)。

## 构建与测试

内嵌网页构建产物已提交，只修改 Go 不需要 Node：

```sh
go build -trimpath -o afplatform ./cmd/afplatform
go test ./cmd/afplatform ./internal/platform
```

修改前端需要 Node.js 22.12+ / npm；Go 构建需要 Go 1.26+：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
node scripts/build.mjs
node scripts/verify.mjs
```

浏览器测试使用隔离数据，不登录 Google、不消耗额度：

```powershell
go build -trimpath -o private/platform-v2/modified.exe ./cmd/afplatform
# 默认 Windows Edge；AF_TEST_BROWSER_PATH 可指定已安装的 Chrome/Edge
npm --prefix frontend test
$env:AF_TEST_BROWSER='1'
go test ./internal/platform -count=1
```

`build.mjs` 生成 Windows amd64、Linux amd64/arm64 及许可；加 `--frontend` 先构建页面。Linux 交叉编译不代表 Linux 浏览器/Google 认证实测。运行时不依赖 Node、npm 或前端源码。

## 目录

```text
cmd/afplatform/             CLI 与服务启动
internal/platform/         Go 认证、任务、调度、浏览器及测试
internal/platform/assets/  内嵌网页与上游浏览器请求脚本
frontend/                  React + TypeScript + Tailwind + shadcn/ui 源码
frontend/tests/            Playwright UI 测试
scripts/                   当前构建、验证与行为探针
docs/                      API、部署、上游协议及许可
examples/                  无凭据的 JSON 样例
archive/                   归档说明；旧原型仅保存在本机，不随 Git 发布
private/ dist/             私有数据与构建结果，不入 Git
transactions/ evidence/    本机历史与可回滚验证证据，不入 Git
```

旧原型、页面、抓包工具与验证脚本移入 `archive/node-prototype/`，现行代码不依赖它。浏览器 profile、密码、Cookie、Token、HAR、研究数据和本机证据不得提交。

更多：[文档索引](docs/README.md) · [本平台 API](docs/GO_PLATFORM_API.zh-CN.md) · [部署](docs/GO_PLATFORM_DEPLOY.zh-CN.md) · [上游 RPC](docs/ALPHAFOLD_RPC.zh-CN.md)。
