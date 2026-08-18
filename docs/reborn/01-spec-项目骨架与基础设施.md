<!-- 📌 影响范围：定义空白新项目的骨架：配置、日志、数据库连接与迁移、健康检查、Task、优雅退出；无业务逻辑。 -->
# Spec 01：项目骨架与基础设施

## 目标

一个能启动、能优雅退出、能连 PostgreSQL 并执行迁移、能响应健康检查的空服务。这是后续一切的地基，本 Spec 不包含任何机器人/插件逻辑。

## 前置依赖

- 本机：Go 1.26+、Docker（PostgreSQL）、Task（taskfile.dev）。
- 已阅读 `00-总体设计与开发决策.md`。

## 设计决策

- 配置：环境变量优先（`DB_*`、`NAPCAT_TOKEN`、`WS_PORT`、`JWT_SECRET`、`SUPER_ADMIN_QQ`、`WEBUI_PASSWORD`、`LOG_LEVEL`），CLI 参数可覆盖，`.env` 仅本地开发且不入库。
- 日志：zap 结构化日志，`LOG_LEVEL=debug|info|warn|error`，生产默认 info。
- 数据库：`github.com/jackc/pgx/v5` 连接池；迁移用 `golang-migrate`（SQL 文件配对 `NNNNNN_description.up.sql` / `.down.sql`，嵌入二进制）。
- 健康检查：HTTP `GET /healthz`（含 DB ping），供 docker-compose healthcheck 与部署探针。
- 优雅退出：收到 SIGINT/SIGTERM 后关闭 HTTP、关闭连接池，超时强制退出。
- 测试不依赖真实数据库：迁移执行器抽象为接口，单测覆盖错误路径；真实迁移在 `task compose-up` 起的 PG 上手动验证。

## 实现任务（按序）

1. `go mod init github.com/w1ndys/w1ndys-bot-reborn`；安装依赖（viper、pflag、zap、pgx、golang-migrate）。
2. `internal/config`：定义 `Config` 结构、`Load()`（env + flags + 校验：JWT_SECRET ≥32 字节、WEBUI_PASSWORD ≥12 字符、SUPER_ADMIN_QQ 非空、DB 必填项）。
3. `internal/logger`：zap 初始化，按 `LOG_LEVEL` 和 `LOG_FORMAT` 切换开发/生产格式。
4. `internal/db`：`pgxpool` 连接池 + `Migrate(ctx)` 执行嵌入的迁移（up）；`cmd/migrate` 提供 `up`/`down` 子命令。
5. 首个迁移 `000001_bootstrap.up.sql` / `.down.sql`：仅建 `schema_migrations` 占位或最小平台表（本 Spec 先只建 `system_meta` 表验证迁移链路，业务表在 Spec 03/04/06 新增配对迁移）。
6. `cmd/bot`：装配 config→logger→db→HTTP 健康检查，`signal.NotifyContext` 优雅退出。
7. `Taskfile.yml`：`setup`（下载模块）、`run`、`migrate-up`、`migrate-down`、`test`、`lint`、`compose-up`（PostgreSQL + 应用）、`compose-down`，每个 Task 必须有 `desc`。
8. `compose.yml`：postgres:16（固定数据卷）、bot 服务（依赖 DB healthy）。
9. `.env.example`：全部环境变量占位与注释；`.gitignore` 忽略 `.env`。

## 测试

- `config`：合法配置通过；缺 `JWT_SECRET`、短密码、空 `SUPER_ADMIN_QQ` 报错；CLI 覆盖 env。
- `db`：迁移执行器接口的错误路径（迁移文件缺失、SQL 错误、重复执行 up 幂等）。
- 手动：`task compose-up` 后 `task migrate-up` 幂等执行；`GET /healthz` 返回 200 且含 db 状态；Ctrl+C 优雅退出日志完整。

## 验收清单

- [ ] `task lint`、`task test`、`git diff --check` 全绿。
- [ ] `task compose-up` 起 PG，`task migrate-up` 两次执行均成功（幂等）。
- [ ] `curl localhost:PORT/healthz` 返回 `{"status":"ok","db":"up"}`。
- [ ] SIGINT 优雅退出：日志出现关闭顺序，连接池关闭无泄漏报错。
- [ ] `.env` 未被 git 跟踪；`.env.example` 包含全部变量。
- [ ] 提交：`feat(bootstrap): 🚀 项目骨架与基础设施`。

## 边界与风险

- 本 Spec 不建任何业务表；后续 Spec 的表都通过新增配对迁移，**不得修改 000001**。
- 迁移一旦被部署版本锁定，不允许原地修改；这是后续所有 Spec 的硬约束。
