# Go 平台部署与安全边界

编译、测试、发布与服务快捷命令见 [Makefile 运维指南](MAKEFILE_OPERATIONS.zh-CN.md)。现成模板位于 `deploy/afplatform.service` 与 `deploy/afplatform.env.example`；以下配置用于说明，可按模板保持一致。

## 组成

1. 单个 Go 服务实例：内置网页、用户认证、文件持久队列、调度器及 CDP WebSocket 客户端。
2. 外部 Chrome/Chromium：安装浏览器和系统依赖，以非 root 专用服务用户运行，保留默认浏览器 sandbox；不使用 `--no-sandbox`。
3. 私有数据目录：`state.json`、`profiles/<account-id>/`、`downloads/`、`platform.lock`。目录 0700、状态和下载文件 0600；Windows 仍应配置目录 ACL，仅服务账号和管理员可访问。
4. 团队访问的 HTTPS 域名及反向代理，或服务直接 TLS。不要公开 CDP 端口、数据目录、浏览器 profile、远程桌面或初始密码文件。

这是单实例团队服务，不是多节点高可用调度集群。数据目录使用操作系统文件锁，第二个进程会失败；不要在 NFS 等未验证的共享锁语义存储上运行。状态采用写临时文件、fsync、rename；Linux 目录 fsync / 灾难断电一致性及外部备份恢复仍需实际部署验收，不宣称数据库级 HA。

## 服务配置

首次运行不执行 `init`：直接启动 `afplatform` / `afplatform serve`，在网页创建管理员。一次性凭据在数据目录 `setup-token.txt`，只由操作服务器的人读取输入，成功后失效；避免公开部署被首个访问者抢占。已有用户数据时不会重新初始化。网页无需浏览器即可启动，使用账号池前仍需安装 Chrome/Edge。

升级前正常停止旧服务，备份私有数据，用新二进制指定原 `-data`。不要同时运行两个服务访问同一目录，也不要重新执行旧 `init`。state.json 格式保持兼容，历史任务和账号配置保留。React/shadcn/ui 页面内嵌二进制，运行不需要 Node。

```ini
# /etc/systemd/system/afplatform.service；路径、用户名、域名和 DISPLAY 按部署修改。
[Unit]
Description=AlphaFold team platform
After=network-online.target
Wants=network-online.target

[Service]
User=afplatform
Group=afplatform
WorkingDirectory=/srv/afplatform
ExecStart=/srv/afplatform/afplatform-linux-amd64 serve -data /srv/afplatform/private/data -listen 127.0.0.1:8788 -public-url https://fold.example.org -browser /usr/bin/google-chrome -max-active-browsers 3 -browser-idle 2m
Restart=on-failure
RestartSec=5
TimeoutStopSec=30
KillMode=control-group
UMask=0077
# 仅在已部署同一服务用户可访问的受保护桌面时启用：
# Environment=DISPLAY=:1

[Install]
WantedBy=multi-user.target
```

不要在有 Google 登录窗口时强杀浏览器；正常停止服务会等待任务中止并关闭其拥有的浏览器。异常退出可能留下 profile 占用，使用 systemd 的 control-group 清理本服务进程；不要删除 profile 锁或终止不属于本服务的浏览器。

Nginx 示例：

```nginx
server {
    listen 443 ssl;
    server_name fold.example.org;
    ssl_certificate /etc/ssl/fold/fullchain.pem;
    ssl_certificate_key /etc/ssl/fold/privkey.pem;
    client_max_body_size 1m;
    location / {
        proxy_pass http://127.0.0.1:8788;
        proxy_set_header Host $http_host;
        proxy_read_timeout 150s;
    }
}
```

服务严格校验 Host/Origin。`-public-url` 必须与用户浏览器访问的 origin 完全一致，不带末尾斜线。应用不信任客户端转发头做身份判定，Cookie 在 HTTPS origin 下带 Secure。反向代理场景下登录限速按代理地址共享（每分钟 10 次）；较大的团队需另行评估认证限流。数据流量与登录密码不得经公网明文传输。

## Google 登录不等于团队登录

管理员在本平台打开一个 Google 登录窗口，实际窗口位于**运行服务的机器**。Linux 无图形桌面只能执行已登录 profile 的无头任务；首次认证需受保护的远程桌面/虚拟显示会话，并以同一 OS 用户、同一数据目录操作。服务不内置 noVNC，不把远端 Google 登录页面无保护转发给团队成员。

认证窗口是普通浏览器，认证阶段不启用 CDP/WebDriver、不执行页面脚本、不读取密码。完成 Google 认证并进入 AlphaFold Server 后，管理员应先关闭该账号的全部认证窗口，再点击“完成登录并检查”；窗口未关时不会启动自动化检查，也不会标记已验证。随后检查动作通过同一 profile 的后台浏览器读取账户身份指纹及额度，不导出 Cookie 或 Token。重复登录同一 Google 身份会被拒绝；绑定后意外切换身份会停用该配置。关窗/休眠不会保证 Google 会话永久有效；如果 Google 要求 MFA/重新认证，配置进入需要登录或连接异常状态，任务等待其他合法可用配置。

应使用更新后的官方 Chrome/Edge。移除认证阶段自动化不等于保证 Google 登录成功；账号风险或组织策略仍由 Google 决定，不使用伪造浏览器标识或关闭安全检查来强行登录。

Windows profile 不保证可移植到 Linux（系统加密及浏览器环境不同）。部署新服务器应重新登录，不承诺直接复制 Cookie 就能迁移。

## 权限与恢复

- 管理员创建平台用户、管理账号配置、查看全体任务和审计；普通成员仅自己的任务、输入、结果。
- 用户 API Token 只返回一次，库内存储 SHA-256；轮换立即撤销旧 Token。密码为随机盐 PBKDF2-SHA256 600000 次，不存明文（初始化生成的私有密码文件除外）。
- 浏览器会话为 HttpOnly / SameSite=Strict Cookie；写请求要求同源和 CSRF；服务重启后需重新登录。禁用用户立即撤销 Token 和会话；已提交的远端任务无法撤销，未提交队列在调度时取消。
- 未确认的 create/update 不自动重发。已知 remote ID 的 `submission_unknown` 可做只读核对；没有 remote ID 的需管理员在原账号网页人工核对。不要通过新请求标识盲目重试。
- 成功 ZIP 在权限校验后下载；缓存保存在私有目录并校验 SHA-256。最多 50 MB；更大的包需调整实现并重新测试，不宣称无限大结果支持。
- 账号配置上限 1000、单用户非终态任务 50、平台非终态队列 1000；浏览器上限可配 1–20，默认 3。没有对真实 50 个 Google 账号同时提交做压测。
- 备份需停止服务及其浏览器后备份整个私有数据目录；备份包含研究输入、凭据和结果，应加密并限制访问。恢复必须同一兼容环境；先只读检查，再开放提交。

## 验收范围

自动验证覆盖 fake RPC 调度/错误/幂等、权限隔离、持久恢复、50 配置 / 100 次并发租用上限、真实 Windows Chromium profile 启停及内置网页。Linux amd64/arm64 仅交叉编译，不等于 Linux 浏览器或 Google 认证验收。旧版真实预测证据继续保留；本轮没有新真实预测。

## 编译依赖及许可

- [Go 标准库 embed](https://pkg.go.dev/embed)、[PBKDF2](https://pkg.go.dev/crypto/pbkdf2)
- [coder/websocket](https://github.com/coder/websocket) — MIT
- [gofrs/flock](https://github.com/gofrs/flock) — BSD-3-Clause
- [golang.org/x/sys](https://cs.opensource.google/go/x/sys) — BSD-3-Clause
- [Chrome Headless 官方说明](https://developer.chrome.com/docs/chromium/headless)

分发二进制时携带 `dist/THIRD_PARTY_NOTICES.txt`。Chrome/Chromium 为外部组件，按其发行版许可和部署要求安装。
