# Docker 部署（Linux Docker Engine）

配套文件：[Dockerfile](../Dockerfile)、[compose.yaml](../compose.yaml)、[可选环境变量模板](../deploy/docker.env.example)。

镜像使用多阶段 Go 构建，直接内嵌仓库中已有网页，不需要 Node。运行层包含 Debian Chromium、Xvfb/Openbox、x11vnc/noVNC、D-Bus 和 tini；服务及浏览器使用非 root UID/GID `10001:10001`。没有更改 Google 登录、浏览器参数或平台权限逻辑。

## 部署前提与网络

- 使用 **Linux Docker Engine + Docker Compose 插件**。本模板使用 `network_mode: host`，不是默认 bridge/端口映射方案；不面向 Windows 容器。
- 默认平台、noVNC、VNC 分别只监听宿主机 `127.0.0.1:8788`、`:6080`、`:5900`。确保端口空闲；不要添加 `ports:`，host 模式没有端口映射。
- Host 网络意味着容器能访问宿主机 loopback 服务。只部署在可信、受管理的服务器；若要求严格网络隔离，需要另行设计 bridge + 应用 TLS，不要直接改成明文 `0.0.0.0`。
- 默认仅通过 SSH 隧道访问。团队公开入口用宿主机 Nginx/Caddy 提供 HTTPS，后端仍走 HTTP loopback；认证桌面始终仅供管理员通过 SSH 访问。
- 主机需要允许容器内非 root Chromium 的用户命名空间沙箱。Compose 使用附带的 deny-by-default seccomp 配置允许所需 namespace 调用，同时去除 capabilities、启用 no-new-privileges；**不使用 privileged、SYS_ADMIN、seccomp=unconfined 或 --no-sandbox**。

[Docker Host 网络说明](https://docs.docker.com/engine/network/drivers/host/) · [Chromium 沙箱的容器配置说明](https://playwright.dev/docs/docker)。附带 seccomp 文件仅是系统调用策略，不使用 Playwright 操作 Google 登录；来源、版本、哈希和许可证见 [NOTICE](../deploy/docker/NOTICE.txt)。

## 1. 构建与首次启动

在仓库根目录运行：

```sh
docker compose config --quiet
docker compose build
docker compose up -d
docker compose ps
docker compose logs --tail=100 afplatform
```

默认不需要 `.env`。首次构建会下载 Go 模块、基础镜像及 Debian 包。默认构建宿主机架构；如使用 buildx 交叉构建，目标架构也必须有相应 Chromium 包。镜像包含系统浏览器，因此比单个 Go 二进制大，不能替换为只有 Go 二进制的 scratch 镜像。

运行层 `/data` 挂载命名卷 `afplatform_platform-data`（Compose 项目名默认为 afplatform）。里面保存平台状态、账号 profiles、下载、浏览器 HOME，以及认证桌面的密码文件。**不要同时启动多个实例共用这个卷，也不要随意修改 Compose 项目名或使用 `down -v`。** 改项目名可能创建另一份空卷，看起来像账号丢失。

## 2. 打开平台与服务器登录桌面

在管理员自己的电脑建立隧道，替换 USER/HOST：

```sh
ssh -N \
  -L 8788:127.0.0.1:8788 \
  -L 6080:127.0.0.1:6080 \
  USER@HOST
```

然后打开：

- 平台：`http://127.0.0.1:8788`，**使用这个精确地址，不改为 localhost**，否则可能触发 Host/Origin 校验。
- 服务器桌面：`http://127.0.0.1:6080/vnc.html`，点击连接并输入桌面密码。

在服务器终端私下读取两个不同用途的凭据：

```sh
# 仅第一次创建平台管理员需要；初始化完成后文件会被删除。
docker compose exec afplatform cat /data/setup-token.txt
# 自动生成的管理员桌面密码，不是 Google 密码，不会打印到启动日志。
docker compose exec afplatform cat /data/.desktop-password
```

不要把输出发到聊天、工单或公共日志。经典 VNC 口令只有 8 字符，不能单独当作公网保护；模板始终使用 loopback + SSH，不能公开映射 6080/5900。

在平台网页创建管理员后，添加账号并点击认证。普通 Chromium 窗口会显示在 **noVNC 中的服务器桌面**，不是你的本机桌面。在该窗口手动完成 Google 登录，再按平台页面提示完成检查。noVNC 是全部共享账号的管理桌面，不应给普通团队成员访问。

关闭 noVNC 标签或 SSH 隧道不等于删除服务器 profile。重建容器时保留数据卷及服务用户可保留本地会话资料，但 Google 仍可能过期、撤销或要求再次认证；不能保证永久登录。不要直接复制正在使用的 Windows profile 来替代 Linux 登录。

## 3. 团队 HTTPS 访问与可选配置

需要修改配置时再创建模板副本，勿覆盖已有文件：

```sh
test -e deploy/docker.env || cp deploy/docker.env.example deploy/docker.env
# 编辑 deploy/docker.env，将 AF_PUBLIC_URL 改为 https://fold.example.org
docker compose --env-file deploy/docker.env config --quiet
docker compose --env-file deploy/docker.env up -d
```

宿主机 HTTPS 反向代理指向 `http://127.0.0.1:8788`，保留原始 Host，并确保域名与 AF_PUBLIC_URL 完全一致。已有 Nginx 示例见 [Go 部署指南](GO_PLATFORM_DEPLOY.zh-CN.md)。不要将 noVNC 纳入普通团队入口。

| 变量 | 默认值 | 用途 |
|---|---|---|
| AF_PORT | 8788 | 宿主机 loopback 服务端口 |
| AF_PUBLIC_URL | http://127.0.0.1:8788 | 浏览器访问的精确 origin |
| AF_MAX_BROWSERS | 3 | 同时活跃浏览器上限，包含登录窗口 |
| AF_BROWSER_IDLE | 2m | 无头浏览器空闲回收时间 |
| AF_DESKTOP | 1 | 1 启用人工认证桌面，0 关闭桌面 |
| AF_DESKTOP_PORT | 6080 | noVNC loopback 端口 |
| AF_VNC_PORT | 5900 | VNC loopback 端口 |

修改 AF_PORT 后，本地隧道访问场景必须同步调整 AF_PUBLIC_URL 和隧道。桌面端口不能相同或与平台端口冲突。AF_DESKTOP=0 时平台/已认证会话的后台执行仍可使用，但不能完成新账号人工认证；需要时重新开启。

**这次新增的是容器启动适配层**：Compose 将变量注入容器，`entrypoint.sh` 转换为 Go 的 `-data/-listen/-public-url/-browser/-max-active-browsers/-browser-idle` 参数。显式命令行参数最后追加；容器 env 提供默认值。Go 二进制本身仍没有 dotenv 自动加载功能。systemd 的 `deploy/afplatform.env.example` 不是这份 Compose 的配置文件。

高级用法可以给镜像传 AF_TLS_CERT/AF_TLS_KEY，并挂载证书进行原生 TLS，但本 Compose 示例不配置它们。若自行改变监听方式/命令参数，也要相应设置 AF_HEALTHCHECK_URL、必要时 AF_HEALTH_CA_CERT；不得用跳过证书校验掩盖配置错误。默认健康检查仅验证平台 HTTP，不证明 Google 登录或额度可用。

## 4. 运维与自检

```sh
docker compose logs -f --tail=100 afplatform
docker compose restart afplatform
docker compose stop           # 60 秒正常停止窗口，保留命名卷
docker compose up -d          # 使用原数据卷恢复
```

升级前先停止服务、备份整个数据卷，再构建/替换镜像；备份含登录凭据与研究数据，应加密并限制访问。若用 `--env-file` 启动，后续重建/升级仍使用相同参数。浏览器 HOME 也在卷内，不只备份 state.json。使用 bind mount 替代命名卷时，目录须由 UID/GID 10001:10001 可读写；不要以 chmod 777 解决权限问题。

不访问 Google 的基础浏览器自检（不启用真实提交）：

```sh
docker compose exec afplatform sh -c '
  d=$(mktemp -d /tmp/chromium-check.XXXXXXXX)
  trap '\''rm -rf -- "$d"'\'' EXIT
  timeout 30 chromium --headless=new --no-first-run --disable-background-networking \
    --user-data-dir="$d" --dump-dom about:blank
'
```

若出现 sandbox/namespace/Operation not permitted，检查主机内核、用户命名空间及 AppArmor/SELinux 策略。由运维配置针对性的允许策略；不要关闭浏览器沙箱或全局禁用主机保护。若 Google 提示浏览器不受支持或要求重新认证，只能按 Google 正常认证流程处理，镜像不包含绕过逻辑。

本机检查：`node scripts/test-docker-config.mjs` 可在有 Bash、Docker CLI/Compose 的开发环境执行配置回归，不启动 Docker daemon、不操作 Google。完整镜像/浏览器运行还需 Linux Docker 环境。此次 Windows 工作站的 `docker build` 实际返回 `Docker Desktop is unable to start`，因此**配置与脚本检查不代表镜像已经构建成功或 Google 登录已验证**。
