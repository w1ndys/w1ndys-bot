<!-- 📌 影响范围：定义管理员认证、Actor 模型、审计写入与标量配置 Schema；WebUI 登录与平台 API 的前置。 -->
# Spec 04：数据层与管理面（认证/审计/配置）

## 目标

补齐管理面基础设施：管理员认证（JWT）、`Actor` 模型、审计日志写入、标量配置 Schema 与通用配置读写 API。本 Spec 结束时，管理 API 具备"登录 → 鉴权 → 操作 → 审计"的完整闭环（WebUI 界面在 Spec 05）。

## 前置依赖

- Spec 01（DB）、Spec 03（状态表、生命周期、审计接口打点）。

## 设计决策

- 单管理员：`WEBUI_PASSWORD`（环境变量）为登录凭据；登录成功签发 JWT（`JWT_SECRET` 签名，过期时间如 24h）。无注册、无多账号、无角色矩阵。
- `Actor`：每个管理操作携带 `{QQ, Role}`（超级管理员或来自 WebUI 登录的 admin）。所有写操作必须 `authorizer.Authorize(actor)`。
- 审计：一张表记录所有管理写操作（含通用记录表与专属 API）：

```sql
admin_audit_logs(
  id bigserial PK,
  actor_qq text, actor_role text,
  action text,          -- enable_plugin / create_record / update_config ...
  resource_type text,   -- plugin / group / record_table / config
  resource_id text,     -- 稳定标识
  group_id bigint,
  before jsonb, after jsonb,  -- 有界前后快照
  created_at timestamptz default now()
)
```

- 审计写入与业务写入**同事务**（业务表在 Spec 06 引入后，通用记录表写操作在同一事务写审计）。
- 标量配置 Schema（窄契约，仅这些类型）：`string`、`multiline`、`integer`、`number`、`boolean`、`enum`、`secret`。**禁止**：嵌套对象、任意 JSON、SQL、表达式、组件名。业务记录、工作流一律不进配置。
- 配置存储：`plugin_runtime_configs.config_json`，读写走乐观锁，写后触发插件 `OnConfigChanged` 回调（刷新运行时行为）。
- API 错误映射：统一 `{code, message, data}`；HTTP 状态码语义化（400 校验、401 未登录、403 无权限、404 不存在、409 冲突、500 内部）。

## 实现任务（按序）

1. 迁移 `000003_admin_audit_logs`（配对 up/down，结构如上）。
2. `internal/management`：
   - `contracts.go`：`Actor`、`Authorizer`（接口）。
   - `auth.go`：密码校验（`crypto/subtle` 常数时间比较）、JWT 签发/解析/中间件。
   - `audit.go`：`AuditWriter`（写审计，支持事务内注入）。
3. `internal/webapi`：
   - `server.go`：HTTP 路由框架（Go 标准库或 chi），统一中间件（恢复 panic、请求 ID、超时）、错误映射。
   - `auth.go`：`POST /api/auth/login`（返回 JWT）、`GET /api/auth/me`。
   - `plugins.go`：`GET /api/plugins`（列出插件 Key、名称、状态、页面类型）、`GET/PUT /api/plugins/{key}/state`（全局启停）、`GET/PUT /api/plugins/{key}/groups/{gid}/state`（群开关）——全部复用 Spec 03 的 `RuntimeController` 并写审计。
   - `config.go`：`GET/PUT /api/plugins/{key}/config`（乐观锁 + 校验 Schema + 审计 + OnConfigChanged）。
4. `internal/plugin`：`config_schema.go`（Schema 定义 + 校验器：类型、必填、枚举、secret 不回显）。
5. 补 Spec 03 的审计打点：启停操作在同一事务写审计（补 `000003` 后修改 RuntimeController 的事务范围，不新增表）。

## 测试

- 认证：错误密码 401；无 token 401；过期/伪造 token 401/403。
- 授权：非管理员调用写接口 403。
- 审计：启停与配置修改后审计表出现记录，before/after 正确。
- 配置：合法配置保存成功；类型错误、枚举越界、未知字段拒绝；secret 字段读取时脱敏不回显；乐观锁冲突 409。
- 插件列表：返回代码声明信息（Key、名称、描述、页面类型）与运行状态。

## 验收清单

- [ ] `task lint`、`task test`（含 race）全绿。
- [ ] curl 全流程：login 拿 token → 带 token 列出插件 → 启用/停用某插件 → 审计表可见记录；错误路径（无 token、错密码、越权）返回正确状态码。
- [ ] 配置 Schema 校验拒绝非法输入，secret 不回显。
- [ ] 提交：`feat(admin): 🔐 认证审计与通用配置`。

## 边界与风险

- 审计 before/after 必须是有界 JSON（截断超长字段），防审计表膨胀。
- JWT_SECRET 是部署硬要求（≥32 字节），缺失禁止启动。
- 本 Spec 不涉及 WebUI 界面（Spec 05）与通用记录表（Spec 06）；API 先用 curl 验收。
