# W1ndys Bot 开发指南

## 1. 项目目标

本项目是基于 Go 与 NapCat OneBot 11 的 QQ 机器人框架。NapCat 通过反向 WebSocket 连接机器人；机器人使用 PostgreSQL 保存插件元数据、命令、权限与系统配置。业务能力以编译时插件交付，并通过数据库配置控制运行状态。

## 2. 当前架构

消息处理链路如下：

```text
NapCat 事件 → WebSocket 解析 → 命令匹配 → 权限解析
             → PluginManager → 插件实例 → BotAPI → Action Client → NapCat
```

Action Client 使用唯一 `echo` 关联请求与响应。WebSocket 读取循环优先处理响应，普通事件交给受限并发 worker，避免插件等待 Action 响应时阻塞收包。

插件以 `Manifest + Factory` 注册：Manifest 描述插件、功能、默认命令及默认权限；Factory 在运行时接收 `Messenger` 等依赖并创建实例。插件默认关闭，Manifest 同步不会替管理员覆盖现有配置。

权限按以下优先级取首个匹配项，指定用户策略整体优先于角色策略：

```text
用户：群级功能 > 群级插件 > 全局功能 > 全局插件
角色：群级功能 > 群级插件 > 全局功能 > 全局插件
最终回退：Manifest 默认值
```

## 3. 目录结构

```text
cmd/bot/                 程序入口与依赖装配
internal/config/         Viper 配置加载
internal/db/             pgx PostgreSQL 连接池
internal/ws/             反向 WS、事件模型与 Action Client
internal/onebot/         类型化 BotAPI
internal/plugin/         Manifest、注册、同步与运行管理
internal/command/        多作用域命令注册及重复检测
internal/permission/     多级权限解析
internal/migration/      迁移执行器与 SQL 文件
pkg/logger/              zap 结构化日志适配层
plugins/ping/            ping 示例插件
plugins/admin/           不可关闭的 QQ 系统管理插件
web/                     Vue 3 + TypeScript 管理界面
docs/                    设计与开发文档
```

## 4. 数据库迁移

程序启动时自动向上迁移。当前迁移版本为 8：

1. `plugin_config`：插件开关、优先级和 JSON 配置。
2. `system_settings`、`system_admins`、`admin_audit_logs`：系统管理基础表。
3. `plugin_definitions`、`plugin_features`：Manifest 元数据。
4. `plugin_commands`：一个插件功能对应的全局及群级触发词。
5. `permission_policies`：插件或功能的角色及指定用户权限覆盖。
6. 将插件及功能的 `installed` 字段重命名为语义更准确的 `available`。
7. 单管理员模式改由 `SUPER_ADMIN_QQ` 作为唯一权限根，删除废弃的 `system_admins` 表及动态 WebUI 标题设置。
8. 权限主体扩展为角色或指定 QQ 用户，支持全局/群级功能和插件全功能授权。

每个版本同时提供 `up.sql` 与 `down.sql`，分别用于应用和回滚。不得修改已部署的迁移；结构变化应新增版本。

## 5. 开发与部署命令

所有任务从仓库根目录执行：

```bash
task setup              # 下载 Go 依赖
task run                # 本地启动机器人
task web-dev            # 启动 WebUI 并代理本机 18800 API
task web-build          # 类型检查并构建 WebUI
task lint               # 检查 gofmt 与 go vet
task test               # 运行全部测试
task compose-up         # 构建并启动 bot 与 PostgreSQL
task compose-rebuild    # 重建镜像并强制重建容器
task compose-restart    # 重新读取 .env 并重建容器
task compose-logs       # 持续查看容器日志
task migrate-version    # 查看迁移版本与 dirty 状态
task migrate-up         # 应用待执行迁移
task migrate-down       # 回滚最近一个版本
```

敏感值仅写入未跟踪的 `.env`。首次部署应设置 `SUPER_ADMIN_QQ=你的QQ号`、至少 12 字符的 `WEBUI_PASSWORD` 和至少 32 字节的 `JWT_SECRET`。系统采用单管理员模式，QQ 与 WebUI 仅信任 `SUPER_ADMIN_QQ`；管理员账号和密码均不入库、不支持页面修改，轮换后需重启容器。Compose 内数据库名统一为 `w1ndys_bot`；删除容器不会删除具名卷，若需清空数据库必须明确移除卷。

WebUI 产品名称固定为 `w1ndys-bot-webui`，不作为系统设置开放修改。

所有数据库时间字段使用 `TIMESTAMPTZ`，PostgreSQL 会话固定为 UTC；WebAPI 统一输出 UTC RFC3339 时间，前端按浏览器所在时区转换展示。

排查 OneBot 上报时可临时设置 `LOG_LEVEL=debug`；`internal/ws` 会以 `payload` 字段输出解析成功的原始事件 JSON。设置 `LOG_FORMAT=json` 可让整行日志使用 JSON 编码。原始事件可能包含聊天内容、QQ 标识、群组信息、文件名或 URL 等敏感数据，生产环境不应长期启用 debug，并应限制日志访问范围与留存时间。

## 6. 已完成能力

- [x] Viper 配置、pgx 连接池及 zap 日志层
- [x] Token 鉴权反向 WebSocket 与强类型事件模型
- [x] Action Client、类型化 BotAPI 和响应关联
- [x] PluginManager、Manifest + Factory 注册及元数据同步
- [x] 多作用域命令匹配、重复检测和用户优先的八级权限解析
- [x] `ping` 插件端到端命令回复链路
- [x] 最高管理员环境引导、身份缓存与 QQ 插件管理命令
- [x] 多功能触发词 CRUD、重复检测、事务审计与 Command Registry 热刷新服务
- [x] WebUI 功能触发词 REST API 与统一前置鉴权
- [x] WebUI 角色/指定用户权限策略 REST API、审计与热刷新
- [x] 权限策略 CRUD、事务审计与 Permission Resolver 热刷新服务
- [x] `SUPER_ADMIN_QQ` 单管理员授权及 WebUI 环境密码认证
- [x] WebUI 插件列表、启停、优先级 REST API 与审计请求关联
- [x] 受控系统设置、JSONB 审计、原子快照与命令前缀热更新
- [x] WebUI 受控系统设置 REST API、覆盖状态与恢复默认
- [x] WebUI 审计日志分页、筛选与只读详情 REST API
- [x] Vue 3 WebUI 登录、会话路由守卫与插件管理首屏
- [x] 插件功能元数据 API 与 WebUI 功能触发词管理页面
- [x] WebUI 权限策略筛选、角色/指定用户授权与回退删除页面
- [x] 登录限流、HTTP 超时、严格 JSON、CSP 和请求 ID 安全加固
- [x] 数据库自动迁移及迁移管理任务
- [x] Dockerfile 与机器人/PostgreSQL Compose 编排

## 7. 后续开发计划

按以下顺序推进，每一步完成测试并独立提交：

1. 继续建设 Vue 3 WebUI，接入设置和审计页面。
2. 补充 WebUI 生产构建与 Go 静态资源托管，实现 Compose 一体化部署。

WebUI Admin Console 已实现插件、功能触发词和权限策略页面，设置与审计页面仍待接入。QQ 通道仅作为轻量应急入口，最高管理员可使用 `/插件列表`、`/启用插件 <名称>` 和 `/禁用插件 <名称>`；功能触发词、权限策略、优先级及复杂配置统一由 WebUI 管理，避免维护两套高风险 CRUD 交互。系统 `admin` 插件不可通过管理服务禁用。所有管理变更必须同时经过授权校验、重复检测、热更新和审计记录。

## 8. 阶段验收

提交前至少运行：

```bash
task lint
task test
git diff --check
```

新增功能需覆盖正常、边界和错误路径。涉及 Action Client 时必须验证 `echo` 关联、超时、断连及事件处理期间的响应收包；涉及管理写操作时必须验证权限、事务、冲突检测和审计。
