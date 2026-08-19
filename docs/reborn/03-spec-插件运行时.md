<!-- 📌 影响范围：定义插件运行时：PluginSpec、Catalog、Dispatcher、全局/群门禁、生命周期、身份解析；这是"插件加载机制"的核心。 -->
# Spec 03：插件运行时（加载机制核心）

## 目标

实现编译期插件的加载机制：`PluginSpec` → `Catalog`（启动校验）→ `Dispatcher`（唯一执行链）→ 全局/群门禁 → 代码身份授权 → Handler。引入第一个插件 Echo 验证最小链路。这是本项目的灵魂 Spec，后续所有插件都建立在这条链上。

## 前置依赖

- Spec 01（数据库、配置）、Spec 02（WS 事件、Action Client）。
- 阅读 `00-总体设计与开发决策.md` §3 安全基线与执行链。

## 设计决策

- 插件编译期内置：`plugins/{key}` 包实现 `Spec() PluginSpec`，`cmd/bot` 显式装入 `Catalog`。不做动态加载。
- `PluginSpec`（全部编译期、代码声明）：

```go
type PluginSpec struct {
    Key          string        // 稳定小写 key，如 keyword_reply
    DisplayName  string
    Description  string
    Commands     []CommandSpec // 可空
    Observers    []ObserverSpec // 可空
    ConfigSchema *ConfigSchema // 可空（Spec 04 细化）
    RecordTable  *RecordTable   // 可空（Spec 06 引入）
    AdminPageKey string         // 可空（专属页面，Spec 08）
    Lifecycle    Lifecycle      // 可空
}

type CommandSpec struct {
    Key          string
    Triggers     []string     // 命令触发词，如 "查faq"
    AllowedRoles RoleSet      // super_admin / group_owner / group_admin / group_member
    Handler      func(CommandContext) error
}

type ObserverSpec struct {
    Key        string
    EventKinds []ObserverEventKind // 平台支持的群事件类型
    Handler    func(ObserverContext) error
}

type Lifecycle interface {
    OnEnable(context.Context) error  // 发布快照、启动后台任务
    OnDisable(context.Context) error // 清空快照、取消任务
}
```

- 唯一执行链（与现行版一致，不可绕行）：

```text
事件规范化 → 命令匹配 → 全局 Ready → 群 Enabled → 命令作用域(仅群) → 身份解析 → AllowedRoles 包含 → Handler
未命中命令的群事件 → 观察器门禁：全局 Ready → 群 Enabled → Observer
```

- fail-closed：无全局状态记录等同关闭，无群状态记录等同关闭；`enabling/disabling/failed` 均拒绝新调用；身份未知或解析失败拒绝；私聊不进入普通插件链；超级管理员不隐式绕过命令授权。
- 状态分离：数据库存管理员意图（`desired_enabled`、群 `enabled`），进程内存存实际状态（`runtime_status`）。启停使用乐观锁 + 审计（审计接口在 Spec 04 实现，本 Spec 先定义接口并在日志打点）。
- 生命周期：`enabling`（OnEnable，失败回滚为 `failed`）→ `ready`；禁用先停止准入、等待在途 Handler、再 `OnDisable`。方法必须幂等、可取消、隔离 panic。
- 身份解析：`SUPER_ADMIN_QQ` 环境变量为超级管理员；群主/群管/群成员从平台级"群成员同步服务"的内存快照解析；快照缺失时按需调用 `GetGroupMemberInfo` 兜底并回写快照；解析失败 fail-closed。
- 群成员同步服务（平台级能力，非插件）：定时（如每 30 分钟）调用 `get_group_list` 获取群列表，再对每个群调用 `get_group_member_list` 获取成员，持久化到 `group_members` 表并发布内存快照（atomic.Pointer）；监听 `notice` 的群成员增加/减少事件，仅刷新对应群；提供只读查询接口供身份解析与插件使用。数据规模小（几十群 × 数百人），全量快照可接受。
- 运行快照：需要热路径查询数据的插件在 `OnEnable` 时从数据库加载不可变快照（atomic.Pointer），管理写操作后通过回调刷新。消息热路径零 SQL。

## 实现任务（按序）

1. `internal/plugin`：`spec.go`（上述契约 + 启动校验：重复 Key/命令 Key/观察器 Key/触发词、空身份集合、空观察事件集合、不支持的事件类型均拒绝）、`catalog.go`、`dispatcher.go`（命令分发 + 观察器分发）。
2. 平台状态表迁移 `000002_plugin_states`（配对 up/down）：
   - `plugin_states(plugin_key PK, desired_enabled bool, version int8, updated_at timestamptz)`
   - `plugin_group_states(plugin_key, group_id, enabled bool, version int8, updated_at, PK(plugin_key, group_id))`
   - `plugin_runtime_configs(plugin_key PK, config_json jsonb, version int8, updated_at)`
3. `runtime_state_repository.go`：状态读写（乐观锁）、`LoadAllEnabledGroups`（供快照）、迁移数据由 Spec 04 审计补齐。
4. `runtime_controller.go`：`Enable(ctx, actor, pluginKey)` / `Disable(...)` / 群开关 `EnableGroup/DisableGroup`：乐观锁 + 状态机 + OnEnable/OnDisable 编排 + panic 隔离 + 审计接口打点。
5. 迁移 `000003_group_members`（配对 up/down）：`group_members(group_id, user_id, role text, nickname text, card text, joined_at timestamptz, updated_at timestamptz, PK(group_id, user_id))`；`group_member_store.go`：DB repository（批量 upsert / 按群加载 / 按人查询）+ 内存快照发布（atomic.Pointer）。
6. `group_sync.go`：定时同步任务（每 30 分钟：`get_group_list` → 逐群 `get_group_member_list` → upsert → 发布快照）+ 监听 `notice` 群成员增加/减少事件触发的单群刷新；同步失败告警但不清空旧快照。
7. `identity_resolver.go`：`SUPER_ADMIN_QQ` + 从群成员快照解析当前群身份（群主/群管/群成员）；快照无记录时兜底 `GetGroupMemberInfo` 并回写。
8. `runtime_bootstrap.go`：启动时读取 DB 意图，对 `desired_enabled=true` 的插件逐个 `Enable`；DB 不可用则全部保持 disabled 并告警（fail-closed）。
9. 插件 `plugins/echo`：命令触发词 `echo <text>`，`AllowedRoles=group_member`，Handler 原样回复；验证最小链路。注册到 Catalog。
10. `plugins/admin` 暂缓（QQ 应急入口在 Spec 08 做），本 Spec 只做平台控制台日志输出。

## 测试

- Catalog 校验：重复 Key、重复触发词、空角色、非法事件类型 → 启动失败并给出明确错误。
- Dispatcher：全局未 ready / 群未 enabled / 身份不足 / 私聊 → 全部拒绝（fail-closed 表驱动）。
- 生命周期：Enable→Ready 顺序；OnEnable 失败→failed；Disable 期间新调用被拒；幂等重复 Enable/Disable；panic 被隔离。
- 群成员同步：定时任务拉取群列表与成员并持久化；notice 进群/退群事件只刷新对应群；快照查询正确；同步失败保留旧快照并告警。
- 身份解析：SUPER_ADMIN_QQ 命中；群主/群管/群成员从快照解析；快照缺失时兜底调用解析；解析失败拒绝。
- Echo 插件：`echo` 命令触发回复；未启用时无响应。

## 验收清单

- [ ] `task lint`、`task test`（含 race）全绿。
- [ ] 启动时插件默认全部 disabled；WebUI 尚不存在，先用 `task migrate-up` + 日志确认状态。
- [ ] 手动（或集成测试）：启用 echo → 群里发 `echo hi` 被回复；关闭 → 无响应。
- [ ] 所有校验失败场景有明确错误日志，进程不崩溃。
- [ ] 提交：`feat(plugin): 🧩 插件运行时与 Echo 最小链路`。

## 边界与风险

- 本 Spec 不引入任何业务表、WebUI、QQ 管理命令；它们是后续 Spec 的职责。
- 状态机必须唯一：禁止插件自行查门禁/绕行 Dispatcher 注册。
- 观察器与后台任务的群副作用必须在执行前重查群门禁。
- `plugin_states` 等表在 Spec 04 会补审计，本 Spec 表结构即最终形态，后续只新增迁移不修改。
