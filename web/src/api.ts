// 📌 影响范围：读取浏览器当前域名与会话 Token；调用后端 /api HTTP 接口。
import { setSessionToken, sessionToken } from './session'

export interface ApiEnvelope<T> {
  code: string
  message: string
  data: T
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  // constructor 创建保留 HTTP 状态与业务码的 API 错误。
  // @param status：HTTP 状态；code：后端业务码；message：用户可读消息。
  // @returns ApiError 实例。
  // ⚠️副作用说明：创建错误对象，不修改外部状态。
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code

    // >>> 数据演变示例
    // 1. 409+plugin_config_conflict+版本冲突 -> 可按 code 识别的 Error。
    // 2. 404+plugin_config_not_supported+暂无配置 -> 可按 status 识别的 Error。
  }
}

export type PluginConfigFieldType = 'string' | 'multiline' | 'integer' | 'boolean' | 'enum' | 'secret' | 'string_list_json' | 'weighted_terms_json' | 'combination_rules_json'

export interface PluginConfigField {
  key: string
  display_name: string
  description?: string
  type: PluginConfigFieldType
  required: boolean
  default?: unknown
  options?: string[]
}

export interface PluginConfigSchema {
  fields: PluginConfigField[]
}

export type PluginResourceFieldType = 'string' | 'multiline' | 'boolean' | 'enum' | 'datetime'

export interface PluginResourceField {
  key: string
  display_name: string
  description?: string
  type: PluginResourceFieldType
  required: boolean
  default?: unknown
  options?: string[]
}

export interface PluginConfigState {
  plugin_name: string
  config: Record<string, unknown>
  version: number
}

export interface PluginResourceDescriptor {
  key: string
  display_name: string
  description?: string
  fields: PluginResourceField[]
  read_only_fields?: string[]
  can_create: boolean
  can_update: boolean
  can_delete: boolean
  hidden?: boolean
  max_page_size: number
}

export interface PluginResourceRecord {
  id: number
  data: Record<string, unknown>
  version: number
}

export interface PluginResourcePage {
  items: PluginResourceRecord[]
  page: number
  page_size: number
  total: number
}

export interface LoginResult {
  token: string
  expires_in: number
}

export interface PluginState {
  name: string
  display_name: string
  description: string
  available: boolean
  enabled: boolean
  priority: number
  group_controllable: boolean
}

export interface PluginGroupOverride {
  group_id: string
  enabled: boolean
  version: number
}

export interface PluginGroupControlState {
  plugin_name: string
  plugin_enabled: boolean
  default_enabled: boolean
  default_version: number
  overrides: PluginGroupOverride[]
}

export interface FeatureState {
  plugin_name: string
  key: string
  display_name: string
  description: string
  available: boolean
  default_commands: string[]
  default_permissions: Record<string, boolean>
}

export interface CommandState {
  id: number
  scope_type: 'global' | 'group'
  scope_id: string
  plugin_name: string
  feature_key: string
  command: string
  normalized_command: string
  enabled: boolean
}

export interface CommandCreate {
  scope_type: 'global' | 'group'
  scope_id: string
  plugin_name: string
  feature_key: string
  command: string
}

export interface PermissionState {
  id: number
  scope_type: 'global' | 'group'
  scope_id: string
  plugin_name: string
  feature_key: string
  subject_type: 'role' | 'user'
  subject_id: string
  effect: 'allow' | 'deny'
}

export type PermissionSet = Omit<PermissionState, 'id'>

export interface SettingState {
  key: string
  value: unknown
  description: string
  overridden: boolean
}

export interface AuditState {
  id: number
  actor_id: string
  actor_role: string
  channel: string
  action: string
  target_type: string
  target_id: string
  before: unknown
  after: unknown
  success: boolean
  error_message: string
  request_id: string
  created_at: string
}

export type AuditSummary = Omit<AuditState, 'before' | 'after'>

export interface AuditPage {
  items: AuditSummary[]
  page: number
  page_size: number
  total: number
}

export interface AuditQuery {
  page: number
  page_size: number
  actor_id?: string
  action?: string
  target_type?: string
  target_id?: string
  start_time?: string
  end_time?: string
}

// apiRequest 执行统一鉴权请求并解析 code/message/data 响应。
// @param path：以 /api 开头的接口路径；options：Fetch 请求参数。
// @returns 成功响应中的 data，失败时抛出包含后端 message 的 Error。
// ⚠️副作用说明：发起网络请求；401 时清理浏览器会话 Token。
export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')
  // [决策理由] 仅在存在请求体时声明 JSON，避免 GET 产生无意义预检差异。
  if (options.body) {
    headers.set('Content-Type', 'application/json')
  }
  // [决策理由] 登录接口没有 Token，其他请求存在会话时统一附加 Bearer 凭证。
  if (sessionToken.value !== '') {
    headers.set('Authorization', `Bearer ${sessionToken.value}`)
  }
  const response = await fetch(path, { ...options, headers })
  const envelope = (await response.json()) as ApiEnvelope<T>
  // [决策理由] 401 表示本地 Token 已失效，应立即清理以便路由返回登录页。
  if (response.status === 401) {
    setSessionToken('')
  }
  // [决策理由] HTTP 状态和业务码必须同时成功，防止代理异常被误作业务数据。
  if (!response.ok || envelope.code !== 'ok') {
    throw new ApiError(response.status, envelope.code, envelope.message || '请求失败')
  }

  // >>> 数据演变示例
  // 1. 200+code=ok+data插件列表 -> 返回插件数组。
  // 2. 401+unauthorized -> 清Token -> 抛出后端错误。
  return envelope.data
}

// login 使用唯一管理员 QQ 和共享环境密码登录。
// @param qq：SUPER_ADMIN_QQ；password：WEBUI_PASSWORD。
// @returns 登录 Token 与有效秒数。
// ⚠️副作用说明：发起登录网络请求；成功时保存 Token。
export async function login(qq: string, password: string): Promise<LoginResult> {
  const result = await apiRequest<LoginResult>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ qq, password }),
  })
  setSessionToken(result.token)

  // >>> 数据演变示例
  // 1. 正确QQ+密码 -> API Token -> localStorage -> 返回结果。
  // 2. 错误密码 -> apiRequest抛错 -> Token不变。
  return result
}

// listPlugins 获取插件元数据和当前运行配置。
// @param 无。
// @returns 按后端优先级排序的插件列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPlugins(): Promise<PluginState[]> {
  const result = apiRequest<PluginState[]>('/api/plugins')

  // >>> 数据演变示例
  // 1. 有效Token -> GET /api/plugins -> PluginState[]。
  // 2. 过期Token -> 清理会话并抛错。
  return result
}

// patchPlugin 修改插件启用状态或优先级。
// @param name：插件稳定名称；patch：只能包含 enabled 或 priority 之一。
// @returns 后端热刷新后的插件状态。
// ⚠️副作用说明：修改后端数据库、审计记录和插件运行快照。
export function patchPlugin(name: string, patch: { enabled: boolean } | { priority: number }): Promise<PluginState> {
  const result = apiRequest<PluginState>(`/api/plugins/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  })

  // >>> 数据演变示例
  // 1. ping+enabled=true -> PATCH -> 返回启用状态。
  // 2. admin+enabled=false -> 409 -> 抛出保护错误。
  return result
}

// getPluginGroupControl 读取插件群默认与单群覆盖。
// @param name：插件稳定名称。
// @returns 群控制权威快照。
// ⚠️副作用说明：发起鉴权网络请求。
export function getPluginGroupControl(name: string): Promise<PluginGroupControlState> {
  const result = apiRequest<PluginGroupControlState>(`/api/plugins/${encodeURIComponent(name)}/group-control`)

  // >>> 数据演变示例
  // 1. keyword_reply -> GET -> default+覆盖。
  // 2. 不支持 -> 404 -> 抛错。
  return result
}

// setPluginGroupDefault 使用版本检查更新群默认值。
// @param name：插件名；enabled：目标值；version：当前版本。
// @returns 更新后快照。
// ⚠️副作用说明：写入后端策略、审计并热刷新。
export function setPluginGroupDefault(name: string, enabled: boolean, version: number): Promise<PluginGroupControlState> {
  const result = apiRequest<PluginGroupControlState>(`/api/plugins/${encodeURIComponent(name)}/group-control`, { method: 'PATCH', body: JSON.stringify({ enabled, expected_version: version }) })

  // >>> 数据演变示例
  // 1. false/v2 -> PATCH -> v3。
  // 2. 陈旧v1 -> 409 -> 抛错。
  return result
}

// setPluginGroupOverride 新增或更新单群覆盖。
// @param name/groupID：目标；enabled：值；version：0 新增，正数更新。
// @returns 新覆盖快照。
// ⚠️副作用说明：写入覆盖、审计并热刷新。
export function setPluginGroupOverride(name: string, groupID: string, enabled: boolean, version: number): Promise<PluginGroupOverride> {
  const result = apiRequest<PluginGroupOverride>(`/api/plugins/${encodeURIComponent(name)}/group-overrides/${encodeURIComponent(groupID)}`, { method: 'PUT', body: JSON.stringify({ enabled, expected_version: version }) })

  // >>> 数据演变示例
  // 1. group100/true/v0 -> PUT -> v1。
  // 2. 陈旧版本 -> 409 -> 抛错。
  return result
}

// deletePluginGroupOverride 删除覆盖以恢复继承。
// @param name/groupID：目标；version：当前版本。
// @returns 删除标记。
// ⚠️副作用说明：删除后端覆盖、审计并热刷新。
export function deletePluginGroupOverride(name: string, groupID: string, version: number): Promise<{ deleted: boolean }> {
  const params = new URLSearchParams({ expected_version: String(version) })
  const result = apiRequest<{ deleted: boolean }>(`/api/plugins/${encodeURIComponent(name)}/group-overrides/${encodeURIComponent(groupID)}?${params.toString()}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. group100/v2 -> DELETE -> 继承默认。
  // 2. 陈旧v1 -> 409 -> 抛错。
  return result
}

// getPluginConfigSchema 获取插件声明式配置字段。
// @param name：插件稳定名称。
// @returns 后端校验过的配置 Schema。
// ⚠️副作用说明：发起鉴权网络请求。
export function getPluginConfigSchema(name: string): Promise<PluginConfigSchema> {
  const result = apiRequest<PluginConfigSchema>(`/api/plugins/${encodeURIComponent(name)}/config/schema`)

  // >>> 数据演变示例
  // 1. echo -> GET schema -> response_prefix 字段。
  // 2. legacy -> 404 plugin_config_not_supported -> 抛出 ApiError。
  return result
}

// getPluginConfig 获取插件脱敏配置快照。
// @param name：插件稳定名称。
// @returns 配置对象及乐观锁版本。
// ⚠️副作用说明：发起鉴权网络请求；secret 不由后端返回。
export function getPluginConfig(name: string): Promise<PluginConfigState> {
  const result = apiRequest<PluginConfigState>(`/api/plugins/${encodeURIComponent(name)}/config`)

  // >>> 数据演变示例
  // 1. echo:v2 -> GET config -> {config:{response_prefix:""},version:2}。
  // 2. 不支持配置 -> 404 -> 抛出 ApiError。
  return result
}

// putPluginConfig 使用版本检查保存完整插件配置。
// @param name：插件稳定名称；config：非 secret 完整草稿与待更新 secret；expectedVersion：读取时版本。
// @returns 保存并热应用后的脱敏配置快照。
// ⚠️副作用说明：更新数据库、审计和插件运行配置。
export function putPluginConfig(name: string, config: Record<string, unknown>, expectedVersion: number): Promise<PluginConfigState> {
  const result = apiRequest<PluginConfigState>(`/api/plugins/${encodeURIComponent(name)}/config`, {
    method: 'PUT',
    body: JSON.stringify({ config, expected_version: expectedVersion }),
  })

  // >>> 数据演变示例
  // 1. prefix=x,expected=2 -> PUT -> 脱敏快照version=3。
  // 2. expected=1但当前=2 -> 409 plugin_config_conflict -> 抛出 ApiError。
  return result
}

// listPluginResources 获取插件声明的业务资源描述。
// @param pluginName：插件稳定名称。
// @returns 可由通用界面管理的资源列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPluginResources(pluginName: string): Promise<PluginResourceDescriptor[]> {
  const result = apiRequest<PluginResourceDescriptor[]>(`/api/plugins/${encodeURIComponent(pluginName)}/resources`)

  // >>> 数据演变示例
  // 1. 插件声明rules资源 -> GET -> 返回字段与操作能力。
  // 2. 插件无业务资源 -> GET -> 返回空数组。
  return result
}

// listPluginResourceRecords 分页读取插件业务资源记录。
// @param pluginName：插件稳定名称；resourceKey：资源稳定键；page：页码；pageSize：每页数量。
// @returns 资源记录分页快照。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPluginResourceRecords(pluginName: string, resourceKey: string, page: number, pageSize: number): Promise<PluginResourcePage> {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  const result = apiRequest<PluginResourcePage>(`/api/plugins/${encodeURIComponent(pluginName)}/resources/${encodeURIComponent(resourceKey)}?${params.toString()}`)

  // >>> 数据演变示例
  // 1. rules第1页20条 -> GET?page=1&page_size=20 -> 返回分页记录。
  // 2. 未注册资源 -> 404 -> 抛出后端错误。
  return result
}

// createPluginResourceRecord 新增一条插件业务资源记录。
// @param pluginName：插件稳定名称；resourceKey：资源稳定键；data：Schema 允许的字段数据。
// @returns 创建后的权威记录。
// ⚠️副作用说明：写入插件业务表并产生审计记录。
export function createPluginResourceRecord(pluginName: string, resourceKey: string, data: Record<string, unknown>): Promise<PluginResourceRecord> {
  const result = apiRequest<PluginResourceRecord>(`/api/plugins/${encodeURIComponent(pluginName)}/resources/${encodeURIComponent(resourceKey)}`, {
    method: 'POST',
    body: JSON.stringify({ data }),
  })

  // >>> 数据演变示例
  // 1. {keyword:"你好"} -> POST -> 返回带id与version的记录。
  // 2. 重复关键词 -> 409 -> 抛出冲突错误。
  return result
}

// updatePluginResourceRecord 使用乐观版本更新插件业务资源记录。
// @param pluginName：插件稳定名称；resourceKey：资源稳定键；id：记录ID；data：编辑字段；expectedVersion：读取版本。
// @returns 更新后的权威记录。
// ⚠️副作用说明：更新插件业务表并产生审计记录。
export function updatePluginResourceRecord(pluginName: string, resourceKey: string, id: number, data: Record<string, unknown>, expectedVersion: number): Promise<PluginResourceRecord> {
  const result = apiRequest<PluginResourceRecord>(`/api/plugins/${encodeURIComponent(pluginName)}/resources/${encodeURIComponent(resourceKey)}/${encodeURIComponent(String(id))}`, {
    method: 'PATCH',
    body: JSON.stringify({ data, expected_version: expectedVersion }),
  })

  // >>> 数据演变示例
  // 1. id=2,v1+新回复 -> PATCH -> 返回v2记录。
  // 2. id=2,v1但服务端v2 -> 409 -> 抛出冲突错误。
  return result
}

// deletePluginResourceRecord 使用乐观版本删除插件业务资源记录。
// @param pluginName：插件稳定名称；resourceKey：资源稳定键；id：记录ID；expectedVersion：读取版本。
// @returns 删除成功后的空数据。
// ⚠️副作用说明：删除插件业务记录并产生审计记录。
export function deletePluginResourceRecord(pluginName: string, resourceKey: string, id: number, expectedVersion: number): Promise<{ deleted: boolean }> {
  const params = new URLSearchParams({ expected_version: String(expectedVersion) })
  const result = apiRequest<{ deleted: boolean }>(`/api/plugins/${encodeURIComponent(pluginName)}/resources/${encodeURIComponent(resourceKey)}/${encodeURIComponent(String(id))}?${params.toString()}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. id=2,v3 -> DELETE -> {deleted:true}并刷新列表。
  // 2. id=2,v2但服务端v3 -> 409 -> 抛出冲突错误。
  return result
}

// listPluginFeatures 获取指定插件的 Manifest 功能元数据。
// @param pluginName：插件稳定名称。
// @returns 功能列表及默认触发词、默认权限。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPluginFeatures(pluginName: string): Promise<FeatureState[]> {
  const result = apiRequest<FeatureState[]>(`/api/plugins/${encodeURIComponent(pluginName)}/features`)

  // >>> 数据演变示例
  // 1. ping -> GET features -> [ping功能]。
  // 2. missing -> 404 -> 抛出插件不存在。
  return result
}

// listCommands 获取全部功能触发词。
// @param 无。
// @returns 全局与群级触发词列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listCommands(): Promise<CommandState[]> {
  const result = apiRequest<CommandState[]>('/api/commands')

  // >>> 数据演变示例
  // 1. 有效Token -> GET commands -> CommandState[]。
  // 2. Token失效 -> 清会话并抛错。
  return result
}

// createCommand 为插件功能新增触发词。
// @param input：作用域、功能目标与触发词。
// @returns 保存并热刷新后的触发词状态。
// ⚠️副作用说明：新增数据库命令、审计记录并刷新后端命令快照。
export function createCommand(input: CommandCreate): Promise<CommandState> {
  const result = apiRequest<CommandState>('/api/commands', { method: 'POST', body: JSON.stringify(input) })

  // >>> 数据演变示例
  // 1. ping.ping+测试 -> POST -> 新CommandState。
  // 2. 同作用域重复 -> 409 command_conflict -> 抛错。
  return result
}

// renameCommand 修改已有触发词文本。
// @param id：命令ID；command：新触发词。
// @returns 保存后的触发词状态。
// ⚠️副作用说明：更新数据库命令、审计记录并刷新后端命令快照。
export function renameCommand(id: number, command: string): Promise<CommandState> {
  const result = apiRequest<CommandState>(`/api/commands/${id}`, { method: 'PATCH', body: JSON.stringify({ command }) })

  // >>> 数据演变示例
  // 1. id1+延迟 -> PATCH -> 更新状态。
  // 2. 重复文本 -> 409 -> 抛错。
  return result
}

// deleteCommand 删除功能触发词。
// @param id：命令ID。
// @returns 删除成功后的空数据。
// ⚠️副作用说明：删除数据库命令、写审计并刷新后端命令快照。
export function deleteCommand(id: number): Promise<null> {
  const result = apiRequest<null>(`/api/commands/${id}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. id1 -> DELETE -> null。
  // 2. id404 -> 404 -> 抛错。
  return result
}

// listPermissions 获取全部显式权限策略。
// @param 无。
// @returns 权限策略列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPermissions(): Promise<PermissionState[]> {
  const result = apiRequest<PermissionState[]>('/api/permissions')

  // >>> 数据演变示例
  // 1. 有效Token -> GET permissions -> PermissionState[]。
  // 2. Token失效 -> 清会话 -> 抛出鉴权错误。
  return result
}

// setPermission 新增权限策略或更新相同维度的效果。
// @param input：权限作用域、目标、主体和效果。
// @returns 保存并热刷新后的权限策略。
// ⚠️副作用说明：写入后端权限与审计记录，并刷新权限快照。
export function setPermission(input: PermissionSet): Promise<PermissionState> {
  const result = apiRequest<PermissionState>('/api/permissions', { method: 'POST', body: JSON.stringify(input) })

  // >>> 数据演变示例
  // 1. group123+ping+user200+allow -> UPSERT -> 返回策略。
  // 2. user+非数字QQ -> 400 -> 抛出校验错误。
  return result
}

// deletePermission 删除一条显式权限策略。
// @param id：权限策略主键。
// @returns 删除成功后的空数据。
// ⚠️副作用说明：删除后端权限、写入审计并刷新权限快照。
export function deletePermission(id: number): Promise<null> {
  const result = apiRequest<null>(`/api/permissions/${id}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. id8 -> DELETE -> null并回退下一层规则。
  // 2. id404 -> 404 -> 抛出不存在错误。
  return result
}

// listSettings 获取全部受控系统设置及当前有效值。
// @param 无。
// @returns 合并默认值与数据库覆盖后的设置列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listSettings(): Promise<SettingState[]> {
  const result = apiRequest<SettingState[]>('/api/settings')

  // >>> 数据演变示例
  // 1. prefix已覆盖 -> 返回value="!"且overridden=true。
  // 2. DB无覆盖 -> 返回后端定义默认值且overridden=false。
  return result
}

// setSetting 保存一个受控系统设置的JSON值。
// @param key：稳定设置键；value：符合该键定义的值。
// @returns 保存并热刷新后的设置状态。
// ⚠️副作用说明：写入设置与审计记录，并刷新运行时设置快照。
export function setSetting(key: string, value: unknown): Promise<SettingState> {
  const result = apiRequest<SettingState>(`/api/settings/${encodeURIComponent(key)}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
  })

  // >>> 数据演变示例
  // 1. command_prefix+"!" -> PUT -> 返回覆盖状态。
  // 2. default_page_size+500 -> 400 -> 抛出校验错误。
  return result
}

// resetSetting 删除数据库覆盖并恢复后端定义默认值。
// @param key：稳定设置键。
// @returns 删除成功后的空数据。
// ⚠️副作用说明：删除设置覆盖、写入审计并刷新运行时设置快照。
export function resetSetting(key: string): Promise<null> {
  const result = apiRequest<null>(`/api/settings/${encodeURIComponent(key)}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. prefix覆盖存在 -> DELETE -> null并恢复"/"。
  // 2. 无覆盖 -> 404 -> 抛出无覆盖错误。
  return result
}

// listAuditLogs 分页读取只读审计日志。
// @param query：分页、操作者、动作、资源与UTC时间范围筛选。
// @returns 审计记录、当前分页和总数。
// ⚠️副作用说明：发起鉴权网络请求。
export function listAuditLogs(query: AuditQuery): Promise<AuditPage> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    // [决策理由] 空筛选不应传给后端，否则会把“不限”误写成精确空值条件。
    if (value !== undefined && value !== '') {
      params.set(key, String(value))
    }
  }
  const result = apiRequest<AuditPage>(`/api/audit-logs?${params.toString()}`)

  // >>> 数据演变示例
  // 1. page=2+action=plugin.enable -> URL参数编码 -> 返回第2页审计。
  // 2. 空可选筛选+page=1 -> 仅发送分页参数 -> 返回全部类型首屏。
  return result
}

// getAuditLog 获取单条审计的完整前后快照。
// @param id：正整数审计记录ID。
// @returns 后端权威审计详情。
// ⚠️副作用说明：发起鉴权网络请求。
export function getAuditLog(id: number): Promise<AuditState> {
  const result = apiRequest<AuditState>(`/api/audit-logs/${id}`)

  // >>> 数据演变示例
  // 1. id=8 -> GET详情 -> 返回完整before/after。
  // 2. id=404 -> 后端404 -> 抛出“审计日志不存在”。
  return result
}
