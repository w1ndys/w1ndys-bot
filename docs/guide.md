好的，我将我们讨论的所有内容整合成一份**《NapCat 机器人框架技术设计交付文档》**。这份文档可以直接交给开发团队进行评审和开发。

---

# NapCat 机器人框架技术设计交付文档

## 1. 项目概述

本项目旨在构建一个基于 **NapCat (OneBot 11 协议)** 的 QQ 机器人框架。框架具备**插件化架构**、**WebUI 可视化管理后台**、**高并发消息处理**能力，并支持运行时通过管理界面动态开关功能。

## 2. 整体架构设计

### 2.1 网络拓扑与通信协议
- **通信协议**：采用 **反向 WebSocket（Reverse WebSocket）**。NapCat 作为客户端主动连接 Go 后端，Go 后端作为服务端监听。
- **网络策略**：
  - **Go 后端**：部署在**云服务器**，绑定 `0.0.0.0` 提供公网/内网服务。
  - **NapCat 物理机**：通过 **Tailscale 内网 IP** 连接云服务器 Go 后端，**严禁**将 NapCat 端口暴露至公网。
- **优势**：零公网端口暴露、天然支持多账号连接池、网络穿透性强。

### 2.2 技术栈选型

| 层级 | 技术选型 | 理由 |
| :--- | :--- | :--- |
| **后端语言** | **Go (1.21+)** | 高并发 goroutine 完美适配长连接，编译后单二进制部署，内存稳定。 |
| **后端框架** | Gin + Gorilla/WebSocket | Gin 提供 RESTful API，Gorilla 处理 WS 长连接。 |
| **数据库** | **PostgreSQL 15+** | 支持 JSONB（灵活存插件配置）、窗口函数（跨群排名查询）、部分索引。 |
| **前端框架** | Vue 3 + TypeScript | 响应式 UI，组件化开发。 |
| **前端组件库** | Naive UI 或 Ant Design Vue | 成熟的后台管理模板支持。 |
| **部署方式** | **Docker Compose** | 编排 Go 后端与 PostgreSQL，环境一致性高。 |
| **配置管理** | Viper (环境变量) + 数据库 | 基建配置走环境变量，业务配置走数据库。 |

---

## 3. 核心设计原则（重点）

### 3.1 配置分层原则（运维与业务解耦）
必须严格遵守以下配置归属，严禁混淆：

| 配置类别 | 存放位置 | 负责内容 | 修改方式 |
| :--- | :--- | :--- | :--- |
| **基建/环境层** | `.env` 文件 + `docker-compose.yml` 硬编码 | 数据库连接（Host/Port/User/Pass）、NapCat Token、WS 监听端口、JWT 密钥、日志级别（Log Level） | 运维人员修改，需重启容器 |
| **业务/功能层** | PostgreSQL 数据库表 (`plugin_config`, `global_biz_config`) | 插件启用/禁用、签到积分值、抽奖 CD 时间、群管白名单、排行榜展示数量 | 超级管理员通过 **WebUI** 修改，**热生效，无需重启** |

### 3.2 数据存储原则（轻量高性能）
- **群消息默认不落库**：NapCat 推送的群聊消息仅在 Go 内存中完成路由和逻辑处理，不写入 PostgreSQL。这能极大降低数据库 IO 压力。
- **仅功能必要时才写入**：只有当业务逻辑需要记录状态时（如签到积分增加、抽奖记录、管理员操作日志），才进行数据库读写。

---

## 4. 插件系统设计（核心）

### 4.1 架构模式
采用 **“编译时集成 + 数据库驱动开关”** 方案（业界 Go 主流方案，避开 Go plugin 的 CGO 陷阱）。

- **编译时**：所有插件代码位于 `plugins/` 目录，通过 `init()` 函数向全局 `PluginManager` 注册。
- **运行时**：`PluginManager` 启动时从数据库加载 `enabled` 状态到内存。处理消息时，仅执行状态为 `true` 的插件。

### 4.2 插件接口定义（Interface）
所有插件必须严格实现以下接口：

```go
type Plugin interface {
    // 插件唯一标识，如 "sign_in"
    Name() string  
    // 获取启用状态（由 Manager 注入，通常从 DB 读取）
    IsEnabled() bool  
    // 核心消息处理逻辑，返回 error 或标记是否拦截后续插件
    Handle(ctx *MessageContext) error  
    // 启用回调（如加载缓存）
    OnEnable() error  
    // 禁用回调（如清理缓存）
    OnDisable() error  
}
```

### 4.3 消息路由机制（Middleware 风格）
1. NapCat 推送消息 -> Go WS 接收。
2. 解析为 `MessageContext`（包含 GroupID, UserID, RawMessage）。
3. `PluginManager` 遍历已启用的插件列表（按 `priority` 排序）。
4. 执行 `plugin.Handle(ctx)`。
5. 若返回特定错误（如 `ErrStopPropagation`），终止后续插件执行。

---

## 5. 配置与环境变量规范

### 5.1 环境变量列表（Go 后端读取）
仅以下变量由系统环境提供，其余一律走数据库：

| 变量名 | 说明 | 示例值 | 来源 |
| :--- | :--- | :--- | :--- |
| `DB_HOST` | PostgreSQL 主机地址 | `postgres` (compose内部) | compose 硬编码 |
| `DB_PORT` | PostgreSQL 端口 | `5432` | compose 硬编码 |
| `DB_USER` | 数据库用户 | `bot_admin` | compose 硬编码 |
| `DB_NAME` | 数据库名称 | `w1ndys_bot` | compose 硬编码 |
| `DB_PASSWORD` | 数据库密码 | `xxxx` | **`.env` 文件** |
| `DB_SSLMODE` | PostgreSQL TLS 模式 | `disable` (compose内部) | compose 硬编码 |
| `NAPCAT_TOKEN` | WS 连接鉴权 Token | `xxxx` | **`.env` 文件** |
| `WS_PORT` | 反向 WebSocket 监听端口 | `18800` | compose 硬编码 |
| `JWT_SECRET` | WebUI 登录 JWT 密钥 | `xxxx` | **`.env` 文件** |
| `LOG_LEVEL` | 日志级别 | `info` / `debug` | compose 硬编码 |

### 5.2 CLI 参数覆盖
支持启动参数覆盖环境变量（如 `./bot --db-host=127.0.0.1`），用于本地调试，优先级：**CLI 参数 > 环境变量**。

---

## 6. 数据库设计（PostgreSQL）

### 6.1 核心表结构

**表 1：`plugin_config`（插件配置与开关）**
```sql
CREATE TABLE plugin_config (
    plugin_name VARCHAR(64) PRIMARY KEY,
    enabled BOOLEAN DEFAULT false,
    config_json JSONB NOT NULL DEFAULT '{}',  -- 存 {"reward_score": 10, "cd_seconds": 60}
    priority INT DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_plugin_enabled ON plugin_config(enabled);
```

**表 2：`user_scores`（用户积分明细 - 支持跨群排名）**
```sql
CREATE TABLE user_scores (
    id BIGSERIAL PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL,
    group_id VARCHAR(32) NOT NULL,
    score INT DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, group_id)
);
CREATE INDEX idx_user_score ON user_scores(user_id, score DESC);
```

**表 3：`user_profile`（用户信息与总积分冗余 - 优化排名查询）**
```sql
CREATE TABLE user_profile (
    user_id VARCHAR(32) PRIMARY KEY,
    nickname VARCHAR(64),
    total_score INT DEFAULT 0,  -- 冗余字段，避免每次 SUM
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_total_score ON user_profile(total_score DESC);
```

**表 4：`global_biz_config`（全局业务配置）**
```sql
CREATE TABLE global_biz_config (
    config_key VARCHAR(64) PRIMARY KEY,
    config_value JSONB NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
-- 示例数据：('rank_limit', '{"top_n": 50}')
```

### 6.2 积分更新事务逻辑（保证一致性）
1. Go 处理签到命令。
2. 开启数据库事务。
3. `INSERT INTO user_scores ... ON CONFLICT DO UPDATE SET score = score + 1`。
4. `UPDATE user_profile SET total_score = total_score + 1 WHERE user_id = xxx`。
5. 提交事务。

---

## 7. 项目目录结构

```text
your-bot/
├── cmd/
│   └── bot/
│       └── main.go                 # 入口：初始化配置、DB、PluginManager、启动WS与HTTP
├── internal/
│   ├── config/                     # Viper 加载环境变量
│   ├── db/                         # PostgreSQL 连接池 (pgx)
│   ├── plugin/                     # 插件核心框架
│   │   ├── manager.go              # PluginManager (注册、状态刷新、路由)
│   │   └── interface.go            # Plugin 接口定义
│   ├── ws/                         # NapCat 反向 WebSocket 服务端
│   │   ├── server.go               # 启动 WS 监听，解析消息
│   │   └── dispatcher.go           # 将消息分发至 PluginManager
│   └── webui/                      # WebUI 后端 API (Gin)
│       ├── router.go               # 路由注册
│       ├── handler_admin.go        # 插件开关、全局配置接口
│       └── handler_rank.go         # 积分排名查询接口
├── plugins/                        # 【插件存放目录】
│   ├── sign_in/                    # 签到积分插件
│   │   └── plugin.go               # 实现 Plugin 接口，注册到 init()
│   ├── group_manager/              # 群管理插件
│   │   └── plugin.go
│   ├── entertainment/              # 娱乐功能插件
│   │   └── plugin.go
│   └── health/                     # 健康检测插件
│       └── plugin.go
├── pkg/                            # 公共工具
│   ├── logger/                     # Zap 日志封装
│   └── utils/                      # 工具函数
├── web/                            # Vue 前端源码
│   ├── src/
│   │   ├── views/                  # 插件管理、排名、设置页面
│   │   └── api/                    # 后端 HTTP 请求封装
│   └── package.json
├── docker-compose.yml              # 编排 Go + PostgreSQL
├── Dockerfile                      # 多阶段构建
├── .env                            # 存放敏感信息 (不提交 Git)
├── .gitignore
└── go.mod
```

---

## 8. WebUI 功能需求（核心页面）

1. **仪表盘**：展示在线状态、今日消息处理量、插件总数。
2. **插件管理页**：
   - 列表展示所有注册插件（Name, Priority, Status）。
   - 开关切换（Enabled/Disabled）-> 调用后端 API，**即时生效**。
   - 配置编辑（针对 `config_json` 提供 JSON 编辑器或动态表单）。
3. **积分排行榜页**：
   - 展示全局总积分 Top N（调用 `/api/rank/global`）。
   - 支持按 QQ 号查询其各群积分明细。
4. **全局设置页**：
   - 修改全局业务参数（如排行榜展示人数）。

---

## 9. Docker 部署规范

### 9.1 `docker-compose.yml` 关键配置
- Go 容器与 PostgreSQL 容器必须在同一个自定义网络（`bridge`）内。
- PostgreSQL **不得**将 `5432` 端口映射到宿主机公网，仅允许内网访问（或仅映射到 `127.0.0.1:5432`）。
- Go 容器需映射 `8080`（WebUI）和 `18800`（WS）端口。

### 9.2 新增插件开发与上线流程（CI/CD）
1. 开发者在 `plugins/` 下新建包，实现 `Plugin` 接口。
2. 在 `main.go` 中匿名导入该包。
3. 提交代码 -> CI 构建新镜像并推送到仓库。
4. 运维执行 `docker-compose pull && docker-compose up -d` 重启。
5. 新插件自动注册至管理器，但初始 `enabled=false`（因为数据库中尚无记录）。
6. 管理员登录 WebUI，点击“启用”按钮，插件即刻处理消息。

---

## 10. 开发环境配置提示

- **本地调试**：可在本地启动 PostgreSQL（Docker 或宿主机），Go 程序通过 CLI 参数 `--db-host=127.0.0.1` 连接，NapCat 通过 Tailscale 或本地配置指向本机 `127.0.0.1:18800`。
- **日志**：开发阶段建议设置 `LOG_LEVEL=debug` 查看详细消息路由日志。

---

## 11. 交付检查清单（Checklist）

开发人员开始编码前，请确认以下模块分工：

- [ ] **基础框架**：Viper 加载环境变量 + pgx 连接池初始化。
- [ ] **WS 服务端**：实现反向 WebSocket 接收与 JSON 解析（需包含 Token 鉴权）。
- [ ] **PluginManager**：实现插件注册、DB 状态加载、消息路由循环。
- [ ] **示例插件**：开发一个 `ping` 插件验证链路通畅。
- [ ] **WebUI 后端**：实现 `/api/admin/plugins` CRUD 和 `/api/rank/global` 接口。
- [ ] **WebUI 前端**：完成插件管理页面和排行榜页面。
- [ ] **Dockerfile**：多阶段构建，最终镜像尽量精简（Alpine）。
- [ ] **数据库 Migration**：启动时自动执行 DDL 建表（使用 `golang-migrate` 或 GORM AutoMigrate）。

---

这份文档已覆盖我们讨论的所有核心决策点。如果在开发过程中遇到具体的技术实现细节（如 WS 连接池的具体代码、PluginManager 的并发安全锁设计等），随时可以展开细化。祝开发顺利！🚀
