# 部署、升级与回滚

本文适用于使用 `compose.yml` 部署 `w1ndys-bot` 与 PostgreSQL 的单机环境。命令均在仓库根目录执行。

## 首次部署清单

复制 `.env.example` 为 `.env`，并配置以下变量：

| 变量 | 要求 | 说明 |
| --- | --- | --- |
| `DB_PASSWORD` | 强随机密码 | PostgreSQL `bot_admin` 密码，仅 Compose 内部使用 |
| `NAPCAT_TOKEN` | 强随机令牌 | 必须与 NapCat 反向 WebSocket Access Token 一致 |
| `JWT_SECRET` | 至少 32 字节 | WebUI 会话签名密钥，轮换后现有会话立即失效 |
| `SUPER_ADMIN_QQ` | 正整数 QQ 号 | 唯一最高管理员与 WebUI 登录账号 |
| `WEBUI_PASSWORD` | 至少 12 字符 | WebUI 环境密码，不保存到数据库 |
| `WS_BIND_ADDRESS` | 默认 `0.0.0.0` | 仅本机访问可改为 `127.0.0.1` |
| `LOG_LEVEL` | 推荐 `info` | `debug` 会记录可能含聊天内容的原始事件 |
| `LOG_FORMAT` | `text` 或 `json` | 生产日志采集推荐 `json` |

可用 `openssl rand -base64 48` 分别生成密码和密钥。不得复用不同用途的凭据，也不得提交 `.env`。

启动并检查：

```bash
task compose-up
docker compose ps
task migrate-version
docker compose logs --tail=100 bot
```

验收标准：`postgre` 为 `healthy`，`w1ndys-bot` 为 `Up`，迁移版本为最新且 `dirty=false`，日志出现“基础框架已启动”。随后验证：

- WebUI：`http://<机器人主机>:18800/`
- NapCat 反向 WebSocket：`ws://<机器人主机>:18800/onebot/v11/ws`
- NapCat Access Token 与 `NAPCAT_TOKEN` 完全一致

## 数据备份与恢复

升级或回滚数据库前先创建自包含备份：

```bash
mkdir -p backups
docker exec postgre pg_dump -U bot_admin -d w1ndys_bot -Fc > "backups/w1ndys_bot-$(date +%Y%m%d-%H%M%S).dump"
```

先校验归档可读，记录校验和，并复制到容器之外的持久位置：

```bash
docker exec -i postgre pg_restore --list < backups/<备份文件>.dump > /dev/null
sha256sum backups/<备份文件>.dump
```

恢复会覆盖现有业务数据，必须先停止机器人。恢复使用单事务并在首个错误处退出，失败时保持机器人停止：

```bash
docker compose stop bot
docker exec postgre dropdb -U bot_admin --if-exists w1ndys_bot
docker exec postgre createdb -U bot_admin w1ndys_bot
docker exec -i postgre pg_restore -U bot_admin -d w1ndys_bot --exit-on-error --single-transaction < backups/<备份文件>.dump
```

恢复成功后不要执行 `docker compose start bot`，它只会启动原有容器，并不会应用刚切换的代码或镜像。先选择下方回滚方案重建 Bot 容器，再核对迁移版本。若是同版本灾难恢复，确认当前 `w1ndys-bot:latest` 镜像与备份版本兼容后，使用 `docker compose up --force-recreate -d bot` 创建新容器。

不要通过删除 `postgres-data` 卷代替正常恢复；删除容器本身不会删除具名卷。归档校验成功不代表业务恢复一定成功，原备份文件必须保留到完整验收结束。

## 标准升级流程

1. 阅读目标版本变更记录，确认配置项和数据库兼容性。
2. 记录当前提交：`git rev-parse HEAD`。
3. 备份数据库，并给当前镜像保留回滚标签：

   ```bash
   docker image tag w1ndys-bot:latest "w1ndys-bot:rollback-$(date +%Y%m%d-%H%M%S)"
   ```

4. 拉取目标代码后运行：

   ```bash
   task lint
   task test
   task web-e2e-install # 新机器或首次运行时执行一次
   task web-e2e
   task compose-rebuild
   ```

5. 检查容器、迁移、WebUI 登录、NapCat 连接和审计日志。

程序启动时会自动向上迁移。不要在新旧机器人进程同时运行时手工回滚迁移。

## 回滚流程

若仅应用代码异常且数据库结构仍兼容，切回已记录的稳定提交并执行 `task compose-rebuild`。若目标提交要求更旧的数据库版本，必须在以下方案中二选一：

### 方案 A：恢复升级前备份（推荐）

1. 停止机器人。
2. 按“数据备份与恢复”恢复升级前归档。
3. 选择一种方式创建匹配备份版本的新容器：

   - 从代码回滚：`git checkout <稳定提交>`，然后执行 `task compose-rebuild`。
   - 从保留镜像回滚：先执行 `docker image tag <回滚镜像标签> w1ndys-bot:latest`，再执行 `docker compose up --force-recreate -d bot`，不得附加 `--build`。

4. 使用 `docker inspect w1ndys-bot --format '{{.Image}}'` 核对容器镜像，并检查迁移版本和启动日志。

恢复备份后不得再执行 `task migrate-down`；备份已经包含旧版本数据库，再 down 会额外回滚一版。

### 方案 B：原库执行 down 迁移

1. 停止机器人：`docker compose stop bot`。
2. 保留并校验当前数据库备份。
3. 确认每个 `.down.sql` 不会丢失所需数据后，使用 `task migrate-down` 逐版回滚到目标版本。
4. 切回匹配该迁移版本的稳定代码并执行 `task compose-rebuild`。
5. 核验迁移版本、插件状态、命令、权限、设置和 NapCat 连接。

`task migrate-down` 一次只回滚一版。不要在 down 完成前启动新版本机器人，否则自动向上迁移会抵消回滚；不得把 `dirty=true` 当作成功状态继续启动。

## 凭据轮换

一般环境变量修改后使用 `task compose-restart` 重新创建容器。已有 PostgreSQL 数据卷不会因修改 `POSTGRES_PASSWORD` 或 `.env` 自动更新数据库角色密码。

轮换 `DB_PASSWORD` 时按以下顺序操作，缩短新旧密码不一致窗口：

1. 生成并暂存新密码，不写入命令行参数或 Shell 历史。
2. 交互进入数据库：`docker exec -it postgre psql -U bot_admin -d postgres`。
3. 在 `psql` 中执行 `\password bot_admin`，按提示输入两次新密码，然后退出。
4. 立即原子更新 `.env` 中的 `DB_PASSWORD`，再执行 `task compose-restart`。
5. 检查 PostgreSQL 健康状态和机器人数据库连接。

若重启失败，应保持机器人停止，在容器内用 `\password bot_admin` 恢复旧密码、恢复旧 `.env`，再重新创建容器。轮换 `NAPCAT_TOKEN` 时准备好两端配置，在最短维护窗口内依次更新 NapCat 与 Bot 并重连；轮换 `JWT_SECRET` 或 `WEBUI_PASSWORD` 后所有 WebUI 用户需要重新登录。

## 故障排查

```bash
docker compose ps
docker compose logs --tail=200 bot postgres
task migrate-version
```

常见问题：

- `password authentication failed`：容器环境密码与持久卷内数据库用户密码不一致。
- `database does not exist`：旧卷初始化时使用了不同数据库名，当前项目固定为 `w1ndys_bot`。
- WebSocket `401`：NapCat Token 与 `NAPCAT_TOKEN` 不一致。
- 路由刷新 `404`：应访问机器人内置 WebUI 端口，反向代理需把未知前端路径转发给机器人。
- `dirty=true`：迁移中断；保留日志和备份，确认失败版本后再修复，不要直接改迁移表。
