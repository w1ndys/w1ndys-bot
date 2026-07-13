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

权限按以下优先级取首个匹配项：

```text
群级功能 > 群级插件 > 全局功能 > 全局插件 > Manifest 默认值
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
docs/                    设计与开发文档
```

## 4. 数据库迁移

程序启动时自动向上迁移。当前迁移版本为 5：

1. `plugin_config`：插件开关、优先级和 JSON 配置。
2. `system_settings`、`system_admins`、`admin_audit_logs`：系统管理基础表。
3. `plugin_definitions`、`plugin_features`：Manifest 元数据。
4. `plugin_commands`：全局及群级自定义命令。
5. `permission_policies`：插件或功能的角色权限覆盖。

每个版本同时提供 `up.sql` 与 `down.sql`，分别用于应用和回滚。不得修改已部署的迁移；结构变化应新增版本。

## 5. 开发与部署命令

所有任务从仓库根目录执行：

```bash
task setup              # 下载 Go 依赖
task run                # 本地启动机器人
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

敏感值仅写入未跟踪的 `.env`。首次部署应设置 `SUPER_ADMIN_QQ=你的QQ号`：启动时仅在数据库不存在该账号时写入 `system_admins`，以后以数据库启停状态为准，环境变量不会覆盖已有记录。Compose 内数据库名统一为 `w1ndys_bot`；删除容器不会删除具名卷，若需清空数据库必须明确移除卷。

## 6. 已完成能力

- [x] Viper 配置、pgx 连接池及 zap 日志层
- [x] Token 鉴权反向 WebSocket 与强类型事件模型
- [x] Action Client、类型化 BotAPI 和响应关联
- [x] PluginManager、Manifest + Factory 注册及元数据同步
- [x] 多作用域命令匹配、重复检测和五级权限解析
- [x] `ping` 插件端到端命令回复链路
- [x] 最高管理员环境引导、身份缓存与 QQ 插件管理命令
- [x] 命令别名 CRUD、事务审计与 Command Registry 热刷新服务
- [x] 数据库自动迁移及迁移管理任务
- [x] Dockerfile 与机器人/PostgreSQL Compose 编排

## 7. 后续开发计划

按以下顺序推进，每一步完成测试并独立提交：

1. 实现权限策略 CRUD，并在事务提交后原子热刷新权限快照。
2. 扩展系统设置与管理员 Repository，统一写入、缓存和审计。
3. 扩展 QQ 管理命令，使管理员可维护命令别名与群级权限。
4. 建设 WebUI 后端，包括认证、审计及管理 REST API。
5. 建设 Vue 3 WebUI，按 Manifest 配置描述渲染开关、表单和 CRUD 页面。

当前尚未实现 WebUI Admin Console。最高管理员可使用插件管理命令：`/插件列表`、`/启用插件 <名称>`、`/禁用插件 <名称>`、`/设置插件优先级 <名称> <整数>`；也可使用命令管理命令：`/命令列表`、`/新增全局命令 <插件> <功能> <命令>`、`/新增群命令 <群号> <插件> <功能> <命令>`、`/修改命令 <ID> <新命令>`、`/删除命令 <ID>`。系统 `admin` 插件不可通过管理服务禁用。所有管理变更最终必须同时经过授权校验、重复检测、热更新和审计记录。

## 8. 阶段验收

提交前至少运行：

```bash
task lint
task test
git diff --check
```

新增功能需覆盖正常、边界和错误路径。涉及 Action Client 时必须验证 `echo` 关联、超时、断连及事件处理期间的响应收包；涉及管理写操作时必须验证权限、事务、冲突检测和审计。
