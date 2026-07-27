# 变更日志

本文件记录 `w1ndys-bot` 的重要变更。发布标签采用北京时间日历版本 `vYYYY.MM.DD.HHmm`，精确到分钟且不使用自增序号；分类参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [Unreleased]

## [2026.07.27.1615] - 2026-07-27 16:15 CST

### 变更

- 插件运行统一迁移至 `PluginSpec`、Dispatcher 与 `RuntimeService`，删除旧 Manifest/Manager、数据库命令和权限矩阵及通用资源工作台。
- 数据库迁移在首次发布前重建为单一初始基线；WebUI 统一使用插件运行页和编译期专属业务页面。
- QQ 应急管理入口与 WebUI 复用同一运行状态、乐观锁、审计和生命周期服务。

### 新增

- 新增 Keyword Reply 与 Forbidden Monitor 专属管理 API、编译期 WebUI 页面和完整端到端测试。
- 新增 Git Tag 驱动的 GitHub Actions 发布流程，提供 GHCR amd64/arm64 镜像、构建来源证明和 GitHub Release。
- Compose 支持通过 `BOT_IMAGE` 部署不可变版本镜像，并提供发布、升级和回滚手册。

### 安全

- 插件配置审计按 `ConfigSchema` 脱敏 write-only secret，避免大模型密钥进入审计快照。
- 进程退出时停止插件准入、排空在途调用并逆序释放生命周期资源。
- 发布流程校验 Tag 日期和默认分支归属，固定第三方 Action 提交，并拒绝覆盖已有版本镜像。

### 兼容性

- 项目尚未上线，本版本将旧 `000001`–`000020` 迁移替换为单一 `000001_initial_schema`；已有测试数据库应重新创建，不支持从旧开发期迁移链原地升级。

## [2026.07.14.1517] - 2026-07-14 15:17 CST

### 新增

- NapCat OneBot 11 反向 WebSocket、强类型事件模型与 Token 鉴权。
- 支持超时、断连和并发响应关联的 OneBot Action Client。
- 基于 `Manifest + Factory` 的编译时插件注册、元数据同步和运行时管理。
- 全局/群级命令触发词、重复检测和热更新。
- 角色及指定 QQ 用户的多层权限策略与回退解析。
- 单最高管理员 WebUI 登录、插件管理、命令、权限、系统设置和审计日志页面。
- PostgreSQL 自动迁移、Docker Compose 编排和 WebUI 生产静态托管。
- zap 结构化日志、OneBot 原始事件 debug 日志及请求 ID。
- Playwright 无头 Chromium 桌面、平板、手机端到端测试。

### 安全

- 登录限流、JWT 会话、HTTP 超时、严格 JSON、CSP 和管理接口统一鉴权。
- 审计详情服务端敏感字段脱敏，列表接口不读取或返回完整快照。
- 密钥由环境变量或 CLI 部署参数提供，不进入数据库，WebUI 不保存或修改管理员凭据。

### 修复

- 修复 PostgreSQL 持久卷密码、数据库名和 TLS 配置不一致时的部署问题。
- 修复插件命令页首次进入未加载功能并误报空功能的问题。
- 修复启动期 API 401 丢失原访问路径的问题。
- 修复权限策略事务锁键包含 NUL 导致 PostgreSQL 写入失败的问题。

[Unreleased]: https://github.com/w1ndys/w1ndys-bot/compare/v2026.07.27.1615...HEAD
[2026.07.27.1615]: https://github.com/w1ndys/w1ndys-bot/compare/v2026.07.14.1517...v2026.07.27.1615
[2026.07.14.1517]: https://github.com/w1ndys/w1ndys-bot/releases/tag/v2026.07.14.1517
