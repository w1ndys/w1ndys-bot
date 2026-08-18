<!-- 📌 影响范围：用通用记录表能力实现三个示例插件，验证"零 WebUI/API 成本"路径；每个插件是后续同类插件的模板。 -->
# Spec 07：记录表插件示例（验证零成本路径）

## 目标

用 Spec 06 的通用记录表实现三个典型插件：关键词回复（观察器）、FAQ（命令）、群违禁词（观察器），外加一个标量配置插件示例。每个插件**零专属 API、零专属 Vue 页**，WebUI 直接用通用记录表页/配置页。本 Spec 的意义是证明架构成立，并沉淀插件开发模板。

## 前置依赖

- Spec 06 完成（通用记录表 + 通用页面可用）。

## 设计决策

- 插件结构模板（每个记录表插件）：

```text
plugins/{key}/
├── spec.go          # PluginSpec：Key/名称/Observer/Command/RecordTable/Lifecycle
├── handler.go       # 观察器/命令/后台任务（业务逻辑）
├── validate.go      # 可选自定义校验钩子
└── *_test.go
```

- 运行快照模式：`OnEnable` 从 `RecordStore` 加载全量数据构建不可变快照（`atomic.Pointer`）；消息热路径只读快照、零 SQL；管理写操作 → 平台回调 `OnRecordsChanged` → 重建快照。
- 记录表定义集中声明在 `spec.go`，列 Key 与 handler 读取的字段一致（用常量）。

## 示例插件

### 1. `keyword_reply`（观察器 + 记录表）

- 列：`keyword`(string, 必填, 搜索, 唯一)、`reply`(text, 必填)、`enabled`(boolean, 默认 true)。
- `UniqueKeys: [["keyword"]]`；`DefaultSort: "-updated_at"`。
- 行为：群消息完全匹配 `keyword` 且 `enabled=true` → 引用回复 `reply`。排除机器人自己发的消息（防自循环）。
- 快照：`map[groupID]map[keyword]reply`。

### 2. `faq`（命令 + 记录表）

- 列：`question`(string, 必填, 搜索, 唯一)、`answer`(text, 必填)。
- `UniqueKeys: [["question"]]`。
- 命令：`查 <question>` → 精确/模糊命中返回 answer；`faq 列表` → 返回前 5 条；身份 `group_member`。
- 快照：`map[groupID][]FAQItem`（供命令热路径查询）。

### 3. `group_ban_words`（观察器 + 记录表）

- 列：`word`(string, 必填, 搜索, 唯一)、`level`(enum: warn/delete/kick, 默认 warn)。
- `UniqueKeys: [["word"]]`。
- 行为：群消息包含违禁词 → 按 level 动作（警告/撤回/踢出，踢出需权限校验并审计）。含违禁词的记录管理全部走通用页。
- 快照：`map[groupID]map[word]level`。

### 4. `group_welcome`（标量配置示例）

- `ConfigSchema`：`welcome_text`(multiline)、`enabled`(boolean)、`trigger`(enum: join/any)。
- 行为：观察器监听入群通知 → 按配置发送欢迎语。
- WebUI 用通用配置页，无记录表、无专属页。

## 实现任务（按序）

1. 按上述模板实现四个插件并注册进 Catalog；`plugins/{key}/spec.go` 声明 RecordTable/ConfigSchema。
2. `cmd/bot` 装配：给每个插件注入 `RecordStore`（按 plugin_key 路由到通用 repository）、`Messenger`、`RuntimeAuthorizer`。
3. 补齐平台装配细节：`RecordTable` 的注册（启动时收集 → 校验 → 挂到通用 API/页面）。
4. 每个插件写测试（见下）。

## 测试

- 快照一致性：OnEnable 加载正确；写操作后 OnRecordsChanged 刷新；刷新失败保留旧快照。
- keyword_reply：命中回复、未启用不回复、机器人自消息不触发、跨群隔离。
- faq：精确/模糊查询、列表、未命中提示、越权（群外身份）拒绝。
- group_ban_words：三个 level 动作；踢出动作的权限与审计。
- group_welcome：配置变更即时生效。
- 每个插件至少覆盖正常/边界/错误路径。

## 验收清单

- [ ] `task lint`、`task test`（含 race）、`task web-build` 全绿。
- [ ] WebUI：四个插件均出现在侧栏；keyword_reply/faq/group_ban_words 点击进入**同一个**通用记录表页（群选择 + 表格 + 编辑 + CSV），group_welcome 进入通用配置页。
- [ ] 手测：群里启用插件 → 添加记录（WebUI）→ 立即生效（快照刷新）。
- [ ] CSV 导入违禁词表批量生效。
- [ ] 提交：`feat(plugins): 🎯 记录表插件模板与四个示例`。

## 边界与风险

- 本 Spec 刻意**不写任何插件专属 API/页面**；若有插件"忍不住"要专属能力，评估是否应归入 Spec 08 的专属路径。
- 踢出成员等外部副作用不得与数据库事务构成假原子；先落库再执行副作用，失败记日志/重试。
- 新插件的增量成本验收标准：一个纯记录表插件从零到完成 ≤ 半天，且不含任何前端代码。
