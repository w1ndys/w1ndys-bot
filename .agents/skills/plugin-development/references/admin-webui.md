<!-- 📌 影响范围：规定插件专属管理 API、Vue 页面和平台公共管理能力；无外部变量。 -->
# Plugin Admin and WebUI

## Platform responsibilities

Centralize login, administrator and group-operation authorization, trusted group context, request IDs, strict JSON, payload/page limits, error mapping, audit orchestration, timeouts, panic isolation, global Toast, navigation, and common UI primitives.

## Plugin responsibilities

For complex business data, expose semantic endpoints and implement a dedicated compiled Vue page. Keep API DTOs explicit and operations named by domain intent, such as `confirm violation`, instead of disguising side effects as generic field updates.

Use plugin-owned repositories and services. Every group route must authorize the actor for the path group and pass that trusted group to storage. Project output through explicit DTOs so internal and sensitive fields cannot leak.

## UI selection

- Common page: status, global/group gates, runtime errors, simple config.
- Dedicated page: record lists, review flows, bulk operations, history, trials, charts, or multi-step work.
- Shared components: tables, pagination, filters, dialogs, confirmations, status tags, group selectors, responsive layout.

Register dedicated pages in a compile-time frontend map. Do not accept a component name, module path, URL, HTML, script, or expression from the server.

Keep safe configuration and history accessible while disabled. Recheck runtime gates in server endpoints that invoke live engines or group effects. Show loading, empty, error, conflict, disabled, and narrow-screen states; all operation results use the application-level Toast.
