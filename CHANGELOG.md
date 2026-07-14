# 变更日志

本文件记录 `w1ndys-bot` 的重要变更。发布标签采用北京时间日历版本 `vYYYY.MM.DD.HHmm`，精确到分钟且不使用自增序号；分类参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [Unreleased]

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

[Unreleased]: https://github.com/w1ndys/w1ndys-bot/compare/v2026.07.14.1517...HEAD
[2026.07.14.1517]: https://github.com/w1ndys/w1ndys-bot/releases/tag/v2026.07.14.1517
