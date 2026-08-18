<!-- 📌 影响范围：定义平台 WebUI 骨架：登录、布局、插件运行页、标量配置页、公共组件与前端注册表；不含通用记录表页面（Spec 06）。 -->
# Spec 05：平台 WebUI 骨架

## 目标

建立 Vue 3 + TypeScript 管理界面的平台骨架：登录、路由守卫、侧栏布局、插件列表与全局/群开关页、标量配置页、全局 Toast 与公共组件、前端插件注册表。本 Spec 结束时，管理员可以在网页上完成"登录 → 查看插件 → 启停 → 配置标量"全流程。

## 前置依赖

- Spec 04（认证 API、插件状态 API、配置 API）。
- 技术栈：Vue 3、TypeScript、Vue Router、Vite、Naive UI、原生 Fetch（统一 `{code,message,data}` 响应）。

## 设计决策

- 前端插件注册表（编译期固定映射，不接受后端返回的组件路径/URL/脚本）：

```ts
// web/src/plugins/registry.ts
export type PluginPageKind = 'record-table' | 'config-only' | 'custom'
export interface PluginEntry {
  kind: PluginPageKind
  component?: () => Promise<unknown> // 仅 custom
}
export const registry: Record<string, PluginEntry> = {
  echo:           { kind: 'config-only' },
  keyword_reply:  { kind: 'record-table' },  // Spec 06 后生效
  // ...
}
```

- 导航数据：`GET /api/plugins` 返回每个插件的 Key/名称/描述/页面类型；侧栏按此渲染；点击进入"运行状态页"或"记录表页"或"专属页"。
- 页面结构：
  - `LoginView`：登录表单 → 存 token → 跳转。
  - `PluginRuntimesView`：插件列表（状态标签：禁用/启用中/就绪/停用中/失败 + 最近错误）、全局开关、每群开关（群选择器）、启停操作（确认框 + Toast）。
  - `PluginRuntimeConfigView`：按 Schema 渲染的通用配置表单（string/multiline/integer/boolean/enum/secret）。
  - 空状态、加载态、错误态、窄屏适配、重复提交防抖。
- 反馈：成功/失败/警告统一走应用级全局 Toast；字段级校验留在表单项内，不重复弹 Toast。
- 时间：后端返回含时区（RFC3339），前端按浏览器时区展示。
- 公共组件沉淀：`DataTable`、`Pagination`、`ConfirmDialog`、`GroupSelect`、`StatusTag`、`ToastProvider`。**先服务真实页面，出现 2+ 重复点后再抽，不预建。**

## 实现任务（按序）

1. Vite + Vue3 + TS + Naive UI 初始化；路由（login / runtimes / config / audit）；登录守卫（无 token 跳登录）。
2. `api.ts`：统一 `apiRequest`（自动带 token、401 跳登录、错误映射）；`auth.ts`、`plugins.ts`、`config.ts` API client。
3. `LoginView`、`PluginRuntimesView`、`PluginRuntimeConfigView`、`SettingsView`（改密码提示：密码来自环境变量，仅展示说明）、`AuditLogsView`（分页只读，复用 Spec 04 审计表 API——本 Spec 补一个只读审计查询 API）。
4. `feedback.ts`（全局 Toast）、`session.ts`（token 存取）、公共组件。
5. `web/src/plugins/registry.ts` 与路由表挂载（record-table 页面在 Spec 06 实现，先留占位）。

## 测试

- 类型检查 `task web-build` 通过。
- 手测流程：登录 → 列表显示插件与状态 → 启用/停用（Toast + 状态刷新）→ 配置修改保存 → 审计页可见。
- e2e（Playwright 可选，至少覆盖登录与启停主流程）。

## 验收清单

- [ ] `task lint`、`task test`、`task web-build` 全绿。
- [ ] 未登录访问任意页跳登录；登录后正常浏览。
- [ ] 启停、配置修改均有 Toast 反馈且状态即时刷新；错误（如 409 冲突）有明确提示。
- [ ] 手机窄屏下布局不破。
- [ ] 提交：`feat(webui): 🖥️ 平台 WebUI 骨架`。

## 边界与风险

- 本 Spec 不实现通用记录表页面（Spec 06）与专属页面（Spec 08）。
- 组件不得自行创建局部 Toast 或重复挂载 Provider。
- 前端不得内联任何密钥；密码仅来自后端环境变量。
