# 发布手册

本项目使用北京时间日历版本 `vYYYY.MM.DD.HHmm`。推送符合格式的 Git Tag 后，GitHub Actions 会先验证源码，再发布 `linux/amd64`、`linux/arm64` 镜像到 GHCR，并创建 GitHub Release。

## 发布前

1. 确认工作树中的发布内容已形成聚焦且可审查的提交。
2. 将 `CHANGELOG.md` 的 `Unreleased` 内容整理到本次版本，并更新底部 compare 链接。
3. 运行完整验收：

   ```bash
   task lint
   task test
   task web-build
   task web-e2e
   git diff --check
   ```

4. 确认目标提交已推送到受保护的默认分支。仓库应使用 Ruleset 限制 `v*` Tag 只能由维护者创建。

## 创建版本

版本时间以北京时间生成。使用带说明的 Tag，并在推送前再次核对目标提交：

```bash
TAG="$(TZ=Asia/Shanghai date '+v%Y.%m.%d.%H%M')"
git show --stat --oneline HEAD
git tag -a "$TAG" -m "发布 $TAG"
git push origin "$TAG"
```

工作流会校验标签格式、真实日历日期，以及 Tag 提交是否属于默认分支。发布 job 只在验证 job 成功后取得 `packages: write` 与 `contents: write` 权限，不使用 PAT，也不对 PR 或任意分支开放发布权限。

版本镜像 Tag 一旦存在，工作流会拒绝覆盖。工作流先创建 Release 草稿、只推送版本镜像，Release 正式发布成功后才更新 `latest`。若镜像已经成功但 Release 在重试后仍无法发布，应人工核对草稿中的 Tag、提交和镜像摘要后发布草稿；不要重跑整个发布任务，也不要覆盖版本镜像。

## 发布产物

镜像地址：

```text
ghcr.io/w1ndys/w1ndys-bot:<版本 Tag>
ghcr.io/w1ndys/w1ndys-bot:latest
```

生产部署必须在 `.env` 中使用不可变版本 Tag：

```dotenv
BOT_IMAGE=ghcr.io/w1ndys/w1ndys-bot:v2026.07.27.1200
```

然后执行 `task compose-up-image`。`latest` 仅用于快速体验，不作为生产回滚依据。

## 发布后核验

1. GitHub Actions 的验证和多架构发布 job 均成功。
2. 首次发布后在 GitHub Package 设置中确认容器已关联本仓库并设为 Public；否则未登录的部署机器无法拉取。使用隔离 Docker 配置验证匿名拉取，避免本机登录状态掩盖权限问题：

   ```bash
   anonymous_config="$(mktemp -d)"
   DOCKER_CONFIG="$anonymous_config" docker pull "ghcr.io/w1ndys/w1ndys-bot:$TAG"
   rm -r "$anonymous_config"
   ```

   若组织策略暂时不允许公开包，部署机必须使用仅含 `read:packages` 的专用令牌执行 `docker login ghcr.io`，不得复用发布令牌。
3. GitHub Release 已生成，GHCR 显示 amd64/arm64 manifest、来源提交和版本标签。
4. 核对 Release 中记录的镜像 digest，并在隔离环境使用版本 Tag 完成启动、迁移、WebUI 登录和 NapCat 连接检查。
5. 发现问题时不要移动或重建原版本 Tag；修复后创建新版本。部署回滚按 `docs/deployment.md` 将 `BOT_IMAGE` 切回已验证 Tag。

在 GHCR Package 设置中启用不可变版本 Tag（如平台提供该策略）；`latest` 可以移动，日历版本 Tag 不得覆盖。

若版本镜像和 Release 均已成功，但工作流仅在更新 `latest` 时失败，不要重跑整个发布任务。维护者确认版本镜像后，可使用具有 `write:packages` 的发布凭据单独恢复可变标签：

```bash
docker buildx imagetools create \
  --tag ghcr.io/w1ndys/w1ndys-bot:latest \
  "ghcr.io/w1ndys/w1ndys-bot:$TAG"
```
