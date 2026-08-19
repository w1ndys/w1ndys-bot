<!-- 📌 影响范围：用通用记录表能力实现最小插件示例，验证"零 WebUI/API 成本"路径，并沉淀后续插件迁移模板；原 Py 版插件暂不迁移。 -->
# Spec 07：最小插件示例与迁移模板

## 目标

- 运行时链路已由 Spec 03 的 `echo` 插件验证（命令 → 门禁 → 身份 → Handler），本 Spec 不再重复。
- 本 Spec 用**一个**最小记录表插件 `keyword_reply`（最典型的"记录表 + 观察器 + 快照"形态）验证通用记录表的零成本路径：零专属 API、零专属 Vue 页。
- **原 Py 版插件（FAQ、违禁词、签到等）本阶段暂不迁移**，待框架稳定后按需求逐个迁移（记录表形态用通用记录表，复杂形态走 Spec 08 专属路径）。

## 前置依赖

- Spec 03（echo 最小链路）、Spec 06（通用记录表 + 通用页面）。

## 设计决策

- 插件结构模板（记录表插件通用）：

```text
plugins/{key}/
├── spec.go          # PluginSpec：Key/名称/Observer/Command/RecordTable/Lifecycle
├── handler.go       # 观察器/命令/后台任务（业务逻辑）
├── validate.go      # 可选自定义校验钩子
└── *_test.go
```

- 运行快照模式：`OnEnable` 从 `RecordStore` 加载全量数据构建不可变快照（`atomic.Pointer`）；消息热路径只读快照、零 SQL；管理写操作 → 平台回调 `OnRecordsChanged` → 重建快照。
- 记录表定义集中声明在 `spec.go`，列 Key 与 handler 读取的字段一致（用常量）。

## 示例插件：`keyword_reply`（观察器 + 记录表）

- 列：`keyword`(string, 必填, 搜索, 唯一)、`reply`(text, 必填)、`enabled`(boolean, 默认 true)。
- `UniqueKeys: [["keyword"]]`；`DefaultSort: "-updated_at"`。
- 行为：群消息完全匹配 `keyword` 且 `enabled=true` → 引用回复 `reply`。排除机器人自己发的消息（防自循环）。
- 快照：`map[groupID]map[keyword]reply`。

## 实现任务（按序）

1. 按上述模板实现 `keyword_reply` 并注册进 Catalog；`spec.go` 声明 RecordTable。
2. `cmd/bot` 装配：给插件注入 `RecordStore`（按 plugin_key 路由到通用 repository）、`Messenger`、`RuntimeAuthorizer`。
3. 补齐平台装配细节：`RecordTable` 的注册（启动时收集 → 校验 → 挂到通用 API/页面）。
4. 写测试（见下）。

## 测试

- 快照一致性：OnEnable 加载正确；写操作后 OnRecordsChanged 刷新；刷新失败保留旧快照。
- keyword_reply：命中回复、未启用不回复、机器人自消息不触发、跨群隔离。
- 至少覆盖正常/边界/错误路径。

## 验收清单

- [ ] `task lint`、`task test`（含 race）、`task web-build` 全绿。
- [ ] WebUI：keyword_reply 出现在侧栏，点击进入**同一个**通用记录表页（群选择 + 表格 + 编辑 + CSV），无任何插件专属前端代码。
- [ ] 手测：群里启用插件 → WebUI 添加记录 → 立即生效（快照刷新）。
- [ ] CSV 导入关键词批量生效。
- [ ] 提交：`feat(plugins): 🎯 最小记录表插件与迁移模板`。

## 边界与风险

- 本 Spec 刻意**不写任何插件专属 API/页面**；若插件"忍不住"要专属能力，评估是否应归入 Spec 08 的专属路径。
- 原插件迁移另行排期：迁移时以本模板为准，先写设计卡分类（记录表/标量配置/专属），再按对应路径实现。
