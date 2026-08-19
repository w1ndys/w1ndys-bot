<!-- 📌 影响范围：定义复杂插件的专属路径（自有表/专属 API/专属页面）与 QQ 应急命令入口；明确通用记录表的逃逸口。 -->
# Spec 08：专属复杂插件与 QQ 应急入口

## 目标

1. 定义复杂插件的专属路径：自有业务表 + 迁移 + Repository + Service + 专属 API + 专属 Vue 页（注册到前端注册表），并以一个示例插件（邀请树）走通全流程。
2. 实现 QQ 应急命令入口：插件列表/状态/全局启停/当前群启停，复用与 WebUI 相同的 Service 与审计，不维护第二套开关逻辑。

## 前置依赖

- Spec 03-07 全部完成（运行时、认证审计、WebUI、通用记录表、示例插件）。

## 设计决策

### 专属路径判定（什么时候不用通用记录表）

- 记录之间有**强关联/跨表 join**；
- 需要**外部副作用编排**（抓取、AI 调用、审批流）且不能伪装成字段更新；
- 需要**聚合统计/报表**（如邀请树层级）；
- 需要**工作流/多步状态机**（如人工审核）。

以上任一成立 → 走专属路径；否则一律用通用记录表。**先评估再用通用表，评估表写在设计卡里。**

### 专属插件结构

```text
plugins/{key}/
├── spec.go / handler.go / lifecycle.go
├── repository.go        # 固定 SQL，群隔离，乐观锁
├── service.go           # 领域校验、事务、外部副作用编排
├── admin.go             # 专属 HTTP handler（挂在 internal/webapi）
└── *_test.go

web/src/plugins/{key}/
├── Page.vue             # 专属页面（复用平台公共组件）
├── api.ts / types.ts
└── components/
```

- 前端注册：`registry[key] = { kind: 'custom', component: () => import(...) }`；后端只返回稳定页面 Key，不返回组件路径/URL/HTML/脚本。
- 专属 API 责任边界：平台管认证/请求 ID/超时/panic 隔离/错误映射；插件管领域校验/事务/快照刷新/外部副作用。所有写操作：授权 + 严格输入 + 越群隔离 + 乐观锁 + 审计 + 错误路径测试。
- 插件关闭时的管理操作：离线 CRUD/历史读取允许；涉及 OneBot/外部网络/群副作用必须先重查全局与群门禁。

### 示例插件：`invite_tree`（邀请树记录）

- 自有表：`invite_records(id, group_id, inviter_qq, invitee_qq, invited_at, verified bool)`，群隔离 + 唯一约束 `(group_id, invitee_qq)`。
- 观察器：监听群成员增加 → 记录邀请人（入群前最后活跃成员需配置/推算，简化为"邀请人字段由管理员事后维护"或读取群公告记录，MVP 允许管理员在 WebUI 手动登记）。
- 专属 API：分页列表、登记邀请、按邀请人聚合统计（树状或排行）、导出。
- 专属 Vue 页：表格 + 排行卡片 + 登记弹窗（复用公共组件）。

### QQ 应急入口

- 平台管理服务：`list / status / enable <key> / disable <key> / enable-group <key> <gid> / disable-group ...`。
- 复用 `RuntimeController`（同一乐观锁、同一审计、同一状态机）；QQ 命令的授权：`SUPER_ADMIN_QQ` 或群主/群管（仅限当前群操作）。
- 微信同款红线：复杂配置与业务数据只在 WebUI 管理；QQ 只做启停与状态。
- 与 WebUI 页面复用同一 `GET /api/plugins` 数据源。

## 实现任务（按序）

1. 迁移 `000006_invite_records`（配对 up/down）+ Repository + Service + 专属 API + 测试。
2. `invite_tree` 插件：spec/observer/handler/专属页面 + 注册。
3. QQ 应急命令：`plugins/admin` 插件（命令：`bot list/status/enable/disable`、`bot group enable/disable`），复用 RuntimeController；身份授权 + 审计。
4. 平台装配：把 admin 插件与应急命令挂到 Dispatcher（注意：平台命令走平台服务，不经普通插件门禁，但必须做身份校验）。
5. 文档：在 `docs/` 记录"通用记录表 vs 专属路径"判定表（沉淀到开发指南）。

## 测试

- invite_records：越群隔离、唯一冲突、乐观锁、审计、聚合统计正确、外部副作用失败处理。
- QQ 应急：仅授权身份可用；启停与 WebUI 一致（同一状态机，WebUI 能看到 QQ 端触发的变更）；审计完整。
- 专属页面：`task web-build` 通过；手测登记/排行/导出。

## 验收清单

- [ ] `task lint`、`task test`（含 race）、`task web-build` 全绿。
- [ ] WebUI 与 QQ 命令都能启停插件，状态双向一致且都写审计。
- [ ] invite_tree 走通：观察器记录 → WebUI 查看/登记 → 排行。
- [ ] 提交：`feat(plugins): 🌳 专属插件路径与 QQ 应急入口`。

## 边界与风险

- 专属路径是"逃逸口"不是"默认路径"：新插件必须先在设计卡证明通用记录表不适用。
- QQ 应急命令不得演化成完整管理面；业务 CRUD 只在 WebUI。
- 外部副作用（调用 OneBot/网络）与 DB 事务分离：先落库、后副作用、失败重试/告警，禁止假原子。
