<!-- 📌 影响范围：定义 NapCat 反向 WebSocket 接入、事件模型与 OneBot Action Client；只做传输层，不做门禁与插件。 -->
# Spec 02：NapCat 接入与消息管线

## 目标

建立 NapCat 反向 WebSocket 连接（带 token 鉴权、断线自动重连）、事件解析与分发入口、OneBot Action Client（唯一 `echo` 关联、超时、并发响应）。本 Spec 结束时，程序能收到事件并在日志打印，能主动调用一次 `get_login_info` 验证回包关联。

## 前置依赖

- Spec 01 完成（配置里有 `WS_PORT`、`NAPCAT_TOKEN`）。
- 本机或 docker 有 NapCat 可连（开发期可先用测试服务端模拟，见测试节）。

## 设计决策

- 反向 WS：程序监听 `WS_PORT`，NapCat 连入；握手校验 `Authorization: Bearer <token>`，失败拒绝并记日志。
- 读取循环单线程：优先处理 Action 响应（按 `echo` 匹配），普通事件交给受限并发 worker，避免插件等待 Action 响应时阻塞收包。
- Action Client：每次调用生成唯一 `echo`（递增 + 随机），请求与响应通过 `map[echo]chan` 关联；默认超时（如 10s）与并发上限；断线时所有在途请求立即失败。
- 重连：NapCat 断开后指数退避重连（1s→2s→4s→…上限 60s），连接恢复后触发 `OnConnect` 回调（供后续生命周期用）。
- 事件模型最小化：`message`（group/private）、`notice`、`request`、`meta_event` 各一个结构体，`RawMessage` 保留原文；后续插件只消费需要的字段。
- 测试不依赖真实 NapCat：用 gorilla/websocket 起假 NapCat 服务端。

## 实现任务（按序）

1. `internal/ws`：
   - `server.go`：HTTP 升级 + token 校验 + 读写循环 + 重连（指数退避）+ 优雅关闭。
   - `event.go`：`MessageEvent`、`NoticeEvent`、`RequestEvent`、`MetaEvent` 结构体与 JSON 解析（`post_type`、`message_type`、`group_id`、`user_id`、`raw_message`、`message_id`）。
   - `action.go`：`ActionClient`——`Call(ctx, action, params) (json.RawMessage, error)`；`echo` 关联表（带锁）、超时、并发限制、断线失败注入。
2. `internal/onebot`：类型化动作包，先实现 `GetLoginInfo`、`SendGroupMessage`、`SendPrivateMessage`、`DeleteMessage`、`GetGroupMemberInfo`（后续 Spec 再按需扩充）。
3. `cmd/bot`：启动 WS 服务，连接成功后调用 `GetLoginInfo` 打日志（验证 echo 关联）；收到任意事件打 debug 日志。
4. 断线/重连日志与状态暴露（内存计数，供后续健康检查用）。

## 测试

- 假 NapCat 服务端（gorilla/websocket）：
  - 无 token / 错误 token 连接被拒。
  - 正确 token 握手成功。
  - 发事件 → 程序按类型解析正确。
  - `Call` 后服务端回包 → 关联到正确的调用方；两个并发调用回包乱序仍能正确匹配。
  - 服务端不回包 → 超时返回错误。
  - 服务端断开 → 在途调用失败、指数退避重连、重连后新调用成功。
- `internal/ws` 全部用表驱动测试覆盖正常/边界/错误路径。

## 验收清单

- [ ] `task lint`、`task test`（含 race）全绿。
- [ ] 假服务端测试全过；与真实 NapCat 连一次，日志出现 `login_info` 回包与事件。
- [ ] 断线后自动重连（日志可见退避过程），重连后调用成功。
- [ ] 提交：`feat(ws): 🔌 NapCat 反向 WS 与 Action Client`。

## 边界与风险

- 本 Spec 不做任何门禁/插件/数据；事件先只打日志。
- `echo` 关联表必须防泄漏：响应到达后即删除，超时/断线清理。
- 收包与回包处理共用一条读取循环，禁止在读取循环内做阻塞调用（调用 Action 必须在 worker 或独立 goroutine）。
