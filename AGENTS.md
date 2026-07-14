# Repository Guidelines

## 项目结构与架构

本仓库是 Go 1.26 编写的 NapCat OneBot 11 机器人，使用 PostgreSQL 持久化配置，并提供 Vue 3 + TypeScript 管理界面。`cmd/bot/` 是服务入口，`cmd/migrate/` 是迁移工具；核心实现位于 `internal/`（WebSocket、OneBot API、插件、命令、权限、管理 API 与数据库），可复用日志包位于 `pkg/logger/`，内置插件位于 `plugins/`。前端源码和依赖清单位于 `web/`，版本化 SQL 位于 `internal/migration/migrations/`，设计说明位于 `docs/guide.md`。Go 测试与实现同目录，文件名使用 `*_test.go`。

事件链路为：NapCat → 反向 WebSocket → 命令与权限解析 → PluginManager → 插件 → OneBot Action Client。修改并发收包、`echo` 响应关联、权限优先级或插件 Manifest 同步时，应先阅读 `docs/guide.md`。

## 构建、测试与本地开发

统一从仓库根目录使用 [Task](https://taskfile.dev/)：

- `task setup`：下载 Go 模块并安装 WebUI 依赖。
- `task run` / `task web-dev`：分别启动机器人和 Vite 开发服务。
- `task test`：运行 `go test ./...` 完整测试。
- `task lint`：检查 `gofmt`、`go vet` 和 Vue TypeScript 类型。
- `task web-build`：类型检查并构建前端生产资源。
- `task compose-up`：构建并启动机器人与 PostgreSQL。
- `task migrate-up` / `task migrate-down`：应用迁移或回滚一版。

提交前至少运行 `task lint`、`task test` 和 `git diff --check`。新增 Task 必须提供 `desc`，名称使用小写短横线。

## 编码风格与命名

Go 代码必须通过 `gofmt`；包名小写，导出类型使用 `PascalCase`，函数和变量遵循 Go 惯例。Vue/TypeScript 使用 2 空格缩进，组件名使用 `PascalCase`，变量和函数使用 `camelCase`。环境变量使用 `UPPER_SNAKE_CASE`。避免无关格式化，公共接口注释说明设计原因。不得修改已部署的迁移；数据库变化应新增配对的 `NNNNNN_description.up.sql` 与 `.down.sql`。

## AI 辅助编程规范

AI 新增或修改代码时，每个文件顶部必须以该语言的单行注释列出 `📌 影响范围`（无外部变量也须注明“无”）。每个函数头注释必须显式包含 `@param`、`@returns` 和 `⚠️副作用说明`；每个 `if` 正上方必须用 `[决策理由]` 解释判断；每个函数结束前必须用 `>>> 数据演变示例` 给出两组包含关键中间状态的输入到输出演变。不得因逻辑简单而省略。

每个开发任务应优先将边界清晰的具体实现、排查或验证工作交给上下文干净的 subagent。主 agent 的上下文应聚焦于任务目标、关键设计决策、阶段进度、风险和最终结果，并负责拆分任务、协调跨模块依赖、核实 subagent 的代码与证据、处理冲突及完成最终验收。不得因使用 subagent 而省略主 agent 的代码审查、测试或交付责任；任务过小、无法安全拆分或 subagent 不可用时，主 agent 可直接处理，但必须说明原因。

每次代码新增、修改或重构后，AI 必须调用独立 subagent，复核需求符合度、权限与输入校验、数据一致性、并发和资源风险、敏感信息、错误处理及测试遗漏。主 agent 核实证据、修复问题并重新运行检查后方可交付；无法调用时须明确说明并执行独立的第二遍审查。

## 测试指南

使用 Go 标准 `testing` 包，优先采用表驱动测试和本地 fake/mock，禁止依赖真实 NapCat 或外部数据库。测试名使用 `TestXxx`，覆盖正常、边界和错误路径。涉及 Action Client 时验证 `echo`、超时、断连和并发响应；管理写操作须验证授权、事务、冲突检测、审计和热刷新。前端目前仅配置类型检查，修改 WebUI 至少运行 `task web-build`。

## 提交与拉取请求

历史提交遵循 `类型(内容): emoji 颜文字 中文描述`，例如 `fix(事件分发): 🔇 (｡•̀ᴗ-)✧ 忽略无关事件`。类型使用 `feat`、`fix`、`docs`、`test`、`refactor` 或 `chore`；一次提交聚焦一个逻辑变更，emoji 与颜文字缺一不可。PR 应说明目的、主要改动和验证命令，关联 issue；界面改动附截图，配置或兼容性变化说明迁移步骤。

## 安全与配置

密钥仅放入未跟踪的 `.env`。部署必须设置 `DB_PASSWORD`、`NAPCAT_TOKEN`、32 字节以上的 `JWT_SECRET` 和 12 字符以上的 `WEBUI_PASSWORD`；启用 QQ/WebUI 管理入口还须设置 `SUPER_ADMIN_QQ`。不得提交凭据、日志、缓存或构建产物。生产环境不要长期启用 `LOG_LEVEL=debug`，因为原始事件可能包含聊天内容、QQ 标识及 URL。
