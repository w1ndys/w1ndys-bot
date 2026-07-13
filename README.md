# w1ndys-bot

基于 Go、NapCat OneBot 11 和 PostgreSQL 的 QQ 机器人框架，内置插件生命周期、命令路由、多级权限、审计日志与 Web 管理界面。

> 项目仍在积极开发中。后端管理 API 与 Vue WebUI 已可用于开发，生产镜像会一并构建并托管 WebUI 静态资源。

## 功能特性

- NapCat OneBot 11 反向 WebSocket 接入与 Token 鉴权
- 带超时和 `echo` 关联的类型化 OneBot Action Client
- 基于 `Manifest + Factory` 的编译时插件注册与运行时启停
- 全局/群级命令、角色/指定用户权限策略及热更新
- 插件、触发词、权限、系统设置和审计日志管理 API
- Vue 3 + TypeScript + Naive UI 管理界面，采用亮色曲奇棕主题
- PostgreSQL 自动迁移、Docker Compose 编排和结构化日志

## 工作原理

```text
NapCat 事件 → WebSocket → 命令匹配 → 权限解析
             → PluginManager → 插件 → OneBot API → NapCat
```

插件能力由代码中的 Manifest 定义，运行状态、命令、权限和系统设置保存在 PostgreSQL。启动时程序自动应用数据库迁移、同步插件元数据并发布运行时快照。

## 环境要求

- Go 1.26
- [Task](https://taskfile.dev/) v3
- Node.js 20.19+ 或 22.12+ 与 npm（开发 WebUI 时）
- PostgreSQL 17
- Docker 与 Docker Compose（推荐部署方式）
- 已启用 OneBot 11 的 NapCat 实例

## 快速开始

1. 克隆仓库并创建 `.env`：

   ```dotenv
   DB_PASSWORD=请替换为数据库强密码
   NAPCAT_TOKEN=请替换为OneBot访问令牌
   JWT_SECRET=请替换为至少32字节的随机密钥
   SUPER_ADMIN_QQ=请填写最高管理员QQ号
   WEBUI_PASSWORD=请替换为至少12个字符的密码
   LOG_LEVEL=info
   LOG_FORMAT=text
   ```

2. 构建并启动机器人与 PostgreSQL：

   ```bash
   task compose-up
   task compose-logs
   ```

   启动后访问 `http://localhost:18800/` 打开 WebUI；远程部署时将 `localhost` 换成机器人主机地址。

3. 在 NapCat 中配置 OneBot 11 反向 WebSocket：

   ```text
   ws://<机器人主机>:18800/onebot/v11/ws
   ```

   Access Token 必须与 `NAPCAT_TOKEN` 一致。数据库迁移会在机器人启动时自动执行。

4. 开发 WebUI 时，在宿主机另开终端：

   ```bash
   task setup
   task web-dev
   ```

   然后访问 `http://localhost:5173`。Vite 会把 `/api` 请求代理到 `http://127.0.0.1:18800`。

停止容器：

```bash
task compose-down
```

## 本地开发

常用命令均从仓库根目录执行：

| 命令 | 用途 |
| --- | --- |
| `task setup` | 同步 Go 与 WebUI 依赖 |
| `task run` | 构建 WebUI 并启动本地机器人服务 |
| `task web-dev` | 启动 Vite 开发服务器 |
| `task test` | 运行全部 Go 测试 |
| `task lint` | 检查 gofmt、go vet 和前端类型 |
| `task web-build` | 类型检查并构建 WebUI |
| `task migrate-version` | 查看数据库迁移版本 |
| `task migrate-up` | 应用所有待执行迁移 |
| `task migrate-down` | 回滚最近一版迁移 |

提交前请运行：

```bash
task lint
task test
git diff --check
```

## 项目结构

```text
cmd/                         服务与迁移工具入口
internal/admin/              管理服务、设置与审计
internal/command/            命令注册和匹配
internal/migration/          迁移执行器与版本化 SQL
internal/onebot/             类型化 OneBot API
internal/permission/         多级权限解析
internal/plugin/             插件定义、同步与运行管理
internal/webapi/             WebUI 认证与管理 API
internal/ws/                 反向 WebSocket 和 Action Client
pkg/logger/                  zap 日志封装
plugins/                     内置插件
web/                         Vue 3 管理界面
docs/                        架构与开发文档
```

更详细的消息路由、权限优先级、数据库模型及开发计划见 [开发指南](docs/guide.md)。贡献前请阅读 [Repository Guidelines](AGENTS.md)。

## 配置与安全

配置可通过环境变量或同名 CLI 参数提供，CLI 参数优先。常用可选项包括 `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_NAME`、`DB_SSLMODE`、`WS_PORT`、`WS_BIND_ADDRESS`、`LOG_LEVEL` 和 `LOG_FORMAT`。

不要提交 `.env`、Token、密码或日志。`LOG_LEVEL=debug` 会记录原始 OneBot 事件，其中可能包含聊天内容、QQ 标识、群信息和 URL，生产环境不应长期启用。数据库迁移一旦部署不得修改，应新增成对的 `up.sql` 与 `down.sql` 文件。

## 贡献

功能和修复应包含正常、边界与错误路径测试。提交信息使用中文 Conventional Commits，并同时包含 emoji 和颜文字：

```text
feat(插件管理): 🔌 (｡•̀ᴗ-)✧ 新增插件优先级调整
```

Pull Request 请说明目的、主要改动、验证命令并关联 issue；界面改动附截图，配置或兼容性变化说明迁移步骤。

## License

本项目基于 [Apache License 2.0](LICENSE) 开源。
