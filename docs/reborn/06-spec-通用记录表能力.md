<!-- 📌 影响范围：定义通用记录表能力——本项目解决"每插件一套 WebUI"的核心；一次实现，所有记录表插件复用。 -->
# Spec 06：通用记录表能力（核心）

## 目标

实现平台级"通用记录表"：插件用 Go 结构体声明列定义 → 平台自动提供存储、通用 REST API、通用 Vue 页面、CSV 导入导出、审计、群隔离、乐观锁。本 Spec 完成后，新增一个记录表插件的 WebUI/API/存储成本趋近于零。

## 前置依赖

- Spec 03（插件运行时、状态表）、Spec 04（认证/审计/配置）、Spec 05（WebUI 骨架、注册表、Toast、公共组件）。

## 设计决策

### 契约（代码声明，编译期）

```go
// internal/recordtable/table.go
type ColumnType int
const (
    ColumnString ColumnType = iota // 单行文本，≤256 字符
    ColumnText                     // 多行文本，≤8K
    ColumnInteger                  // 64 位整数
    ColumnNumber                   // 浮点（64 位）
    ColumnBoolean
    ColumnEnum                     // 需提供 Options
    ColumnDatetime                 // RFC3339 时间
)

type Column struct {
    Key        string     // 稳定 snake_case 标识
    Label      string     // 展示名
    Type       ColumnType
    Required   bool
    Options    []string   // enum 时必填
    Default    any
    Searchable bool       // 是否参与列表搜索（q 参数）
    Hidden     bool       // 敏感/内部列：API 不回显（如内部标记）
}

type RecordTable struct {
    PluginKey    string     // 所属插件稳定 Key
    DisplayName  string     // 页面标题
    Columns      []Column
    UniqueKeys   [][]string // 每群内的唯一列组合，如 [["keyword"]]
    DefaultSort  string     // 如 "-created_at"（- 表示倒序）
    AppendOnly   bool       // true 时禁用修改/删除（签到、邀请记录等只增场景）
    MaxRecords   int        // 每群上限（默认 10000），防失控
}
```

**红线**：不接受客户端表名/列名/排序片段/过滤表达式/SQL；列定义只来自代码。

### 存储（JSONB 单表）

```sql
-- 迁移 000004_plugin_records
plugin_records(
  id bigserial PK,
  plugin_key text NOT NULL,
  group_id bigint NOT NULL,
  data jsonb NOT NULL,          -- 扁平列值
  created_at timestamptz NOT NULL default now(),
  updated_at timestamptz NOT NULL default now(),
  version int8 NOT NULL default 1,
  UNIQUE (plugin_key, group_id, id)
)
CREATE INDEX idx_plugin_records_group ON plugin_records (plugin_key, group_id);
```

- 唯一约束在应用层校验（查重 + 唯一索引兜底可后续加表达式索引）；记录数按 `MaxRecords` 上限。
- 时间列存 RFC3339（UTC）；展示由前端转时区。

### 通用 REST API（平台注册一次，按插件 Key 路由）

```text
GET    /api/plugins/{key}/record-table/definition      → 列定义（供页面渲染）
GET    /api/plugins/{key}/groups/{gid}/records?page=&page_size=&q=&sort=
POST   /api/plugins/{key}/groups/{gid}/records         {data}
PUT    /api/plugins/{key}/groups/{gid}/records/{id}    {data, expected_version}
DELETE /api/plugins/{key}/groups/{gid}/records/{id}    {expected_version}
POST   /api/plugins/{key}/groups/{gid}/records/import  (multipart CSV, 按唯一键 upsert)
GET    /api/plugins/{key}/groups/{gid}/records/export  (CSV 下载, 全量, 上限 MaxRecords)
```

- 鉴权：复用 `Authorizer`；群 ID 来自路径（可信），不信任 Body。
- 校验：平台按列类型/必填/枚举/长度校验；插件可注册可选钩子 `ValidateRecord(data) error` 做领域校验。
- 乐观锁：version 不匹配 409；写操作与审计同事务。
- 写成功后调用插件可选回调 `OnRecordsChanged(ctx)` 刷新运行快照（先提交、后刷新；刷新失败保留旧快照并告警）。

### CSV 导入导出

- 导出：表头为列 Key（snake_case），值为字符串化后的列值；输出 UTF-8 with BOM（Excel 打开中文不乱码）。
- 导入：按 `UniqueKeys` 匹配 upsert（存在则更新、不存在则新增）；逐行校验，返回 `{success, failed, errors[]}`（含行号与原因）；单次上限（如 5000 行）；编码自动识别 UTF-8/GBK。
- 提供模板下载：`GET .../records/template`（表头 + 一行示例）。

### 通用 Vue 页面

`RecordTablePage.vue`（注册表里 `kind: 'record-table'` 的插件共用，不按插件复制）：

- 群选择器（必选）→ 拉取 definition + 分页列表。
- 搜索框（q 命中 Searchable 列）、排序（DefaultSort + 列排序切换）。
- 表格：列渲染（enum 显示中文选项、time 转本地、boolean 开关样式）、行操作（编辑/删除，AppendOnly 时隐藏）。
- 编辑弹窗：按列类型生成表单（enum 下拉、time 日期选择、text 多行、secret 类型列不编辑）。
- CSV 导入（文件选择 + 结果报告）、导出（下载当前群）、模板下载。
- 全部操作走全局 Toast；处理加载/空/错误/409 冲突/窄屏/重复提交状态。

## 实现任务（按序）

1. 迁移 `000005_plugin_records`（配对 up/down）。
2. `internal/recordtable/table.go`：契约 + 启动校验（列 Key 唯一、enum 有选项、UniqueKeys 引用存在的列、AppendOnly 与唯一键不冲突）。
3. `repository.go`：List/Create/Update/Delete/LoadGroup/LoadAllEnabled + 唯一冲突检测（应用层 + 唯一索引兜底）、MaxRecords 上限、分页与排序（排序字段白名单 = 列 Key 集合）。
4. `service.go`：校验（类型/必填/枚举/长度/自定义钩子）、乐观锁、审计（同事务）、快照回调编排。
5. `http.go`：上述 8 个端点注册进 `internal/webapi`；错误映射。
6. `csv.go`：导出/导入/模板；导入逐行校验与 upsert；错误报告结构。
7. WebUI：`RecordTablePage.vue` + 注册表接入 + 路由（`/plugins/:key/records`）；definition 驱动渲染。
8. 测试覆盖（见下）。

## 测试

- 契约校验：列 Key 重复、enum 无选项、UniqueKeys 引用不存在列 → 启动失败。
- CRUD：正常增删改查；必填缺失/类型错误/枚举越界/超长 → 400；唯一键冲突 → 409；乐观锁陈旧 → 409；越群访问 → 403/404；MaxRecords 超限 → 409。
- 审计：每次写操作审计表有记录，before/after 正确。
- 并发：同群并发写同一唯一键 → 仅一个成功；并发更新同记录 → 一个 409。
- CSV：导出→导入往返一致；导入部分失败返回行级错误；异常编码/超限拒绝；AppendOnly 插件禁用改/删。
- 快照回调：写成功后触发，回调失败不影响提交且告警。
- WebUI：`task web-build` 通过；手测通用页面全流程。

## 验收清单

- [ ] `task lint`、`task test`（含 race）全绿；`task web-build` 通过。
- [ ] 用一个临时测试插件验证：注册记录表后，WebUI 直接出现通用页面，无需写任何插件专属 API/页面。
- [ ] CSV 往返、唯一键 upsert、行级错误报告可用。
- [ ] 提交：`feat(recordtable): 📋 通用记录表能力`。

## 边界与风险

- 本 Spec 是窄契约：嵌套对象、关联查询、表达式、工作流一律拒绝，走专属路径（Spec 08）。
- 记录数据量受 MaxRecords 限制；需要大数据/强关系时用专属表（迁移升级路径在 Spec 08 说明）。
- 排序字段必须白名单校验；CSV 导入必须防公式注入（单元格以 `=`、`+`、`-`、`@` 开头时加前缀或转义）。
