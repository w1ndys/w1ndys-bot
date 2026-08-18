<!-- 📌 影响范围：reborn 文档索引；说明阅读顺序与用途。 -->
# W1ndys Bot 重生版开发文档

> 基于旧 Py 版（`w1ndys/w1ndysbot`）与现行 Go 版（`w1ndys/w1ndys-bot`）重写的空白新项目开发方案。旧配置全部作废。

## 阅读顺序

| 文档 | 内容 | 何时读 |
| --- | --- | --- |
| [00-总体设计与开发决策](00-总体设计与开发决策.md) | 三个战略问题（WebUI 复杂度 / Go vs Python / 管理形式）、核心洞察（通用记录表）、插件分类表、架构与 ADR | **先读** |
| [01-spec-项目骨架与基础设施](01-spec-项目骨架与基础设施.md) | 配置/日志/DB/迁移/健康检查/Task/优雅退出 | 第 1 步 |
| [02-spec-NapCat接入与消息管线](02-spec-NapCat接入与消息管线.md) | 反向 WS、事件模型、Action Client（echo 关联/超时/重连） | 第 2 步 |
| [03-spec-插件运行时](03-spec-插件运行时.md) | PluginSpec/Catalog/Dispatcher/门禁/生命周期/身份（加载机制核心） | 第 3 步 |
| [04-spec-数据层与管理面](04-spec-数据层与管理面.md) | 认证（JWT）、Actor、审计、标量配置 Schema | 第 4 步 |
| [05-spec-平台WebUI骨架](05-spec-平台WebUI骨架.md) | 登录/布局/插件运行页/配置页/公共组件/注册表 | 第 5 步 |
| [06-spec-通用记录表能力](06-spec-通用记录表能力.md) | **核心**：代码声明列 → 通用存储/API/页面/CSV/审计 | 第 6 步 |
| [07-spec-记录表插件示例](07-spec-记录表插件示例.md) | keyword_reply / faq / group_ban_words / group_welcome 模板 | 第 7 步 |
| [08-spec-专属复杂插件与QQ应急](08-spec-专属复杂插件与QQ应急.md) | 专属路径（自有表/API/页面）+ QQ 应急命令 | 第 8 步 |
| [09-spec-打包部署与整体验收](09-spec-打包部署与整体验收.md) | Docker/安全基线/README/最终验收 | 第 9 步 |

## 核心一句话

> WebUI 是对的，但别每个插件写一套。平台提供"通用记录表"（窄契约：群作用域的扁平记录 + 分页/搜索/CRUD/CSV），插件用 Go 结构体声明列定义即可，WebUI/API/存储自动获得。真正复杂的插件（外部副作用/强关联/工作流）才走专属路径。
