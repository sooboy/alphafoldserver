# 部署模板说明

这里的 `afplatform.env.example` **不是本地运行必须的配置文件**，也不是程序自动加载的 `.env`。

| 运行方式 | 配置来源 | 是否需要 env 文件 |
|---|---|---|
| Windows / Linux 直接运行二进制 | 内置默认值与 `serve` 命令行参数；浏览器路径也可用 AF_BROWSER | 不需要 |
| `make run` / `make serve` | Makefile 变量转成命令行参数，例如 DATA、PUBLIC_URL | 不需要 |
| 使用这里的 Linux systemd 模板 | systemd 读取 `/etc/afplatform.env`，将值代入 ExecStart 参数 | 使用本模板时需要 |
| Linux Docker Compose | 容器 entrypoint 将 AF_* 环境变量转成启动参数 | 默认不需要；可用 docker.env.example 配置覆盖 |

只有采用 systemd 部署时，才将示例复制到 `/etc/afplatform.env` 并修改实际路径和域名；不要仅因为仓库包含 deploy 目录就在本地新建 `.env`。Go 服务没有 dotenv 自动加载功能，双击/直接运行 exe 不会读取这个模板。

首次管理员初始化、平台用户、任务与账号配置保存在 `-data` 指定的私有数据目录；Google 会话位于其 profiles 子目录，不存放在 env 模板中。

具体命令见 [Makefile 运维指南](../docs/MAKEFILE_OPERATIONS.zh-CN.md)。

Docker 使用另一份可选模板 `deploy/docker.env.example`，不是 systemd 的 `afplatform.env.example`。仓库根目录的 Dockerfile/compose.yaml 包含浏览器与受保护的人工认证桌面，详见 [Docker 部署指南](../docs/DOCKER_DEPLOY.zh-CN.md)。此适配层只在容器入口生效，并未给原生 Go 二进制添加 dotenv 支持。
