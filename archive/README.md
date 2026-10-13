# 本地历史归档

当前产品是 Go 团队平台，入口见根目录 README。

`node-prototype/` 存放本机整理前的 Node 原型、旧页面、抓包工具、旧验证脚本和历史说明，未删除历史代码。该目录不进入 Git，也不是当前发布物；部分旧脚本依赖原工作目录，不作为现行构建/测试入口。

现行测试在 `internal/platform/*_test.go` 与 `frontend/tests/`；构建与验证入口在 `scripts/`。
