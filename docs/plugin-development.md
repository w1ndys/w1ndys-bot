# 插件开发指南

本文说明如何基于当前编译时插件框架实现业务插件。完整可运行参考位于 `plugins/echo/`；它是默认编译进机器人、但首次同步后默认关闭的正式示例插件。

## 插件接入模型

插件由 `Manifest + Factory + Plugin` 三部分组成：

```text
blank import → init 注册 Registration → 启动同步 Manifest
             → Factory 注入 Runtime → PluginManager 管理实例

消息 → Command Registry 匹配 plugin_name.feature_key
     → Permission Resolver 鉴权 → Handle(ctx, event)
```

- `Manifest`：稳定名称、展示信息、优先级和功能元数据。
- `FeatureManifest`：功能键、默认触发词和默认角色权限。
- `Factory`：接收主程序提供的 `Messenger`、`Management` 等依赖。
- `Plugin`：实现名称、事件处理和启停生命周期。

当前插件使用仓库内 `internal/` 契约，因此应放在本仓库模块内开发。框架尚未提供可供外部 Go Module 直接导入的稳定 SDK。

## 创建插件

1. 复制示例并重命名目录：

   ```bash
   cp -R plugins/echo plugins/my_plugin
   ```

2. 修改包名、Manifest `Name` 和实现的 `Name()`。稳定标识必须匹配：

   ```text
   ^[a-z][a-z0-9_]{0,63}$
   ```

   `plugin_name` 和 `feature_key` 会进入数据库、命令、权限和审计引用，发布后不要随意修改。

   `plugins/echo/constants.go` 集中维护待修改的插件常量。复制示例后，只需修改插件名称、展示信息、优先级、功能键、默认命令和默认权限开关；保持 `plugin.go` 中的 `init` 只负责绑定集中 Manifest 与 Factory。插件随主程序源码一起编译和发布，不维护独立版本。

3. 为每项独立能力声明一个 `FeatureManifest`：

   ```go
   plugin.FeatureManifest{
       Key:             "hello",
       DisplayName:     "问候",
       Description:     "向消息发送者问好",
       DefaultCommands: []string{"你好机器人", "hello"},
       DefaultPermissions: plugin.RolePermissions{
           SuperAdmin: true,
           GroupOwner: true,
           GroupAdmin: true,
           Member:     true,
       },
   }
   ```

   Manifest 中缺失的默认命令会在每次启动同步时补回。管理员删除或改名 `DefaultCommands` 中的词后，重启会重新出现原默认词；只把框架必须长期提供的稳定触发词放入默认命令。若默认词与管理员自定义命令或另一插件的默认词冲突，同步会明确失败并回滚，开发者必须先更换触发词。数据库中的显式权限规则和插件启用状态不会被 Manifest 同步覆盖，默认权限仅在没有显式规则命中时作为最终回退。

4. 在 `Handle` 中读取已匹配功能：

   ```go
   invocation, ok := plugin.InvocationFromContext(ctx)
   // [决策理由] 广播事件没有命令调用信息，不属于定向功能。
   if !ok {
       return nil
   }
   switch invocation.FeatureKey {
   case "echo":
       // invocation.Command 是实际匹配的触发词
       // invocation.Arguments 是命令后的参数
   default:
       return nil
   }
   ```

   命令路由已经完成触发词匹配、参数提取和权限检查。`Invocation.Arguments` 会合并分隔空白但保留参数大小写，并支持管理员配置的多词触发词。处理成功且不希望后续插件继续收到事件时，返回 `plugin.ErrStopPropagation`。

5. 使用注入依赖，不要在插件里自行建立 WebSocket 或绕过管理服务访问数据库：

   ```go
   func newPlugin(runtime plugin.Runtime) (plugin.Plugin, error) {
       if runtime.Messenger == nil {
           return nil, errors.New("缺少 Messenger")
       }
       return &implementation{messenger: runtime.Messenger}, nil
   }
   ```

   - `Messenger.ReplyToMessage`：构造带引用的文本回复。
   - `Messenger.Reply`：发送普通 OneBot 消息内容。
   - `Management`：仅系统管理类插件需要；调用会经过鉴权、审计和热更新。

6. 在 `cmd/bot/main.go` 添加 blank import：

   ```go
   _ "github.com/w1ndys/w1ndys-bot/plugins/my_plugin"
   ```

   没有 import 的包不会执行 `init`，也不会进入 Catalog 或生产二进制。

## 事件与生命周期

命令插件通常只接受 `*ws.MessageEvent`。类型断言前先确认 `feature_key` 属于当前插件；无关事件返回 `nil`，不要记录错误。观察型插件可以接收未匹配命令的广播事件，但必须快速返回，耗时外部调用应遵守上下文取消和超时。

`OnEnable` 和 `OnDisable` 可能因启动恢复、WebUI 热切换而多次调用：

- `OnEnable` 必须幂等，避免重复启动 goroutine 或重复注册资源。
- `OnDisable` 必须释放定时器、goroutine、连接和订阅。
- 后台任务必须监听传入上下文或自有取消函数。
- 不要让生命周期方法永久阻塞 PluginManager。

## 命令、权限与优先级

- 一个插件可以有多个功能，每个功能可以配置多个全局或群级触发词。
- 重复触发词由管理服务在同一作用域内拒绝。
- 指定用户权限优先于角色权限；群级规则优先于全局规则。
- `Manifest.Priority` 是首次同步默认优先级，数值较大者先处理广播事件。
- 群消息插件如需平台统一逐群启停，应声明 `Manifest.GroupControllable: true`；不要在插件业务表或 `config_json` 中重复实现群开关。
- 定向命令通过 `HandleNamed` 进入目标插件，不依赖广播优先级。
- 普通业务插件保持 `System: false`；系统插件具有额外禁用保护，不应滥用。

## 配置与数据

普通插件可选择实现 `plugin.Configurable`，通过 `ConfigSchema` 声明扁平配置字段，并在 `ValidateConfig` 中执行领域校验、在 `ApplyConfig` 中原子发布不可变运行快照。平台提供 Schema、脱敏读取和带 `config_version` 乐观锁的更新接口；省略 `secret` 字段表示保留原值，读取响应和审计均不会包含秘密。`ApplyConfig` 返回错误时不得部分提交，平台会补偿数据库旧快照并再次恢复运行态。

WebUI 会在插件概览中按 `string`、`multiline`、`integer`、`boolean`、`enum` 和 `secret` 六种字段类型生成通用表单。保存使用读取时的版本号；发生冲突时保留当前草稿并要求重新加载。只有通用字段无法安全表达工作流、图表或多资源事务时，才新增插件专属页面。

启动时，`PluginManager` 会在 `OnEnable` 前恢复并校验 `plugin_config.config_json`。普通小型设置放入该 JSON；持续增长、需要查询关系或独立事务的业务记录仍应使用版本化 SQL Migration 和插件专属 Repository，不要把配置 JSON 当作文档数据库，也不要在 `Handle` 内散落 SQL。

需要管理重复业务记录时，插件可实现 `plugin.AdminResourceProvider`，返回固定资源键、字段描述和插件自有 `AdminResourceHandler`。平台统一处理 WebUI 鉴权、分页边界、严格字段校验、资源路由和 HTTP 错误映射；插件处理器只能使用固定 SQL，并负责在同一事务内完成领域写入、乐观锁和审计。`plugins/keyword_reply/` 是首个完整样板。

关键词回复插件把规则保存在独立表中，运行时使用原子不可变映射执行群消息 `RawMessage` 完全相等匹配。规则 CRUD 提交后根据事务前后像增量发布快照，不在消息热路径查询数据库，也不会对机器人自身的 `message_sent` 事件触发回复。

平台群控制使用“全局开关 → 群开关 → 用户权限 → 插件处理”链路。无单群覆盖时继承 `group_default_enabled`，删除覆盖即恢复继承；管理写入使用独立版本 CAS、事务审计和群门禁局部热刷新。非群插件、系统管理插件和私聊事件不受群门禁影响。

不得把 Token、密码或用户隐私写入 Manifest、日志或错误文本。原始消息、QQ号和URL只应在必要的 debug 场景记录。

## 测试与验收

插件测试应使用 fake `Messenger`，不要依赖真实 NapCat 或 PostgreSQL。至少覆盖：

- Manifest 校验和功能键数量。
- Factory 缺少依赖时失败。
- 每个 `feature_key` 的正常回复。
- 非目标功能和非支持事件类型。
- Messenger 返回错误。
- 生命周期重复调用和资源释放（如插件使用后台资源）。

运行完整检查：

```bash
task lint
task test
task web-e2e
git diff --check
```

首次集成后执行 `task compose-rebuild`，在 WebUI 中确认：插件显示为 available、功能元数据正确、默认命令无冲突、权限符合预期。新插件默认关闭，需要管理员明确启用后再验证实际回复。

## 发布前检查清单

- 插件名和功能键稳定，发布后不随意重命名。
- 每个功能都有说明、默认命令和最小默认权限。
- Factory 校验所有必需依赖。
- `Handle` 对无关事件安静返回。
- 外部调用有超时并尊重 `context.Context`。
- 启停生命周期幂等且无 goroutine 泄漏。
- 错误包含业务上下文但不泄露凭据。
- 正常、边界和错误路径测试全部通过。
- 已在 `cmd/bot/main.go` 显式 blank import。
