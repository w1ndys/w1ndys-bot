<!-- 📌 影响范围：定义打包部署、安全基线、项目文档与最终验收；无新增业务逻辑。 -->
# Spec 09：打包部署与整体验收

## 目标

交付可部署的完整项目：Docker 多阶段构建（Go 二进制 + Vite 产物 + SPA 托管）、compose 一键启动、安全配置基线、README 与开发指南、以及全项目最终验收清单。

## 前置依赖

- Spec 01-08 全部完成。

## 设计决策

- 部署形态：单容器。多阶段 Dockerfile：`node:22` 构建 WebUI → `golang:1.26` 构建二进制（Vite 产物经 `go:embed` 打入）→ `alpine` 运行（CA 证书、时区数据）；Go 服务同端口托管 REST + WS + SPA。
- 配置：全部来自环境变量；`.env` 仅本地；生产用 compose 的 `env_file` 或部署平台的 secret 注入。
- 数据库：compose 内置 postgres:16 + 数据卷；迁移由 `cmd/migrate up` 在启动前执行（entrypoint 顺序），也可手动 `task migrate-up`。
- 健康检查：`/healthz` 供 compose healthcheck；容器启动依赖 DB healthy。
- 安全基线（部署硬性要求）：
  - `DB_PASSWORD`、`NAPCAT_TOKEN`、`JWT_SECRET`（≥32 字节）、`WEBUI_PASSWORD`（≥12 字符）、`SUPER_ADMIN_QQ` 全部必设，缺失则启动失败。
  - 生产 `LOG_LEVEL=info`（debug 会记录原始聊天内容与 QQ 标识）。
  - 容器以非 root 用户运行；不暴露 DB 端口到公网。
  - 不内建 TLS：`WEBUI_PORT`（默认 8080）纯 HTTP，本机/内网访问；需公网时由外部反代（nginx/caddy）加 TLS。
- 发布：`CHANGELOG.md` 记录；`docs/` 更新指南（含"通用记录表 vs 专属路径"判定表、新插件开发模板、部署手册）。

## 实现任务（按序）

1. `Dockerfile`（多阶段）、`.dockerignore`（排除 .env/node_modules/dist）、`compose.yml`（bot + postgres + healthcheck + 非 root）。
2. `cmd/migrate` 完善（up/down/version 子命令 + 帮助文本）。
3. `README.md`：项目简介、快速开始（docker compose up）、环境变量表、插件列表、文档索引。
4. `docs/guide.md`（开发指南）：事件链、状态管理、插件开发模板、通用记录表用法、专属路径判定表、测试与提交规范、安全配置。
5. `.env.example` 定稿；`CHANGELOG.md` 补充各 Spec 里程碑。
6. 全项目最终验收（见下）。

## 全项目验收清单

- [ ] `task setup` 后 `task lint`、`task test`（含 race）、`task web-build`、`git diff --check` 全绿。
- [ ] 干净环境 `docker compose up` 一键启动：迁移自动执行、健康检查通过、WebUI 可登录。
- [ ] 安全基线：缺任一关键环境变量启动失败；`.env` 未入库；容器非 root。
- [ ] 插件路径验证：
  - 记录表插件（如 keyword_reply）：WebUI 通用页全流程（群选择/增删改/搜索/CSV 导入导出）可用；群里立即生效。
  - 标量配置插件：通用配置页生效。
  - 专属插件（invite_tree）：专属页 + API 可用。
  - QQ 应急命令：启停与 WebUI 状态一致、审计完整。
- [ ] 数据一致性：重启后状态/配置/记录保留；版本迁移 up/down 可回滚。
- [ ] 代码审查以人工可读性为准：无过度抽象（无提前引入的接口/封装层）、无反射与 `any` 滥用、规范由编译期约束保证（`gofmt`/`go vet`/`go build` 通过即可拦截不合规用法）。
- [ ] 提交：`feat(deploy): 🚢 打包部署与整体验收`，然后打 tag 发布。

## 边界与风险

- 迁移一旦随发布部署，**任何结构变更只能新增配对迁移**；本指南所有 Spec 均遵守。
- 生产环境升级顺序：备份 DB → `migrate up` → 新镜像滚动发布 → 验证 `/healthz` 与关键插件。
- 本项目定位单管理员/小规模群，不需要多实例、不需要 K8s；若未来规模扩大，先加只读副本与备份策略。
