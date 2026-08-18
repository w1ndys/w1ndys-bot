# 插件开发指南

当前插件架构以编译期 `PluginSpec` 为唯一注册模型。开始开发前先阅读 `.agents/skills/plugin-development/SKILL.md`，并按任务涉及范围读取其 runtime、storage、admin-webui 与 testing-review 契约。

## 最小接入

插件导出 `Spec(依赖) (plugin.PluginSpec, error)`，由 `cmd/bot/main.go` 显式构造并装入 `SpecCatalog`。稳定 Key 使用 `^[a-z][a-z0-9_]{0,63}$`，发布后不要随意更名。

命令声明稳定 Key、触发词、群作用域、非空 `AllowedRoles` 和 Handler。允许身份为 `super_admin`、`group_owner`、`group_admin`、`group_member`。Handler 接收的上下文已经通过：

```text
命令匹配 → 全局 Ready → 群 Enabled → 群作用域 → 身份解析 → 代码角色
```

观察器声明稳定 Key 和平台支持的群事件类型，经过全局与群门禁但不执行命令角色授权。后台任务只由全局生命周期启动；每次群副作用前重新检查该群门禁。私聊不进入普通插件链，QQ 应急管理属于平台服务。

`plugins/echo/` 是命令插件样板，`plugins/keyword_reply/` 是群隔离业务表和专属页面样板，`plugins/forbidden_message_monitor/` 是复杂工作流、配置、生命周期及外部副作用样板。

## 状态与存储

- 有限标量运行设置：`ConfigSchema` + `plugin_runtime_configs`。
- 增长、分页、筛选、关联、审核或审计记录：插件自有表、Repository 和 Service。
- 复杂业务管理：语义化专属 API 和编译期 Vue 页面。

群业务表必须使用可信路径/上下文的 `group_id` 隔离；Body 不接受群号。SQL 固定且参数化，时间使用 `TIMESTAMPTZ`/UTC。业务写入按需要使用事务和乐观锁；不可变运行快照仅在校验与持久化成功后发布，消息热路径不查询数据库。

Secret 字段只写不读，省略更新表示保留原值。不得把凭据、原始私密消息或外部服务秘密写入响应、错误、日志、审计或规格声明。

## 生命周期和副作用

`OnEnable`/`OnDisable` 必须幂等、可取消、panic-safe 且有界。启用准备完成后才能进入 `ready`；禁用先停止准入、排空调用，再释放 goroutine、定时器、连接和订阅。外部调用必须有超时并尊重上下文。

数据库事务不能与不可逆 OneBot、模型或 HTTP 调用伪装成原子操作。需要可靠重试时使用明确的 job/outbox 边界。管理端触发实时副作用前必须重新检查全局和群门禁。

## WebUI 与 API

平台负责登录、管理员授权、可信群上下文、请求 ID、严格 JSON、限额、审计、错误映射和全局 Toast。插件负责领域校验、事务、Repository、快照刷新和外部副作用编排。

专属页面通过 `web/src/plugins/registry.ts` 的编译期映射注册。不得从服务端加载组件名、模块路径、URL、HTML、脚本或表达式。页面必须覆盖加载、空数据、失败、冲突、禁用、窄屏和重复提交状态。

## 测试与交付

使用本地 fake/mock，不依赖真实 NapCat。根据能力覆盖注册冲突、门禁、身份、生命周期、配置版本、跨群隔离、事务回滚、审计、严格输入、错误脱敏、并发快照和资源释放。

```bash
task lint
task test
task web-build
task web-e2e
go test -race ./internal/plugin ./plugins/your_plugin
git diff --check
```

每次代码修改后必须由独立 subagent 复核需求符合度、授权与输入校验、群隔离、事务/快照一致性、并发与资源、敏感信息、错误处理和测试遗漏，修复确认的问题后重新验证。
