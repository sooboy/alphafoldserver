# Makefile 编译与运维指南

从仓库根目录使用 GNU Make。Windows 本机已安装的命令是 `mingw32-make`，本文 `make` 可替换为它；Linux 通常使用 `make`。Go 构建需要 Go 1.26+，只有重建前端或运行 UI 测试时需要 Node/npm。

## 常用命令

```sh
make help
make build                 # 当前系统二进制与许可 -> build/
make check                 # Go vet、单元、API 与清理保护测试
make test-api              # 运行文档中的 HTTP 示例测试，不耗真实额度
make test-race              # 需要平台支持的 C 编译器
make deps                  # 下载 Go 模块，按 lockfile 安装前端依赖
make frontend              # 重建网页并复制到 Go embed 目录
make build                 # 前端重建后，再编译 Go 才会带上新页面
make release               # Windows amd64、Linux amd64/arm64 -> dist/
make ui-test               # 已安装 Chrome/Edge，运行隔离 UI 测试
make verify                # 完整检查、页面构建/UI 测试/API 探针及三平台编译
```

二进制内嵌页面，运行时不需要 Node/npm/前端源码，但实际任务仍需要外部浏览器。`make build` 使用已提交的网页资源，不联网安装 npm 依赖；Go 首次下载模块仍可能需要网络。`make release` 也不会自动重建网页，前端有改动请先 `make frontend`。

源码以外的构建 helper 位于 `tools/dev/`，只用于开发，不需要部署。使用 `-trimpath -buildvcs=false` 避免本机路径和 Git 脏状态影响构建。发布目录包含 SHA256 清单与第三方许可。

```sh
make build TARGET_OS=linux TARGET_ARCH=arm64 OUT=build/linux-arm64
make release RELEASE_DIR=dist/release
```

输出只允许位于仓库内 `build/` 或 `dist/`，拒绝路径穿越、源代码/私有目录、符号链接或目录联接。构建不会主动停止或重启服务。若输出名指向运行中的版本，请先停服务再更新；Windows 覆盖运行中的 exe 通常会报文件占用。编译成功不代表旧实例已切换到新版本。

## 前台运行与状态

```sh
make run                   # 编译本机版本并前台运行，Ctrl+C 停止
make serve                 # 不重新编译，运行 build/ 中已有版本
make status                # GET /api/v1/setup，检查 HTTP 与初始化状态
```

`make status` 不需要 Token，也**不检查 Google 登录或剩余额度**。响应错误、重定向或无法访问时退出非零；没有另外启动服务。

```sh
make run DATA=private/go-platform LISTEN=127.0.0.1:8788 \
  PUBLIC_URL=http://127.0.0.1:8788 MAX_BROWSERS=3
```

可信内网可显式启用无需证书的 HTTP（替换为实际服务器内网 IP）：

```sh
make run LISTEN=0.0.0.0:8788 PUBLIC_URL=http://192.168.1.100:8788 ALLOW_INSECURE_HTTP=true
```

`ALLOW_INSECURE_HTTP` 默认 `false`，只对 Make `run` / `serve` 转为同名 CLI 开关；不是原生二进制自动读取的环境变量。仅允许私有 IP 入口和私有/loopback 直接来源，HTTP 不加密凭据，防火墙仅放行可信内网。详见 [局域网 HTTP 说明](../README.md#可信局域网直接使用-http无需证书)。

Windows 含空格路径应对整个参数加引号：

```powershell
mingw32-make run 'BROWSER=C:/Program Files/Google/Chrome/Application/chrome.exe'
```

首次打开网页创建管理员，初始化凭据位于 DATA 下的 `setup-token.txt`。升级继续指定原 DATA，不能同时运行两个进程访问同一私有目录。服务运行期间不要修改 state.json、复制正在使用的 profile 或删除浏览器锁。

## Linux systemd 部署

**本地直接运行和 `make run` 都不需要 `.env`。** 本程序不自动读取 dotenv 文件。下面的 env 示例只供 systemd 读取并代入启动参数；普通 exe 运行仍使用默认值/命令行参数。参见 [三种运行方式的配置区别](../deploy/README.md)。

模板位于 [`deploy/afplatform.service`](../deploy/afplatform.service) 与 [`deploy/afplatform.env.example`](../deploy/afplatform.env.example)。以下命令会改变目标服务器，需要运维人员确认路径、专用用户、HTTPS 代理和浏览器安装后手动执行；构建或测试不会自动安装/启动系统服务。

```sh
# 专用账号 afplatform 需事先由管理员创建。
sudo install -d -m 0755 /opt/afplatform
sudo install -d -o afplatform -g afplatform -m 0700 /var/lib/afplatform
sudo install -m 0755 dist/afplatform-linux-amd64 /opt/afplatform/afplatform
sudo install -m 0644 docs/THIRD_PARTY_NOTICES.txt /opt/afplatform/THIRD_PARTY_NOTICES.txt
sudo install -m 0600 deploy/afplatform.env.example /etc/afplatform.env
sudo install -m 0644 deploy/afplatform.service /etc/systemd/system/afplatform.service
# 编辑 /etc/afplatform.env，尤其域名、数据目录和浏览器路径。
sudo systemctl daemon-reload
sudo systemctl enable afplatform.service
sudo systemctl start afplatform.service
```

模板使用 `Type=exec`，可在进程无法执行时报告启动失败；使用 `KillMode=control-group` 管理该服务的子进程，避免退出后残留自身浏览器。服务按非 root 专用用户运行，不禁用 Chrome sandbox。[systemd 官方服务说明](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)

默认仅监听 loopback；通过 HTTPS 反向代理向团队开放，Host 与 AF_PUBLIC_URL 必须匹配。参见 [部署说明](GO_PLATFORM_DEPLOY.zh-CN.md) 的 Nginx 配置。

运行配置不接收 Google 密码或 Token。人工 Google 认证需要同一服务用户可访问的受保护桌面；按实际桌面配置 DISPLAY/WAYLAND_DISPLAY 等，不应直接暴露远程桌面。无桌面的 Linux 服务不能弹出可供用户认证的窗口。Windows profile 不保证能复制到 Linux 后继续登录。

有源码目录时可使用以下快捷命令，它们只调用 systemctl/journalctl，不需要在部署机安装 Go 或 Node：

```sh
sudo make service-start
sudo make service-stop
sudo make service-restart
make service-status
make service-logs
# 非默认服务名：make service-status SERVICE=your-fold.service
```

Windows 调用 `service-*` 会明确报错；使用前台运行和 Ctrl+C，或由自己的 Windows 服务管理方案接管二进制。Linux 服务启停未在本 Windows 工作机实际执行。

## 升级 备份与回退

1. 完成新版本编译/测试，把旧二进制单独保存，不直接覆盖运行中的文件。
2. 正常停服务，确认所属浏览器也已退出；备份**完整**数据目录（状态、profiles、下载），备份含凭据和研究数据，须加密并限制访问。
3. 替换二进制与许可，保持原数据目录和服务用户，启动后先检查平台状态、账号会话，再开放真实提交。
4. 如需回退，停服务再换回旧二进制；只有确实需要恢复数据时才恢复已验证兼容的备份。回退本地文件不撤销已经提交的上游预测。

`make service-restart` 不执行备份，也不替换二进制。`make release` 不安装服务、不重启服务、不迁移账号会话。项目没有自动清空、重置管理员或删除全部任务的运维命令。

## 安全清理范围

```sh
make clean
```

仅删除可再生的 `frontend/dist/`、`frontend/test-results/`、`frontend/playwright-report/`。删除前检查每个绝对目标在仓库内，拒绝链接/目录联接；如果发现 state.json、setup-token.txt、profiles、downloads 等受保护数据，整次清理会在删除前中止。

特意不删除 `build/`、`dist/` 二进制、`private/`、`frontend/node_modules/`、已内嵌的 `internal/platform/assets/`、源码、归档以及 `transactions/` / `evidence/`。因此不会误删部署或回滚需要的文件。旧版本二进制只在人工确认无运行进程引用后单独清理。

## 常见问题

- 找不到 make：Windows 可使用已有的 mingw32-make；这是开发工具，不是服务运行依赖。
- `npm CLI not found`：确认 Node/npm 同一安装；特殊 Windows 安装可将 AF_NPM_CLI 指向 npm-cli.js。
- UI 测试找不到浏览器：设置 AF_TEST_BROWSER_PATH 为已安装 Chrome/Edge 可执行文件路径。
- 修改页面后看不到变化：先 make frontend，再 make build，然后正常停止旧服务并启动新二进制。
- 没有账号/额度但 API 返回 202：任务仅已入队；查询 job.stage 和管理员账号池状态。
- Token 不能调用外部站点：本平台 Token 仅用于本平台 API，不能用于 Google OAuth 或 AlphaFold 原始 RPC。
