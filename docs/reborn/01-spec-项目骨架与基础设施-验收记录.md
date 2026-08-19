# Spec 01 验收记录：项目骨架与基础设施

- 完成日期：2026-08-19
- 提交：`feat(bootstrap): 🚀 项目骨架与基础设施`

## 验收清单

- [x] `task lint`、`task test`、`git diff --check` 全绿
- [x] `go build ./...`、`go vet ./...` 通过
- [x] 迁移文件内嵌 + iofs 读取链路单测通过（`internal/migration`，首个版本为 1）
- [x] CLI 冒烟：`migrate` 无参/非法参 → usage + exit 2；`bot` 缺配置 → fail-fast + exit 1
- [ ] `task compose-up` 起 PG，`task migrate-up` 两次执行均成功（幂等）——本环境 Docker 守护进程未运行，留待本机验证
- [ ] `curl localhost:PORT/healthz` 返回 `{"status":"ok","db":"up"}`——依赖上一条，留待本机验证
- [x] SIGINT 优雅退出已实现（顺序：停止 HTTP → 关闭连接池 → 完成，10s 超时兜底；运行时验证依赖 DB，留待本机）
- [x] `.env` 未被 git 跟踪；`.env.example` 包含全部变量
- [x] 提交信息符合全局规范（中文 + emoji）

## 风格一致性审查结论

> 本阶段结束时无法调用独立 subagent（本会话未注册 `subagent` 工具），按 AGENTS.md
> 兜底要求，由主 agent 执行独立的第二遍复核。

通过项：

- 可读性优先：直白实现，无反射 / `any` / 动态注册；无运行时字符串拼接逻辑（DSN 用类型化 `pgx.ConnConfig` 字段赋值，规避"禁止拼接 URL"红线）。
- 抽象适度：`required` / `optional` / `portEnv` 在 2+ 处复用后才提取；`Runner` 接口为测试要求而设；`Direction` 为最小类型化常量；`migrator` 仅 6 行适配，未层层封装。
- 命名与注释：遵循 Go 惯例（`DBHost` / `WebUIPort` / `JWTSecret`）；注释统一中文、以中文开发者视角撰写，仅说明非显而易见的决策与副作用。
- 错误处理：启动校验 fail-fast；迁移 `ErrNoChange` 幂等处理；优雅退出顺序清晰，日志覆盖完整关闭链。

问题项及修复记录：

- 无阻塞问题。修复记录：`db.Connect` 参数由值类型 `config.Config` 改为 `*config.Config`，与 `config.Load()` 返回值类型一致（消除编译期类型不匹配）。
