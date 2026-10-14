# 文档索引

- [快速开始](../README.md)：网页初始化、批量导入、构建和目录。
- [API 调用使用指南](API_USAGE.zh-CN.md)：可复制的 curl / PowerShell 示例、批量、导入、查询、下载与安全重试。
- [Makefile 编译与运维](MAKEFILE_OPERATIONS.zh-CN.md)：统一编译/测试/发布入口、systemd 管理和安全清理。
- [Docker 部署](DOCKER_DEPLOY.zh-CN.md)：Linux 镜像、Compose、持久化数据和管理员登录桌面。
- [本平台 API](GO_PLATFORM_API.zh-CN.md)：团队身份、批量任务、管理员接口。集成系统请使用这一层。
- [部署与安全边界](GO_PLATFORM_DEPLOY.zh-CN.md)：HTTPS、服务端认证和数据恢复。
- [上游 RPC](ALPHAFOLD_RPC.zh-CN.md)：内部适配协议观察，不是官方稳定公共 API。
- [上游 RPC 机器可读记录](alphafold-rpc.json)。
- [第三方许可](THIRD_PARTY_NOTICES.txt)：随二进制发布。

上游 JSON 格式参考 [Google DeepMind 官方说明](https://github.com/google-deepmind/alphafold/blob/main/server/README.md)。本平台仅实现明确标注的蛋白子集；不支持字段会拒绝导入。

前端使用 [shadcn/ui Vite 流程](https://ui.shadcn.com/docs/installation/vite)，registry 组件位于 `frontend/src/components/ui/`，不是只模仿外观。配置在 `frontend/components.json`，页面内嵌于 Go，不请求外部字体或 CDN。
